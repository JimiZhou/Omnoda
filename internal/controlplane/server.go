// Package controlplane is the single-process Omnoda server:
// HTTP API, session auth, node registry, and the outbound-node websocket hub.
package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/JimiZhou/Omnoda/internal/auth"
	"github.com/JimiZhou/Omnoda/internal/identity"
	"github.com/JimiZhou/Omnoda/internal/planner"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/scheduler"
	"github.com/JimiZhou/Omnoda/internal/storage"
	"github.com/JimiZhou/Omnoda/internal/workflow"
	"github.com/gorilla/websocket"
)

type Server struct {
	DB        *storage.DB
	DataDir   string
	PublicURL string
	Version   string
	SealKey   []byte
	hub       *Hub
	engine    *workflow.Engine
	mux       *http.ServeMux
	upgrader  websocket.Upgrader
}

func New(db *storage.DB, dataDir, publicURL, version string) (*Server, error) {
	key, err := loadSeal(dataDir)
	if err != nil {
		return nil, err
	}
	s := &Server{
		DB: db, DataDir: dataDir, PublicURL: strings.TrimRight(publicURL, "/"), Version: version, SealKey: key,
		hub: newHub(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // node auth is cryptographic, not cookie origin
		},
	}
	s.engine = workflow.New(db, &hubRunner{s: s})
	s.engine.Load = s.candidates
	s.mux = http.NewServeMux()
	s.routes()
	if _, err := db.RecoverInflight(); err != nil {
		return nil, err
	}
	_ = db.MarkOfflineAll()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.withSecurityHeaders(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.health)
	s.mux.HandleFunc("GET /api/v1/meta", s.meta)
	s.mux.HandleFunc("POST /api/v1/setup", s.setup)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	s.mux.HandleFunc("GET /api/v1/auth/me", s.me)
	s.mux.HandleFunc("GET /api/v1/nodes", s.authed(s.listNodes))
	s.mux.HandleFunc("GET /api/v1/nodes/{id}", s.authed(s.getNode))
	s.mux.HandleFunc("DELETE /api/v1/nodes/{id}", s.authed(s.deleteNode))
	s.mux.HandleFunc("GET /api/v1/nodes/{id}/capabilities", s.authed(s.nodeCaps))
	s.mux.HandleFunc("POST /api/v1/nodes/enrollment-codes", s.authed(s.createEnroll))
	s.mux.HandleFunc("POST /api/v1/tasks", s.authed(s.createTask))
	s.mux.HandleFunc("GET /api/v1/tasks", s.authed(s.listTasks))
	s.mux.HandleFunc("GET /api/v1/tasks/{id}", s.authed(s.getTask))
	s.mux.HandleFunc("GET /api/v1/tasks/{id}/events", s.authed(s.taskEvents))
	s.mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.authed(s.cancelTask))
	s.mux.HandleFunc("POST /api/v1/workflows", s.authed(s.createWorkflow))
	s.mux.HandleFunc("GET /api/v1/workflows", s.authed(s.listWorkflows))
	s.mux.HandleFunc("GET /api/v1/workflows/{id}", s.authed(s.getWorkflow))
	s.mux.HandleFunc("POST /api/v1/workflows/{id}/cancel", s.authed(s.cancelWorkflow))
	s.mux.HandleFunc("GET /api/v1/approvals", s.authed(s.listApprovals))
	s.mux.HandleFunc("POST /api/v1/approvals/{id}/approve", s.authed(s.decideApproval("approved")))
	s.mux.HandleFunc("POST /api/v1/approvals/{id}/reject", s.authed(s.decideApproval("rejected")))
	s.mux.HandleFunc("GET /api/v1/artifacts/{id}", s.authed(s.getArtifact))
	s.mux.HandleFunc("GET /api/v1/audit", s.authed(s.audit))
	s.mux.HandleFunc("GET /api/v1/settings/model", s.authed(s.getModel))
	s.mux.HandleFunc("PUT /api/v1/settings/model", s.authed(s.putModel))
	s.mux.HandleFunc("GET /api/v1/events", s.authed(s.eventStream))
	s.mux.HandleFunc("GET /api/v1/node/ws", s.nodeWS)
	s.mux.HandleFunc("GET /install.sh", s.installSh)
	s.mux.HandleFunc("GET /install.ps1", s.installPS)
	s.mux.HandleFunc("GET /releases/{file}", s.releaseFile)
	s.mux.Handle("/", s.static())
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self' ws: wss:; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": s.Version, "time": time.Now().UTC()})
}

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	n, _ := s.DB.UserCount()
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.Version, "setup_required": n == 0, "public_url": s.PublicURL,
		"product": "Omnoda", "slogan": "Every Device. One Mind.",
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	n, err := s.DB.UserCount()
	if err != nil || n > 0 {
		writeErr(w, http.StatusConflict, "setup_done", "administrator already exists")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &body); err != nil || len(body.Password) < 10 || body.Username == "" {
		writeErr(w, http.StatusBadRequest, "bad_setup", "username and a password of at least 10 characters are required")
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hash", err.Error())
		return
	}
	u, err := s.DB.CreateUser(body.Username, hash)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "create_user", err.Error())
		return
	}
	s.DB.Audit(u.Username, "", "", "", "setup", "", "ok", "")
	writeJSON(w, http.StatusCreated, map[string]any{"username": u.Username})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", "invalid json")
		return
	}
	u, hash, err := s.DB.UserByName(body.Username)
	if err != nil || auth.CheckPassword(hash, body.Password) != nil {
		s.DB.Audit(body.Username, "", "", "", "login", "", "denied", "")
		writeErr(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
		return
	}
	ses, err := s.DB.CreateSession(u.ID, 12*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session", err.Error())
		return
	}
	secure := r.TLS != nil || strings.HasPrefix(s.PublicURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name: "omnoda_session", Value: ses.ID, Path: "/", HttpOnly: true,
		Secure: secure, SameSite: http.SameSiteLaxMode, Expires: ses.ExpiresAt,
	})
	s.DB.Audit(u.Username, "", "", "", "login", "", "ok", "")
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "csrf": ses.CSRF, "expires_at": ses.ExpiresAt})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("omnoda_session"); err == nil {
		_ = s.DB.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "omnoda_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	ses, err := s.session(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "login required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_id": ses.UserID, "csrf": ses.CSRF})
}

