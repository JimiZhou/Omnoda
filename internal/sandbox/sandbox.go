// Package sandbox runs untrusted capability code outside the node process.
//
// Linux uses bubblewrap plus a dropped-capability user namespace when bwrap
// is installed. A plain subprocess with a path check is not treated as a
// sandbox: if no backend is available, Run returns ErrUnavailable and the
// caller must refuse the untrusted program.
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("no sandbox backend meets policy on this platform")

// runtimeOS is the OS used by Select. Tests do not change it.
var runtimeOS = runtime.GOOS

type Request struct {
	Argv        []string
	WorkDir     string // writable workspace inside the sandbox
	ReadOnly    []string
	Writable    []string
	Timeout     time.Duration
	MemoryMB    int
	Network     bool
	OutputLimit int
}

type Result struct {
	ExitCode int
	Output   string
	Err      error
	Backend  string
}

type Backend interface {
	Name() string
	Available() bool
	Run(ctx context.Context, req Request) Result
}

func Select() Backend {
	if runtimeOS == "linux" {
		b := bwrapBackend{}
		if b.Available() {
			return b
		}
	}
	return noneBackend{}
}

type noneBackend struct{}

func (noneBackend) Name() string    { return "none" }
func (noneBackend) Available() bool { return false }
func (noneBackend) Run(context.Context, Request) Result {
	return Result{ExitCode: 126, Err: ErrUnavailable, Backend: "none"}
}

type bwrapBackend struct{}

func (bwrapBackend) Name() string { return "bwrap" }
func (bwrapBackend) Available() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

func (b bwrapBackend) Run(ctx context.Context, req Request) Result {
	if !b.Available() {
		return Result{ExitCode: 126, Err: ErrUnavailable, Backend: "bwrap"}
	}
	if len(req.Argv) == 0 {
		return Result{ExitCode: 126, Err: errors.New("argv required"), Backend: "bwrap"}
	}
	if req.Timeout <= 0 {
		req.Timeout = 30 * time.Second
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = 256
	}
	if req.OutputLimit <= 0 {
		req.OutputLimit = 64 * 1024
	}
	cctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	args := []string{
		"--unshare-all",
		"--die-with-parent",
		"--new-session",
		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--ro-bind", "/usr", "/usr",
		"--symlink", "usr/bin", "/bin",
		"--symlink", "usr/lib", "/lib",
		"--symlink", "usr/lib64", "/lib64",
		"--clearenv",
		"--setenv", "PATH", "/usr/bin:/bin",
		"--setenv", "HOME", "/work",
		"--setenv", "LANG", "C.UTF-8",
	}
	if !req.Network {
		// --unshare-all already drops the network namespace.
	} else {
		args = append(args, "--share-net")
	}
	// Interpreter binaries.
	for _, p := range []string{"/usr/bin/python3", "/usr/bin/python", "/bin/sh"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			// already covered by /usr bind
			_ = st
		}
	}
	if req.WorkDir != "" {
		args = append(args, "--bind", req.WorkDir, "/work", "--chdir", "/work")
	}
	for _, p := range req.ReadOnly {
		abs, err := filepath.Abs(p)
		if err != nil {
			return Result{ExitCode: 126, Err: err, Backend: "bwrap"}
		}
		base := filepath.Base(abs)
		args = append(args, "--ro-bind", abs, "/inputs/"+base)
	}
	for _, p := range req.Writable {
		abs, err := filepath.Abs(p)
		if err != nil {
			return Result{ExitCode: 126, Err: err, Backend: "bwrap"}
		}
		if err := os.MkdirAll(abs, 0o700); err != nil {
			return Result{ExitCode: 126, Err: err, Backend: "bwrap"}
		}
		args = append(args, "--bind", abs, "/output")
	}
	args = append(args, "--")
	args = append(args, req.Argv...)

	cmd := exec.CommandContext(cctx, "bwrap", args...)
	// ulimit via prlimit when present, otherwise the context timeout is the hard stop.
	if _, err := exec.LookPath("prlimit"); err == nil {
		mem := strconv.Itoa(req.MemoryMB * 1024 * 1024)
		wrapped := append([]string{"--as=" + mem, "--cpu=30", "--nproc=32", "--nofile=64", "--", "bwrap"}, args...)
		cmd = exec.CommandContext(cctx, "prlimit", wrapped...)
	}
	var buf bytes.Buffer
	cmd.Stdout = &limitWriter{buf: &buf, n: req.OutputLimit}
	cmd.Stderr = cmd.Stdout
	err := cmd.Run()
	res := Result{Output: buf.String(), Backend: "bwrap"}
	if err != nil {
		res.Err = err
		if cctx.Err() != nil {
			res.Err = fmt.Errorf("sandbox timeout: %w", err)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = 1
		}
	}
	return res
}

type limitWriter struct {
	buf *bytes.Buffer
	n   int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.buf.Len() >= w.n {
		return len(p), nil
	}
	if w.buf.Len()+len(p) > w.n {
		p = p[:w.n-w.buf.Len()]
	}
	_, _ = w.buf.Write(p)
	return len(p), nil
}

// DeniedPath reports whether a relative path tries to leave its root.
func DeniedPath(rel string) bool {
	if rel == "" {
		return true
	}
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return true
	}
	return false
}
