package controlplane

import (
	"net/http"
	"os"

	"github.com/JimiZhou/Omnoda/internal/agent"
	"github.com/JimiZhou/Omnoda/internal/evolver"
	"github.com/JimiZhou/Omnoda/internal/policy"
)

func (s *Server) createEvo(w http.ResponseWriter, r *http.Request) {
	if s.Evos == nil {
		writeErr(w, 404, "not_found", "evolver is not configured")
		return
	}
	var body struct {
		Goal        string            `json:"goal"`
		Missing     string            `json:"missing_capability"`
		Constraints map[string]string `json:"constraints"`
		// Fixture is a test-only switch. It never counts as a model run.
		Fixture bool `json:"fixture"`
	}
	if err := decode(r, &body); err != nil || body.Goal == "" {
		writeErr(w, 400, "bad_json", "goal is required")
		return
	}
	rec, err := s.Evos.Start(body.Goal, body.Missing, body.Constraints)
	if err != nil {
		writeErr(w, 500, "evolver", err.Error())
		return
	}
	var gen evolver.Generator
	if body.Fixture && os.Getenv("OMNODA_ALLOW_FIXTURE_EVOLUTION") == "1" {
		gen = evolver.FixtureGenerator{}
	}
	// Model-backed generation is wired when a provider is configured.
	// Without one, Run records a failure instead of inventing a capability.
	if gen == nil {
		if mc, enc, err := s.DB.LoadModel(); err == nil && enc != "" && mc.BaseURL != "" {
			if key, _, err := s.openSecret(enc); err == nil && key != "" {
				gen = &modelGenerator{base: mc.BaseURL, model: mc.Model, key: key}
			}
		}
	}
	done, err := s.Evos.Run(r.Context(), rec.ID, gen, 2)
	if err != nil {
		writeErr(w, 500, "evolver", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "evolution.create", "", string(done.State), done.ID)
	writeJSON(w, 201, done)
}

func (s *Server) listEvos(w http.ResponseWriter, r *http.Request) {
	if s.Evos == nil {
		writeJSON(w, 200, map[string]any{"evolutions": []any{}})
		return
	}
	rows, err := s.Evos.List()
	if err != nil {
		writeErr(w, 500, "evolver", err.Error())
		return
	}
	if rows == nil {
		rows = []evolver.Record{}
	}
	writeJSON(w, 200, map[string]any{"evolutions": rows})
}

func (s *Server) getEvo(w http.ResponseWriter, r *http.Request) {
	if s.Evos == nil {
		writeErr(w, 404, "not_found", "evolver is not configured")
		return
	}
	rec, err := s.Evos.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", "evolution not found")
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) approveEvo(w http.ResponseWriter, r *http.Request) {
	if s.Evos == nil {
		writeErr(w, 404, "not_found", "evolver is not configured")
		return
	}
	rec, err := s.Evos.Approve(r.PathValue("id"))
	if err != nil {
		writeErr(w, 409, "evolution", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "evolution.approve", "", "ok", rec.ID)
	writeJSON(w, 200, rec)
}

func (s *Server) rejectEvo(w http.ResponseWriter, r *http.Request) {
	if s.Evos == nil {
		writeErr(w, 404, "not_found", "evolver is not configured")
		return
	}
	rec, err := s.Evos.Reject(r.PathValue("id"))
	if err != nil {
		writeErr(w, 409, "evolution", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "evolution.reject", "", "ok", rec.ID)
	writeJSON(w, 200, rec)
}

func (s *Server) agentRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Goal      string `json:"goal"`
		Workspace string `json:"workspace"`
	}
	if err := decode(r, &body); err != nil || body.Goal == "" {
		writeErr(w, 400, "bad_json", "goal is required")
		return
	}
	mc, enc, err := s.DB.LoadModel()
	if err != nil || enc == "" || mc.BaseURL == "" {
		writeErr(w, 409, "no_model", "model provider is not configured; local capabilities remain available")
		return
	}
	key, _, err := s.openSecret(enc)
	if err != nil || key == "" {
		writeErr(w, 409, "no_model", "model key could not be opened")
		return
	}
	answer, err := agent.Run(r.Context(), agent.Config{
		BaseURL: mc.BaseURL, Model: mc.Model, APIKey: key,
		MaxSteps: mc.MaxSteps, TokenBudget: mc.TokenBudget,
	}, body.Goal, agent.ToolContext{Policy: policy.Policy{Workspaces: compactWS(body.Workspace)}, Workspace: body.Workspace}, nil)
	if err != nil {
		writeErr(w, 422, "agent", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "agent.run", "", "ok", "")
	writeJSON(w, 200, map[string]any{"answer": answer})
}

func compactWS(w string) []string {
	if w == "" {
		return nil
	}
	return []string{w}
}