func (s *Server) session(r *http.Request) (storage.Session, error) {
	c, err := r.Cookie("omnoda_session")
	if err != nil || c.Value == "" {
		return storage.Session{}, errors.New("no session")
	}
	return s.DB.Session(c.Value)
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ses, err := s.session(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "login required")
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			origin := r.Header.Get("Origin")
			if origin != "" && !s.originOK(r, origin) {
				writeErr(w, http.StatusForbidden, "origin", "cross-origin write blocked")
				return
			}
			csrf := r.Header.Get("X-CSRF-Token")
			if subtle.ConstantTimeCompare([]byte(csrf), []byte(ses.CSRF)) != 1 {
				writeErr(w, http.StatusForbidden, "csrf", "missing or invalid csrf token")
				return
			}
		}
		ctx := context.WithValue(r.Context(), ctxUser, ses.UserID)
		next(w, r.WithContext(ctx))
	}
}

type ctxKey int

const ctxUser ctxKey = 1

func (s *Server) originOK(r *http.Request, origin string) bool {
	if s.PublicURL != "" && strings.HasPrefix(origin, s.PublicURL) {
		return true
	}
	host := r.Host
	return strings.Contains(origin, "://"+host)
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.DB.ListNodes()
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if nodes == nil {
		nodes = []storage.Node{}
	}
	for i := range nodes {
		if nodes[i].Status != protocol.NodeRevoked && !s.hub.online(nodes[i].ID) {
			if nodes[i].Status == protocol.NodeOnline {
				nodes[i].Status = protocol.NodeOffline
			}
		}
	}
	writeJSON(w, 200, map[string]any{"nodes": nodes})
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.DB.GetNode(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", "node not found")
		return
	}
	writeJSON(w, 200, n)
}

