// Package runtime executes allow-listed commands on the node.
// It never invokes a shell. Cancellation kills the process group.
package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/JimiZhou/Omnoda/internal/policy"
	"github.com/JimiZhou/Omnoda/internal/protocol"
)

const maxOutput = 256 * 1024

type Request struct {
	Argv    []string
	Dir     string
	Timeout time.Duration
	Level   protocol.ExecLevel
	OnLine  func(string)
}

type Result struct {
	ExitCode int
	Output   string
	Err      error
	TimedOut bool
}

func Run(ctx context.Context, pol policy.Policy, req Request) Result {
	if err := pol.CheckCommand(req.Level, req.Argv); err != nil {
		return Result{ExitCode: 126, Err: err}
	}
	dir := req.Dir
	if dir != "" {
		resolved, err := pol.ResolveWorkspace(dir)
		if err != nil {
			return Result{ExitCode: 126, Err: err}
		}
		// If the path exists, reject symlink escapes.
		if fi, err := os.Lstat(resolved); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				target, err := filepathEval(resolved)
				if err != nil {
					return Result{ExitCode: 126, Err: err}
				}
				if _, err := pol.ResolveWorkspace(target); err != nil {
					return Result{ExitCode: 126, Err: policy.ErrPathEscape}
				}
			}
		}
		dir = resolved
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(req.Argv[0], req.Argv[1:]...)
	cmd.Dir = dir
	cmd.Env = minimalEnv()
	setGroup(cmd)
	var buf bytes.Buffer
	var mu sync.Mutex
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return Result{ExitCode: 127, Err: fmt.Errorf("start: %w", err)}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tmp := make([]byte, 1024)
		var line []byte
		for {
			n, err := pr.Read(tmp)
			if n > 0 {
				mu.Lock()
				if buf.Len() < maxOutput {
					buf.Write(tmp[:n])
				}
				mu.Unlock()
				line = append(line, tmp[:n]...)
				for {
					i := bytes.IndexByte(line, '\n')
					if i < 0 {
						break
					}
					if req.OnLine != nil {
						req.OnLine(string(line[:i]))
					}
					line = line[i+1:]
				}
			}
			if err != nil {
				if len(line) > 0 && req.OnLine != nil {
					req.OnLine(string(line))
				}
				return
			}
		}
	}()
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var res Result
	select {
	case <-cctx.Done():
		killGroup(cmd)
		<-waitErr
		_ = pw.Close()
		<-done
		res.TimedOut = ctx.Err() == nil
		res.ExitCode = 124
		res.Err = fmt.Errorf("timeout or cancel")
	case err := <-waitErr:
		_ = pw.Close()
		<-done
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				res.ExitCode = ee.ExitCode()
			} else {
				res.ExitCode = 1
				res.Err = err
			}
		}
	}
	mu.Lock()
	res.Output = buf.String()
	mu.Unlock()
	return res
}

func minimalEnv() []string {
	keep := []string{"PATH", "HOME", "USER", "LANG", "LC_ALL", "TMPDIR", "SYSTEMROOT", "WINDIR"}
	var out []string
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

func filepathEval(path string) (string, error) {
	return filepathAbsEval(path)
}
