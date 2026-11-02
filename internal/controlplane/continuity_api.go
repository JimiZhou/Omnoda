package controlplane

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/capabilities/executor"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
	"github.com/JimiZhou/Omnoda/internal/supervisor"
)

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	_ = decode(r, &body)
	c, err := s.DB.CreateConversation(userOf(r), body.Title)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	writeJSON(w, 201, c)
}

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.ListConversations(userOf(r), 50)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if rows == nil {
		rows = []storage.Conversation{}
	}
	writeJSON(w, 200, map[string]any{"conversations": rows})
}

func (s *Server) getConversation(w http.ResponseWriter, r *http.Request) {
	c, err := s.DB.GetConversation(r.PathValue("id"))
	if err != nil || c.OwnerID != userOf(r) {
		writeErr(w, 404, "not_found", "conversation not found")
		return
	}
	msgs, err := s.DB.Messages(c.ID)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if msgs == nil {
		msgs = []storage.Message{}
	}
	writeJSON(w, 200, map[string]any{"conversation": c, "messages": msgs})
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	conv, err := s.DB.GetConversation(r.PathValue("id"))
	if err != nil || conv.OwnerID != userOf(r) {
		writeErr(w, 404, "not_found", "conversation not found")
		return
	}
	var body struct {
		Content   string `json:"content"`
		Idem      string `json:"idempotency_key"`
		Workspace string `json:"workspace"`
	}
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.Content) == "" {
		writeErr(w, 400, "bad_json", "content is required")
		return
	}
	if body.Idem != "" {
		if id, st, ok := s.DB.TaskByIdem(body.Idem); ok {
			writeJSON(w, 200, map[string]any{"conversation_id": conv.ID, "task_id": id, "status": st, "idempotent": true})
			return
		}
	}
	msg, err := s.DB.AddMessage(conv.ID, "user", body.Content, "")
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	spec := protocol.TaskSpec{
		TaskID: storage.NewID("task"), Kind: "builtin", Prompt: body.Content,
		Workspace: body.Workspace, TimeoutSec: 120, Level: protocol.LevelReadOnly,
		Idempotency: body.Idem,
	}
	if spec.Idempotency == "" {
		spec.Idempotency = storage.NewID("idem")
	}
	if err := s.DB.InsertOwnedTask(spec, userOf(r), conv.ID, body.Content, false); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	row, _, _ := s.DB.GetTask(spec.TaskID)
	if s.jobs != nil {
		s.jobs.Start(row, spec)
	}
	s.DB.Audit(userOf(r), "", spec.TaskID, "", "conversation.message", "", "ok", conv.ID)
	writeJSON(w, 201, map[string]any{
		"conversation_id": conv.ID, "message_id": msg.ID, "task_id": spec.TaskID, "status": protocol.TaskQueued,
	})
}

func (s *Server) taskEventsCursor(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	ev, err := s.DB.EventsAfter(r.PathValue("id"), after)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if ev == nil {
		ev = []protocol.Event{}
	}
	writeJSON(w, 200, map[string]any{"events": ev, "after": after})
}

func (s *Server) taskRuns(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Runs(r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if rows == nil {
		rows = []storage.RunRow{}
	}
	writeJSON(w, 200, map[string]any{"runs": rows})
}

func (s *Server) wireJobs() {
	s.jobs = supervisor.New(context.Background(), s.DB, s.executeBuiltin)
}

func (s *Server) executeBuiltin(ctx context.Context, row storage.TaskRow, spec protocol.TaskSpec) protocol.TaskResult {
	seq, _ := s.DB.NextEventSeq(row.ID)
	_ = s.DB.AppendEventOnce(protocol.Event{
		EventType: protocol.EvRunStarted, Sequence: seq, Timestamp: time.Now().UTC(),
		TaskID: row.ID, Origin: "omnoda", Payload: map[string]any{"kind": spec.Kind},
	})
	if spec.Kind != "builtin" && spec.Kind != "" {
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: "supervisor handles builtin goals; node tasks stay on the node runner", ExitCode: 1}
	}
	if s.Caps == nil {
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: "no local capability registry", ExitCode: 1}
	}
	select {
	case <-ctx.Done():
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskCancelled, Error: "cancelled by user", ExitCode: 130}
	default:
	}
	ex := executor.Executor{Reg: s.Caps}
	res := ex.Invoke(ctx, executor.Request{ID: "omnoda.system-info", Workspace: spec.Workspace})
	if !res.OK {
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: res.Error, ExitCode: res.ExitCode}
	}
	summary := res.Stdout
	if summary == "" {
		summary = "system-info"
	}
	if conv := messageConv(s, row.ID); conv != "" {
		_, _ = s.DB.AddMessage(conv, "assistant", summary, row.ID)
	}
	return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskSucceeded, Summary: summary, ExitCode: 0}
}

func messageConv(s *Server, taskID string) string {
	conv, _, _, _, err := s.DB.TaskMeta(taskID)
	if err != nil {
		return ""
	}
	return conv
}
