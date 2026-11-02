package controlplane

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/JimiZhou/Omnoda/internal/capabilities/executor"
	"github.com/JimiZhou/Omnoda/internal/capabilities/packaging"
	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
)

func (s *Server) bootstrapInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"version": s.Version, "mode": "seed", "bind": "127.0.0.1",
		"capabilities": s.Caps != nil, "evolver": s.Evos != nil,
	})
}

func (s *Server) listCaps(w http.ResponseWriter, r *http.Request) {
	if s.Caps == nil {
		writeJSON(w, 200, map[string]any{"capabilities": []any{}})
		return
	}
	rows, err := s.Caps.List()
	if err != nil {
		writeErr(w, 500, "registry", err.Error())
		return
	}
	if rows == nil {
		rows = []registry.Record{}
	}
	writeJSON(w, 200, map[string]any{"capabilities": rows})
}

func (s *Server) getCap(w http.ResponseWriter, r *http.Request) {
	if s.Caps == nil {
		writeErr(w, 404, "not_found", "no capability registry")
		return
	}
	rec, err := s.Caps.Get(r.PathValue("id"), r.URL.Query().Get("version"))
	if err != nil {
		writeErr(w, 404, "not_found", "capability not found")
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) activateCap(w http.ResponseWriter, r *http.Request) {
	s.setCap(w, r, registry.StatusEnabled)
}

func (s *Server) disableCap(w http.ResponseWriter, r *http.Request) {
	s.setCap(w, r, registry.StatusDisabled)
}

func (s *Server) setCap(w http.ResponseWriter, r *http.Request, st registry.Status) {
	if s.Caps == nil {
		writeErr(w, 404, "not_found", "no capability registry")
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	_ = decode(r, &body)
	if st == registry.StatusEnabled {
		current, err := s.Caps.Get(r.PathValue("id"), body.Version)
		if err != nil {
			writeErr(w, 404, "not_found", "capability not found")
			return
		}
		if current.VerificationStatus == registry.VerifyUnverified || current.VerificationStatus == registry.VerifyFailed || current.VerificationStatus == registry.VerifyNone {
			if current.Origin != "builtin" {
				writeErr(w, 409, "unverified", "capability is unverified and cannot be enabled")
				return
			}
		}
	}
	rec, err := s.Caps.SetStatus(r.PathValue("id"), body.Version, st)
	if err != nil {
		writeErr(w, 409, "capability", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "capability."+string(st), "", "ok", rec.CapabilityID+"@"+rec.Version)
	writeJSON(w, 200, rec)
}

func (s *Server) testCap(w http.ResponseWriter, r *http.Request) {
	if s.Caps == nil {
		writeErr(w, 404, "not_found", "no capability registry")
		return
	}
	var body struct {
		Version   string         `json:"version"`
		Input     map[string]any `json:"input"`
		Workspace string         `json:"workspace"`
	}
	_ = decode(r, &body)
	ex := executor.Executor{Reg: s.Caps}
	res := ex.Invoke(r.Context(), executor.Request{
		ID: r.PathValue("id"), Version: body.Version, Input: body.Input, Workspace: body.Workspace,
	})
	code := 200
	if !res.OK {
		code = 422
	}
	s.DB.Audit(userOf(r), "", "", "", "capability.test", "", res.Error, r.PathValue("id"))
	writeJSON(w, code, res)
}

func (s *Server) importCap(w http.ResponseWriter, r *http.Request) {
	if s.Caps == nil {
		writeErr(w, 404, "not_found", "no capability registry")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := decode(r, &body); err != nil || body.Path == "" {
		writeErr(w, 400, "bad_json", "path is required")
		return
	}
	rec, err := packaging.Import(s.Caps, body.Path)
	if err != nil {
		writeErr(w, 400, "import", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "capability.import", "", "installed", rec.CapabilityID)
	writeJSON(w, 201, rec)
}

func (s *Server) exportCap(w http.ResponseWriter, r *http.Request) {
	if s.Caps == nil {
		writeErr(w, 404, "not_found", "no capability registry")
		return
	}
	rec, err := s.Caps.Get(r.PathValue("id"), "")
	if err != nil {
		writeErr(w, 404, "not_found", "capability not found")
		return
	}
	dest := filepath.Join(os.TempDir(), rec.CapabilityID+"-"+rec.Version+".tar.gz")
	if err := packaging.Export(rec, dest); err != nil {
		writeErr(w, 500, "export", err.Error())
		return
	}
	s.DB.Audit(userOf(r), "", "", "", "capability.export", "", "ok", dest)
	writeJSON(w, 200, map[string]any{"path": dest, "content_hash": rec.ContentHash})
}