func (s *Server) nodeCaps(w http.ResponseWriter, r *http.Request) {
	n, err := s.DB.GetNode(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", "node not found")
		return
	}
	writeJSON(w, 200, map[string]any{"capabilities": n.Capabilities, "harnesses": n.Harnesses, "workspaces": n.Workspaces})
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.DB.RevokeNode(id); err != nil {
		writeErr(w, 404, "not_found", "node not found")
		return
	}
	s.hub.drop(id)
	s.DB.Audit(userOf(r), id, "", "", "node.revoke", "", "ok", "")
	writeJSON(w, 200, map[string]any{"revoked": id})
}

func (s *Server) createEnroll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DisplayName string `json:"display_name"`
		OS          string `json:"os"`
	}
	_ = decode(r, &body)
	if body.DisplayName == "" {
		body.DisplayName = "New node"
	}
	plain, e, err := s.DB.CreateEnrollment(body.DisplayName, body.OS, 10*time.Minute)
	if err != nil {
		writeErr(w, 500, "enroll", err.Error())
		return
	}
	base := s.PublicURL
	if base == "" {
		base = "http://" + r.Host
	}
	sh := fmt.Sprintf("curl -fsSL %s/install.sh | sh -s -- --server %s --code %s", base, base, plain)
	ps := fmt.Sprintf("irm %s/install.ps1 | iex; Install-Omnoda -Server %s -Code %s", base, base, plain)
	s.DB.Audit(userOf(r), "", "", "", "enrollment.create", "", "ok", e.ID)
	writeJSON(w, 201, map[string]any{
		"id": e.ID, "display_name": e.DisplayName, "expires_at": e.ExpiresAt,
		"code": plain, "install_sh": sh, "install_ps": ps, "server": base,
	})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		NodeID    string             `json:"node_id"`
		Kind      string             `json:"kind"`
		Command   string             `json:"command"`
		Args      []string           `json:"args"`
		Prompt    string             `json:"prompt"`
		HarnessID string             `json:"harness_id"`
		Workspace string             `json:"workspace"`
		Timeout   int                `json:"timeout_sec"`
		Level     protocol.ExecLevel `json:"level"`
		Idem      string             `json:"idempotency_key"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, 400, "bad_json", "invalid json")
		return
	}
	if body.Idem != "" {
		if id, st, ok := s.DB.TaskByIdem(body.Idem); ok {
			writeJSON(w, 200, map[string]any{"task_id": id, "status": st, "idempotent": true})
			return
		}
	}
	node, err := s.DB.GetNode(body.NodeID)
	if err != nil {
		writeErr(w, 404, "not_found", "node not found")
		return
	}
	if node.Status == protocol.NodeRevoked || !s.hub.online(node.ID) {
		writeErr(w, 409, "node_offline", "node is offline or revoked")
		return
	}
	if body.Level == "" {
		body.Level = protocol.LevelReadOnly
	}
	if body.Level == protocol.LevelPrivileged {
		writeErr(w, 403, "privileged", "privileged execution is not enabled from the API")
		return
	}
	if body.Kind == "" {
		body.Kind = "command"
	}
	if body.Timeout <= 0 {
		body.Timeout = 60
	}
	if body.Workspace == "" && len(node.Workspaces) > 0 {
		body.Workspace = node.Workspaces[0]
	}
	spec := protocol.TaskSpec{
		TaskID: storage.NewID("task"), Kind: body.Kind, Command: body.Command, Args: body.Args,
		Prompt: body.Prompt, HarnessID: body.HarnessID, Workspace: body.Workspace,
		TimeoutSec: body.Timeout, Level: body.Level, Idempotency: body.Idem,
	}
	if spec.Idempotency == "" {
		spec.Idempotency = storage.NewID("idem")
	}
	if err := s.DB.InsertTask(spec); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	_ = s.DB.BindTaskNode(spec.TaskID, node.ID)
	s.DB.Audit(userOf(r), node.ID, spec.TaskID, "", "task.create", string(body.Level), "ok", body.Kind)
	if err := s.hub.assign(node.ID, spec); err != nil {
		_ = s.DB.SetTaskStatus(spec.TaskID, protocol.TaskFailed, 1, "", "", err.Error())
		writeErr(w, 409, "dispatch", err.Error())
		return
	}
	_ = s.DB.SetTaskStatus(spec.TaskID, protocol.TaskRunning, 0, "", "", "")
	writeJSON(w, 201, map[string]any{"task_id": spec.TaskID, "status": protocol.TaskRunning, "node_id": node.ID})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.ListTasks(100)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if rows == nil {
		rows = []storage.TaskRow{}
	}
	writeJSON(w, 200, map[string]any{"tasks": rows})
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, spec, err := s.DB.GetTask(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", "task not found")
		return
	}
	writeJSON(w, 200, map[string]any{"task": t, "spec": spec})
}

func (s *Server) taskEvents(w http.ResponseWriter, r *http.Request) {
	ev, err := s.DB.Events(r.PathValue("id"))
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if ev == nil {
		ev = []protocol.Event{}
	}
	writeJSON(w, 200, map[string]any{"events": ev})
}

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	t, _, err := s.DB.GetTask(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", "task not found")
		return
	}
	if t.NodeID != "" {
		_ = s.hub.cancel(t.NodeID, t.ID)
	}
	if t.Status == protocol.TaskRunning || t.Status == protocol.TaskQueued {
		_ = s.DB.SetTaskStatus(t.ID, protocol.TaskCancelled, 130, "cancelled by user", t.Output, "")
	}
	s.DB.Audit(userOf(r), t.NodeID, t.ID, t.WorkflowID, "task.cancel", "", "ok", "")
	writeJSON(w, 200, map[string]any{"task_id": t.ID, "status": protocol.TaskCancelled})
}

func (s *Server) createWorkflow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Goal    string `json:"goal"`
		Planner string `json:"planner"` // auto | template | llm
	}
	if err := decode(r, &body); err != nil || strings.TrimSpace(body.Goal) == "" {
		writeErr(w, 400, "bad_json", "goal is required")
		return
	}
	nodes, _ := s.DB.ListNodes()
	var views []planner.NodeView
	for _, n := range nodes {
		if n.Status == protocol.NodeRevoked {
			continue
		}
		var hs []string
		for _, h := range n.Harnesses {
			if h.Present {
				hs = append(hs, h.ID)
			}
		}
		views = append(views, planner.NodeView{ID: n.ID, Name: n.DisplayName, OS: n.OS, Arch: n.Arch, Status: string(n.Status), Labels: n.Labels, Harnesses: hs})
	}
	var llm *planner.LLMConfig
	if body.Planner == "llm" || body.Planner == "auto" || body.Planner == "" {
		mc, enc, _ := s.DB.LoadModel()
		if enc != "" && mc.BaseURL != "" {
			key, err := auth.Open(s.SealKey, enc)
			if err == nil {
				llm = &planner.LLMConfig{BaseURL: mc.BaseURL, Model: mc.Model, APIKey: key}
			}
		}
	}
	prefer := body.Planner
	if prefer == "" || prefer == "auto" {
		if llm == nil {
			prefer = "template"
		} else {
			prefer = "llm"
		}
	}
	plan, err := planner.Plan(r.Context(), planner.Request{Goal: body.Goal, Nodes: views, Prefer: prefer}, llm)
	if err != nil {
		writeErr(w, 422, "planner", err.Error())
		return
	}
	if err := planner.Validate(plan); err != nil {
		writeErr(w, 422, "plan_invalid", err.Error())
		return
	}
	// Re-check: planner must not name a revoked node.
	for _, st := range plan.Steps {
		if st.Selector.NodeID != "" {
			n, err := s.DB.GetNode(st.Selector.NodeID)
			if err != nil || n.Status == protocol.NodeRevoked {
				writeErr(w, 422, "plan_invalid", "plan references a missing or revoked node")
				return
			}
		}
	}
	res, err := s.engine.Start(r.Context(), plan)
	if err != nil {
		writeErr(w, 500, "workflow", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", res.ID, "workflow.create", "", "ok", plan.Planner)
	writeJSON(w, 201, map[string]any{"workflow_id": res.ID, "planner": plan.Planner, "plan": plan})
}

func (s *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.ListWorkflows(50)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if rows == nil {
		rows = []storage.WorkflowRow{}
	}
	writeJSON(w, 200, map[string]any{"workflows": rows})
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, err := s.DB.GetWorkflow(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", "workflow not found")
		return
	}
	steps, _ := s.DB.Steps(wf.ID)
	if steps == nil {
		steps = []storage.StepRow{}
	}
	var plan protocol.Plan
	_ = json.Unmarshal([]byte(wf.PlanJSON), &plan)
	var report any
	_ = json.Unmarshal([]byte(wf.Report), &report)
	writeJSON(w, 200, map[string]any{"workflow": wf, "plan": plan, "steps": steps, "report": report})
}

func (s *Server) cancelWorkflow(w http.ResponseWriter, r *http.Request) {
	s.engine.Cancel(r.PathValue("id"))
	writeJSON(w, 200, map[string]any{"cancelled": r.PathValue("id")})
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.ListApprovals(r.URL.Query().Get("status"))
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if rows == nil {
		rows = []storage.Approval{}
	}
	writeJSON(w, 200, map[string]any{"approvals": rows})
}

func (s *Server) decideApproval(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := s.DB.DecideApproval(id, status); err != nil {
			writeErr(w, 409, "approval", err.Error())
			return
		}
		s.DB.Audit(userOf(r), "", "", "", "approval."+status, "", "ok", id)
		writeJSON(w, 200, map[string]any{"id": id, "status": status})
	}
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	a, err := s.DB.GetArtifact(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", "artifact not found")
		return
	}
	if a.Expires != "" {
		if t, err := time.Parse(time.RFC3339Nano, a.Expires); err == nil && time.Now().After(t) {
			writeErr(w, 410, "expired", "artifact expired")
			return
		}
	}
	writeJSON(w, 200, a)
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.ListAudit(200)
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	if rows == nil {
		rows = []storage.AuditRow{}
	}
	writeJSON(w, 200, map[string]any{"audit": rows})
}

func (s *Server) getModel(w http.ResponseWriter, r *http.Request) {
	m, _, err := s.DB.LoadModel()
	if err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	m.APIKey = ""
	writeJSON(w, 200, m)
}

func (s *Server) putModel(w http.ResponseWriter, r *http.Request) {
	var m storage.ModelConfig
	if err := decode(r, &m); err != nil {
		writeErr(w, 400, "bad_json", "invalid json")
		return
	}
	enc := ""
	if m.APIKey != "" {
		enc = auth.Seal(s.SealKey, []byte(m.APIKey))
	}
	if m.MaxSteps <= 0 {
		m.MaxSteps = 8
	}
	if m.TimeoutSec <= 0 {
		m.TimeoutSec = 120
	}
	if m.TokenBudget <= 0 {
		m.TokenBudget = 8000
	}
	if err := s.DB.SaveModel(m, enc); err != nil {
		writeErr(w, 500, "db", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "settings.model", "", "ok", m.BaseURL)
	m.APIKey = ""
	m.Configured = true
	writeJSON(w, 200, m)
}

func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "sse", "streaming unsupported")
		return
	}
	id, ch := s.hub.subscribe()
	defer s.hub.unsubscribe(id)
	fmt.Fprintf(w, "event: hello\ndata: {\"ok\":true}\n\n")
	fl.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: task\ndata: %s\n\n", b)
			fl.Flush()
		}
	}
}

func (s *Server) candidates() []scheduler.Candidate {
	nodes, _ := s.DB.ListNodes()
	var out []scheduler.Candidate
	for _, n := range nodes {
		if s.hub.online(n.ID) {
			n.Status = protocol.NodeOnline
		} else if n.Status != protocol.NodeRevoked {
			n.Status = protocol.NodeOffline
		}
		out = append(out, scheduler.Candidate{Node: n, Running: s.hub.running(n.ID)})
	}
	return out
}

func userOf(r *http.Request) string {
	if v, ok := r.Context().Value(ctxUser).(string); ok {
		return v
	}
	return ""
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, c, msg string) {
	writeJSON(w, code, protocol.ErrorBody{Code: c, Message: msg})
}

func loadSeal(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "seal.key")
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 32 {
		return b[:32], nil
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, k, 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Server) static() http.Handler {
	// Filled by embed in web_static.go if present; otherwise a tiny fallback.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/install.sh" {
			http.NotFound(w, r)
			return
		}
		s.serveUI(w, r)
	})
}

// --- node websocket ---

func (s *Server) nodeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	var hello protocol.Envelope
	if err := conn.ReadJSON(&hello); err != nil || hello.Type != protocol.MsgHello {
		return
	}
	var h protocol.NodeHello
	raw, _ := json.Marshal(hello.Payload)
	if json.Unmarshal(raw, &h) != nil || h.NodeID == "" || h.PublicKey == "" {
		_ = conn.WriteJSON(errEnv("bad_hello", "missing identity"))
		return
	}
	pub, err := identity.ParsePublic(h.PublicKey)
	if err != nil || identity.NodeID(pub) != h.NodeID {
		_ = conn.WriteJSON(errEnv("bad_identity", "node id does not match public key"))
		return
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	nonceB64 := base64.StdEncoding.EncodeToString(nonce)
	_ = conn.WriteJSON(protocol.Envelope{V: protocol.WireVersion, Type: "node.challenge", Timestamp: time.Now().UTC(), Payload: map[string]any{"nonce": nonceB64}})
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	var ch protocol.Envelope
	if err := conn.ReadJSON(&ch); err != nil || ch.Type != "node.challenge_response" {
		return
	}
	sig, _ := ch.Payload["signature"].(string)
	if identity.Verify(pub, []byte(nonceB64), sig) != nil {
		_ = conn.WriteJSON(errEnv("bad_signature", "challenge failed"))
		return
	}
	// Enrollment or known node.
	existing, err := s.DB.GetNode(h.NodeID)
	if errors.Is(err, sql.ErrNoRows) {
		code, _ := ch.Payload["enrollment_code"].(string)
		enr, err := s.DB.ConsumeEnrollment(code)
		if err != nil {
			_ = conn.WriteJSON(errEnv("enrollment", err.Error()))
			return
		}
		name := h.DisplayName
		if enr.DisplayName != "" && name == "" {
			name = enr.DisplayName
		}
		if name == "" {
			name = h.Hostname
		}
		now := time.Now().UTC()
		n := storage.Node{
			ID: h.NodeID, DisplayName: name, OS: h.OS, Arch: h.Arch, Version: h.Version, Hostname: h.Hostname,
			Status: protocol.NodeOnline, PublicKey: h.PublicKey, Labels: h.Labels,
			Capabilities: h.Capabilities, Harnesses: h.Harnesses, Workspaces: h.Workspaces, LastSeen: &now,
		}
		if n.Labels == nil {
			n.Labels = map[string]string{}
		}
		if err := s.DB.UpsertNode(n); err != nil {
			_ = conn.WriteJSON(errEnv("db", err.Error()))
			return
		}
		s.DB.Audit("node", h.NodeID, "", "", "node.enroll", "", "ok", "")
	} else if err != nil {
		_ = conn.WriteJSON(errEnv("db", err.Error()))
		return
	} else {
		if existing.Status == protocol.NodeRevoked || existing.PublicKey != h.PublicKey {
			_ = conn.WriteJSON(errEnv("revoked", "node is revoked or key does not match"))
			return
		}
		now := time.Now().UTC()
		_ = s.DB.UpdateNodeInventory(h.NodeID, h.Capabilities, h.Harnesses, h.Workspaces, h.OS, h.Arch, h.Version, h.Hostname, h.DisplayName, h.Labels)
		_ = s.DB.TouchNode(h.NodeID, protocol.NodeOnline)
		_ = now
	}
	_ = conn.WriteJSON(protocol.Envelope{V: protocol.WireVersion, Type: protocol.MsgHelloAck, Timestamp: time.Now().UTC(), NodeID: h.NodeID, Payload: map[string]any{"ok": true}})
	s.hub.attach(h.NodeID, conn)
	defer s.hub.detach(h.NodeID, conn)
	_ = s.DB.TouchNode(h.NodeID, protocol.NodeOnline)
	log.Printf("node connected %s", h.NodeID)
	s.readNode(h.NodeID, conn)
	_ = s.DB.TouchNode(h.NodeID, protocol.NodeOffline)
	// If the node was revoked while connected, keep revoked.
	if n, err := s.DB.GetNode(h.NodeID); err == nil && n.Status != protocol.NodeRevoked {
		_, _ = s.DB.SQL().Exec(`UPDATE nodes SET status='offline' WHERE id=? AND status!='revoked'`, h.NodeID)
	}
}

func (s *Server) readNode(id string, conn *websocket.Conn) {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			return
		}
		switch env.Type {
		case protocol.MsgHeartbeat:
			_ = s.DB.TouchNode(id, protocol.NodeOnline)
			_ = s.hub.send(id, protocol.Envelope{V: 1, Type: protocol.MsgHeartbeatAck, Timestamp: time.Now().UTC()})
		case protocol.MsgTaskEvent:
			var ev protocol.Event
			b, _ := json.Marshal(env.Payload["event"])
			if json.Unmarshal(b, &ev) == nil {
				if ev.TaskID == "" {
					ev.TaskID, _ = env.Payload["task_id"].(string)
				}
				ev.NodeID = id
				_ = s.DB.AppendEvent(ev)
				s.hub.broadcast(ev)
			}
		case protocol.MsgTaskResult:
			var res protocol.TaskResult
			b, _ := json.Marshal(env.Payload)
			if json.Unmarshal(b, &res) == nil {
				_ = s.DB.SetTaskStatus(res.TaskID, res.Status, res.ExitCode, res.Summary, res.Output, res.Error)
				s.hub.finish(res.TaskID, res)
				s.hub.broadcast(protocol.Event{EventType: protocol.EvRunCompleted, TaskID: res.TaskID, NodeID: id, Timestamp: time.Now().UTC(), Payload: map[string]any{"status": res.Status, "summary": res.Summary}})
			}
		case protocol.MsgCancelAck:
			// recorded via result
		default:
		}
	}
}

func errEnv(code, msg string) protocol.Envelope {
	return protocol.Envelope{V: 1, Type: protocol.MsgError, Timestamp: time.Now().UTC(), Error: &protocol.ErrorBody{Code: code, Message: msg}}
}

// Hub tracks live sockets and in-flight task waiters.

type Hub struct {
	mu       sync.Mutex
	conns    map[string]*websocket.Conn
	waiters  map[string]chan protocol.TaskResult
	subs     map[int]chan protocol.Event
	nextSub  int
	inflight map[string]int
}

func newHub() *Hub {
	return &Hub{
		conns: map[string]*websocket.Conn{}, waiters: map[string]chan protocol.TaskResult{},
		subs: map[int]chan protocol.Event{}, inflight: map[string]int{},
	}
}

func (h *Hub) online(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[id]
	return ok
}

func (h *Hub) running(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inflight[id]
}

func (h *Hub) attach(id string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.conns[id]; ok && old != c {
		_ = old.Close()
	}
	h.conns[id] = c
}

func (h *Hub) detach(id string, c *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.conns[id]; ok && cur == c {
		delete(h.conns, id)
	}
}

func (h *Hub) drop(id string) {
	h.mu.Lock()
	c := h.conns[id]
	delete(h.conns, id)
	h.mu.Unlock()
	if c != nil {
		_ = c.WriteJSON(protocol.Envelope{V: 1, Type: "node.revoked", Timestamp: time.Now().UTC()})
		_ = c.Close()
	}
}

func (h *Hub) send(id string, env protocol.Envelope) error {
	h.mu.Lock()
	c := h.conns[id]
	h.mu.Unlock()
	if c == nil {
		return errors.New("node offline")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return c.WriteJSON(env)
}

func (h *Hub) assign(id string, spec protocol.TaskSpec) error {
	h.mu.Lock()
	if h.inflight[id] >= 2 {
		h.mu.Unlock()
		return errors.New("node concurrency limit reached")
	}
	c := h.conns[id]
	if c == nil {
		h.mu.Unlock()
		return errors.New("node offline")
	}
	h.inflight[id]++
	ch := make(chan protocol.TaskResult, 1)
	h.waiters[spec.TaskID] = ch
	err := c.WriteJSON(protocol.Envelope{V: 1, Type: protocol.MsgTaskAssign, RequestID: spec.Idempotency, Timestamp: time.Now().UTC(), Payload: map[string]any{"task": spec}})
	h.mu.Unlock()
	if err != nil {
		h.finish(spec.TaskID, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, Error: err.Error()})
		return err
	}
	return nil
}

func (h *Hub) cancel(id, taskID string) error {
	return h.send(id, protocol.Envelope{V: 1, Type: protocol.MsgTaskCancel, Timestamp: time.Now().UTC(), Payload: map[string]any{"task_id": taskID}})
}

func (h *Hub) finish(taskID string, res protocol.TaskResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.waiters[taskID]; ok {
		select {
		case ch <- res:
		default:
		}
		delete(h.waiters, taskID)
	}
}

func (h *Hub) release(nodeID string) {
	h.mu.Lock()
	if h.inflight[nodeID] > 0 {
		h.inflight[nodeID]--
	}
	h.mu.Unlock()
}

func (h *Hub) wait(taskID string, timeout time.Duration) (protocol.TaskResult, error) {
	h.mu.Lock()
	ch := h.waiters[taskID]
	h.mu.Unlock()
	if ch == nil {
		return protocol.TaskResult{}, errors.New("no waiter")
	}
	select {
	case res := <-ch:
		return res, nil
	case <-time.After(timeout):
		return protocol.TaskResult{TaskID: taskID, Status: protocol.TaskUncertain, Error: "result ack timeout; not replayed"}, nil
	}
}

func (h *Hub) subscribe() (int, chan protocol.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextSub++
	id := h.nextSub
	ch := make(chan protocol.Event, 32)
	h.subs[id] = ch
	return id, ch
}

func (h *Hub) unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subs[id]; ok {
		close(ch)
		delete(h.subs, id)
	}
}

func (h *Hub) broadcast(ev protocol.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

type hubRunner struct{ s *Server }

func (r *hubRunner) Run(ctx context.Context, nodeID string, spec protocol.TaskSpec) (protocol.TaskResult, error) {
	if err := r.s.DB.InsertTask(spec); err != nil {
		return protocol.TaskResult{}, err
	}
	_ = r.s.DB.BindTaskNode(spec.TaskID, nodeID)
	if err := r.s.hub.assign(nodeID, spec); err != nil {
		_ = r.s.DB.SetTaskStatus(spec.TaskID, protocol.TaskFailed, 1, "", "", err.Error())
		return protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, Error: err.Error()}, nil
	}
	_ = r.s.DB.SetTaskStatus(spec.TaskID, protocol.TaskRunning, 0, "", "", "")
	timeout := time.Duration(spec.TimeoutSec+15) * time.Second
	done := make(chan protocol.TaskResult, 1)
	go func() {
		res, err := r.s.hub.wait(spec.TaskID, timeout)
		if err != nil {
			res = protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskUncertain, Error: err.Error()}
		}
		done <- res
	}()
	select {
	case <-ctx.Done():
		_ = r.s.hub.cancel(nodeID, spec.TaskID)
		r.s.hub.release(nodeID)
		return protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskCancelled, Error: "workflow cancelled"}, nil
	case res := <-done:
		r.s.hub.release(nodeID)
		if res.Status == protocol.TaskUncertain {
			_ = r.s.DB.SetTaskStatus(spec.TaskID, protocol.TaskUncertain, 0, "", "", res.Error)
		}
		return res, nil
	}
}
