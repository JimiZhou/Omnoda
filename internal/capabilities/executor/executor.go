// Package executor invokes an enabled capability after policy checks.
package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
	"github.com/JimiZhou/Omnoda/internal/sandbox"
)

type Request struct {
	ID      string
	Version string
	Input   map[string]any
	// Workspace is the owner's authorized directory. Inputs must live under it.
	Workspace string
	// OutputDir is a directory the capability may write.
	OutputDir string
	// NetworkAllowed is the owner's grant. A manifest cannot set this alone.
	NetworkAllowed bool
}

type Result struct {
	OK       bool           `json:"ok"`
	Output   map[string]any `json:"output,omitempty"`
	Stdout   string         `json:"stdout,omitempty"`
	Error    string         `json:"error,omitempty"`
	ExitCode int            `json:"exit_code"`
	Hash     string         `json:"content_hash,omitempty"`
	Backend  string         `json:"sandbox,omitempty"`
}

var (
	ErrDisabled = errors.New("capability is not enabled")
	ErrPlatform = errors.New("capability does not support this platform")
	ErrVersion  = errors.New("capability version mismatch")
	ErrSchema   = errors.New("input does not match schema")
	ErrPolicy   = errors.New("capability permission is not granted")
	ErrSandbox  = errors.New("untrusted capability refused: sandbox unavailable")
)

type Executor struct {
	Reg *registry.Registry
}

func (e *Executor) Invoke(ctx context.Context, req Request) Result {
	rec, err := e.Reg.Get(req.ID, "")
	if err != nil {
		return Result{Error: err.Error(), ExitCode: 127}
	}
	if req.Version != "" && rec.Version != req.Version {
		return Result{Error: ErrVersion.Error(), ExitCode: 126}
	}
	if rec.ActivationStatus != registry.StatusEnabled {
		return Result{Error: ErrDisabled.Error(), ExitCode: 126}
	}
	if !rec.Manifest.Supports(runtime.GOOS, runtime.GOARCH) {
		return Result{Error: ErrPlatform.Error(), ExitCode: 126}
	}
	if err := validateInput(rec.Manifest.Spec.Inputs, req.Input); err != nil {
		return Result{Error: ErrSchema.Error() + ": " + err.Error(), ExitCode: 126}
	}
	if rec.Manifest.Spec.Permissions.Network && !req.NetworkAllowed {
		return Result{Error: ErrPolicy.Error() + ": network", ExitCode: 126}
	}
	if rec.Runtime == "builtin" {
		return runBuiltin(rec, req)
	}
	return runPackaged(ctx, rec, req)
}

func runBuiltin(rec registry.Record, req Request) Result {
	switch rec.CapabilityID {
	case "omnoda.system-info":
		return Result{OK: true, ExitCode: 0, Hash: rec.ContentHash, Output: map[string]any{
			"os": runtime.GOOS, "arch": runtime.GOARCH,
		}, Stdout: runtime.GOOS + "/" + runtime.GOARCH}
	default:
		return Result{Error: "unknown builtin", ExitCode: 127}
	}
}

func runPackaged(ctx context.Context, rec registry.Record, req Request) Result {
	if rec.Origin != "builtin" {
		if sandbox.Select().Name() == "none" {
			return Result{Error: ErrSandbox.Error(), ExitCode: 126, Backend: "none"}
		}
	}
	work, err := os.MkdirTemp("", "omnoda-cap-")
	if err != nil {
		return Result{Error: err.Error(), ExitCode: 1}
	}
	defer os.RemoveAll(work)
	// Stage the package read-only by copying into the work dir. The sandbox
	// then bind-mounts only this directory plus declared inputs.
	stage := filepath.Join(work, "pkg")
	if err := copyVisible(rec.Dir, stage); err != nil {
		return Result{Error: err.Error(), ExitCode: 1}
	}
	inPath := ""
	if name, _ := req.Input["input_file"].(string); name != "" {
		if sandbox.DeniedPath(name) && !filepath.IsAbs(name) {
			return Result{Error: ErrPolicy.Error() + ": input path", ExitCode: 126}
		}
		abs := name
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(req.Workspace, name)
		}
		abs, err = filepath.Abs(abs)
		if err != nil {
			return Result{Error: err.Error(), ExitCode: 126}
		}
		ws, _ := filepath.Abs(req.Workspace)
		if ws == "" || !within(abs, ws) {
			return Result{Error: ErrPolicy.Error() + ": input outside workspace", ExitCode: 126}
		}
		inPath = abs
	}
	outDir := req.OutputDir
	if outDir == "" {
		outDir = filepath.Join(work, "out")
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return Result{Error: err.Error(), ExitCode: 1}
	}
	argv, err := commandFor(rec, stage, inPath)
	if err != nil {
		return Result{Error: err.Error(), ExitCode: 127}
	}
	body, _ := json.Marshal(req.Input)
	_ = os.WriteFile(filepath.Join(stage, "input.json"), body, 0o600)
	sb := sandbox.Select()
	timeout := time.Duration(rec.Manifest.Spec.Limits.TimeoutSeconds) * time.Second
	res := sb.Run(ctx, sandbox.Request{
		Argv: argv, WorkDir: stage, ReadOnly: compact(inPath), Writable: []string{outDir},
		Timeout: timeout, MemoryMB: rec.Manifest.Spec.Limits.MemoryMB,
		Network: false, // execution never receives network, even if research did
	})
	out := Result{Stdout: res.Output, ExitCode: res.ExitCode, Hash: rec.ContentHash, Backend: res.Backend}
	if res.Err != nil || res.ExitCode != 0 {
		msg := res.Output
		if res.Err != nil {
			msg = res.Err.Error() + " " + msg
		}
		out.Error = msg
		return out
	}
	out.OK = true
	var parsed map[string]any
	if json.Unmarshal([]byte(res.Output), &parsed) == nil {
		out.Output = parsed
	} else if b, err := os.ReadFile(filepath.Join(outDir, "result.json")); err == nil {
		_ = json.Unmarshal(b, &parsed)
		out.Output = parsed
	}
	return out
}

func commandFor(rec registry.Record, stage, input string) ([]string, error) {
	switch rec.Runtime {
	case "python":
		ep := filepath.ToSlash(rec.Manifest.Spec.Entrypoint)
		argv := []string{"python3", ep}
		if input != "" {
			argv = append(argv, "--input", "/inputs/"+filepath.Base(input))
		}
		argv = append(argv, "--output", "/output/result.json")
		return argv, nil
	case "shell", "cli":
		if len(rec.Manifest.Spec.Argv) == 0 {
			return nil, errors.New("argv required")
		}
		return rec.Manifest.Spec.Argv, nil
	default:
		return nil, fmt.Errorf("runtime %s", rec.Runtime)
	}
}

func within(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func compact(p string) []string {
	if p == "" {
		return nil
	}
	return []string{p}
}

func copyVisible(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "record.json" {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
}
