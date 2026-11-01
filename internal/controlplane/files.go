package controlplane

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) installSh(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(filepath.Join("scripts", "install.sh"))
	if err != nil {
		// Try next to the binary.
		exe, _ := os.Executable()
		b, err = os.ReadFile(filepath.Join(filepath.Dir(exe), "install.sh"))
	}
	if err != nil {
		http.Error(w, "install.sh missing", 404)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) installPS(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(filepath.Join("scripts", "install.ps1"))
	if err != nil {
		exe, _ := os.Executable()
		b, err = os.ReadFile(filepath.Join(filepath.Dir(exe), "install.ps1"))
	}
	if err != nil {
		http.Error(w, "install.ps1 missing", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) releaseFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.DataDir, "releases", name)
	http.ServeFile(w, r, path)
}
