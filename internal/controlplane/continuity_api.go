package controlplane

import (
	"context"
	"errors"
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
	intent := classifySeed(body.Content)
	spec := protocol.TaskSpec{
		Kind: "builtin", Prompt: body.Content,
		Workspace: body.Workspace, TimeoutSec: 120, Level: protocol.LevelReadOnly,
		Idempotency: body.Idem, Command: intent.Kind,
	}
	if intent.Kind == "builtin" {
		spec.Command = intent.Capability
	}
	if intent.SideEffect {
		spec.Level = protocol.LevelWorkspaceWrite
	}
	msg, spec, replayed, err := s.DB.CreateMessageTaskReplay(conv.ID, userOf(r), body.Content, spec, intent.SideEffect)
	if errors.Is(err, storage.ErrIdempotencyConflict) {
		writeErr(w, 409, "idempotency_conflict", err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	row, _, _ := s.DB.GetTask(spec.TaskID)
	// A committed retry returns the original task. It does not start a second run.
	if !replayed && row.Status == protocol.TaskQueued && s.jobs != nil {
		s.jobs.Start(row, spec)
	}
	s.DB.Audit(userOf(r), "", spec.TaskID, "", "conversation.message", "", "ok", conv.ID)
	status := 201
	bodyOut := map[string]any{
		"conversation_id": conv.ID, "message_id": msg.ID, "task_id": spec.TaskID, "status": row.Status,
		"intent": intent.Kind,
	}
	if replayed {
		status = 200
		bodyOut["idempotent"] = true
		bodyOut["status"] = row.Status
	}
	writeJSON(w, status, bodyOut)
}

func (s *Server) taskEventsCursor(w http.ResponseWriter, r *http.Request) {
	if !s.allowTask(w, r) {
		return
	}
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
	if !s.allowTask(w, r) {
		return
	}
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

func (s *Server) executeBuiltin(ctx context.Context, row storage.TaskRow, spec protocol.TaskSpec) (result protocol.TaskResult) {
	defer func() {
		if rec := recover(); rec != nil {
			result = protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: "builtin runner panicked", ExitCode: 1}
		}
	}()
	seq, _ := s.DB.NextEventSeq(row.ID)
	_ = s.DB.AppendEventOnce(protocol.Event{
		EventType: protocol.EvRunStarted, Sequence: seq, Timestamp: time.Now().UTC(),
		TaskID: row.ID, Origin: "omnoda", Payload: map[string]any{"kind": spec.Kind, "intent": spec.Command},
	})
	select {
	case <-ctx.Done():
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskCancelled, Error: "cancelled by user", ExitCode: 130}
	default:
	}
	intent := classifySeed(spec.Prompt)
	switch intent.Kind {
	case "builtin":
		if s.Caps == nil {
			return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: "no local capability registry", ExitCode: 1}
		}
		ex := executor.Executor{Reg: s.Caps}
		res := ex.Invoke(ctx, executor.Request{ID: intent.Capability, Workspace: spec.Workspace})
		if !res.OK {
			return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: res.Error, ExitCode: res.ExitCode}
		}
		summary := res.Stdout
		if summary == "" {
			summary = intent.Capability
		}
		s.note(row.ID, summary)
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskSucceeded, Summary: summary, ExitCode: 0}
	case "missing":
		msg := "capability_missing: " + intent.Reason
		s.note(row.ID, msg)
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: msg, ExitCode: 1}
	case "model":
		if !s.modelConfigured() {
			msg := "no_model: configure a model provider before asking Seed to plan or explain"
			s.note(row.ID, msg)
			return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: msg, ExitCode: 1}
		}
		msg := "model_required: Seed recorded the goal. Open Advanced to run the configured model. Seed does not start a second agent harness."
		s.note(row.ID, msg)
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: msg, ExitCode: 1}
	case "approval":
		_, _ = s.DB.CreateApproval(storage.Approval{TaskID: row.ID, Action: "seed.side_effect", ArgsJSON: spec.Prompt}, 30*time.Minute)
		msg := "approval_required: " + intent.Reason
		s.note(row.ID, msg)
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: msg, ExitCode: 1}
	default:
		msg := "unsupported: " + intent.Reason
		s.note(row.ID, msg)
		return protocol.TaskResult{TaskID: row.ID, Status: protocol.TaskFailed, Error: msg, ExitCode: 1}
	}
}

func (s *Server) note(taskID, summary string) {
	if conv := messageConv(s, taskID); conv != "" {
		_, _ = s.DB.AddMessage(conv, "assistant", summary, taskID)
	}
}

func (s *Server) modelConfigured() bool {
	mc, enc, err := s.DB.LoadModel()
	return err == nil && enc != "" && mc.BaseURL != ""
}

func (s *Server) allowTask(w http.ResponseWriter, r *http.Request) bool {
	ok, err := s.DB.OwnsTask(r.PathValue("id"), userOf(r))
	if err != nil || !ok {
		writeErr(w, 404, "not_found", "task not found")
		return false
	}
	return true
}

func messageConv(s *Server, taskID string) string {
	conv, _, _, _, err := s.DB.TaskMeta(taskID)
	if err != nil {
		return ""
	}
	return conv
}
