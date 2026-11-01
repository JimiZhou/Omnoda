// Package seed boots a single-device Omnoda runtime.
// It embeds the control plane, identity, capability registry, and evolver store.
// It does not require a remote server, Docker, or an external database.
package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/JimiZhou/Omnoda/internal/capabilities/builtins"
	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
	"github.com/JimiZhou/Omnoda/internal/controlplane"
	"github.com/JimiZhou/Omnoda/internal/evolver"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

type Paths struct {
	Root string
	Data string
	Caps string
	Evos string
	PID  string
}

func DefaultRoot() string {
	if v := os.Getenv("OMNODA_CONFIG_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".omnoda")
}

func Resolve(root string) Paths {
	if root == "" {
		root = DefaultRoot()
	}
	return Paths{
		Root: root,
		Data: filepath.Join(root, "seed"),
		Caps: filepath.Join(root, "capabilities"),
		Evos: filepath.Join(root, "evolutions"),
		PID:  filepath.Join(root, "seed.pid"),
	}
}

type Status struct {
	Running      bool   `json:"running"`
	Addr         string `json:"addr,omitempty"`
	Version      string `json:"version"`
	Model        bool   `json:"model_configured"`
	Capabilities int    `json:"capabilities"`
	Root         string `json:"root"`
}

type Runtime struct {
	Paths  Paths
	DB     *storage.DB
	Server *controlplane.Server
	Reg    *registry.Registry
	Evos   *evolver.Store
	HTTP   *http.Server
}

// Bootstrap creates directories, the database, and built-in capabilities.
// It is safe to call on an existing root: identity files and the database are kept.
func Bootstrap(root string) (*Runtime, error) {
	p := Resolve(root)
	if err := os.MkdirAll(p.Data, 0o700); err != nil {
		return nil, err
	}
	db, err := storage.Open(filepath.Join(p.Data, "omnoda.db"))
	if err != nil {
		return nil, err
	}
	srv, err := controlplane.New(db, p.Data, "http://127.0.0.1", protocol.Version)
	if err != nil {
		db.Close()
		return nil, err
	}
	reg, err := registry.Open(p.Caps)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := builtins.Ensure(reg); err != nil {
		db.Close()
		return nil, err
	}
	ev, err := evolver.Open(p.Evos, reg)
	if err != nil {
		db.Close()
		return nil, err
	}
	srv.Caps = reg
	srv.Evos = ev
	return &Runtime{Paths: p, DB: db, Server: srv, Reg: reg, Evos: ev}, nil
}

// Serve listens on 127.0.0.1 only. port 0 asks the OS for a free port.
func (rt *Runtime) Serve(ctx context.Context, port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	addr := ln.Addr().String()
	rt.HTTP = &http.Server{Handler: rt.Server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	_ = os.WriteFile(rt.Paths.PID, []byte(fmt.Sprintf(`{"pid":%d,"addr":%q}`+"\n", os.Getpid(), addr)), 0o600)
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = rt.HTTP.Shutdown(c)
	}()
	err = rt.HTTP.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func ReadStatus(root string) Status {
	p := Resolve(root)
	st := Status{Version: protocol.Version, Root: p.Root}
	b, err := os.ReadFile(p.PID)
	if err != nil {
		return st
	}
	var meta struct {
		PID  int    `json:"pid"`
		Addr string `json:"addr"`
	}
	if json.Unmarshal(b, &meta) == nil && meta.PID > 0 && procAlive(meta.PID) {
		st.Running = true
		st.Addr = meta.Addr
	}
	return st
}
