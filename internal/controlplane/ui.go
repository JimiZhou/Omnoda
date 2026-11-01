package controlplane

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	root := filepath.Join("apps", "web", "dist")
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		exe, _ := os.Executable()
		alt := filepath.Join(filepath.Dir(exe), "web")
		if st, err := os.Stat(alt); err == nil && st.IsDir() {
			root = alt
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(fallbackHTML))
			return
		}
	}
	if r.URL.Path == "/manifest.webmanifest" || r.URL.Path == "/sw.js" || strings.HasPrefix(r.URL.Path, "/assets/") {
		http.FileServer(http.Dir(root)).ServeHTTP(w, r)
		return
	}
	// SPA fallback
	path := filepath.Join(root, filepath.Clean(r.URL.Path))
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	http.ServeFile(w, r, filepath.Join(root, "index.html"))
}

// silence unused in case embed is added later
var _ = fs.ErrNotExist

const fallbackHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Omnoda</title></head>
<body style="font-family:sans-serif;background:#0f1115;color:#eee;padding:40px">
<h1>Omnoda</h1><p>Web UI bundle is missing. Build apps/web and restart.</p></body></html>`
