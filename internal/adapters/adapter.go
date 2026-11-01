// Package adapters discovers and runs external agent CLIs.
// Omnoda does not reimplement Codex, Claude Code, or OpenCode.
package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
)

type Detection struct {
	Info protocol.HarnessInfo
}

type RunRequest struct {
	RunID     string
	Prompt    string
	Workspace string
	Timeout   time.Duration
}

type RunResult struct {
	ExitCode int
	Summary  string
	Session  string
	Err      error
}

type Adapter interface {
	ID() string
	Detect(ctx context.Context) Detection
	Start(ctx context.Context, req RunRequest, emit func(protocol.Event)) (RunResult, error)
}

func All() []Adapter {
	return []Adapter{Codex{}, ClaudeCode{}, OpenCode{}, Fake{}}
}

func DetectAll(ctx context.Context) []protocol.HarnessInfo {
	var out []protocol.HarnessInfo
	for _, a := range All() {
		if a.ID() == "fake" {
			continue
		}
		out = append(out, a.Detect(ctx).Info)
	}
	return out
}

func ByID(id string) (Adapter, error) {
	for _, a := range All() {
		if a.ID() == id {
			return a, nil
		}
	}
	return nil, errors.New("unknown harness: " + id)
}

type Codex struct{}

func (Codex) ID() string { return "codex" }

func (Codex) Detect(ctx context.Context) Detection {
	return detectBin(ctx, "codex", "codex")
}

func (Codex) Start(ctx context.Context, req RunRequest, emit func(protocol.Event)) (RunResult, error) {
	// Official non-interactive interface. Sandbox stays at workspace-write.
	// We do not pass --dangerously-bypass-approvals-and-sandbox.
	argv := []string{"exec", "--json", "--color", "never", "--skip-git-repo-check", "--sandbox", "workspace-write", "--ephemeral"}
	if req.Workspace != "" {
		argv = append(argv, "--cd", req.Workspace)
	}
	argv = append(argv, req.Prompt)
	return runCLI(ctx, "codex", argv, req, emit, parseCodexLine)
}

type ClaudeCode struct{}

func (ClaudeCode) ID() string { return "claude-code" }

func (ClaudeCode) Detect(ctx context.Context) Detection {
	return detectBin(ctx, "claude-code", "claude")
}

func (ClaudeCode) Start(ctx context.Context, req RunRequest, emit func(protocol.Event)) (RunResult, error) {
	// Headless print mode with stream-json. Default permission mode is not bypass.
	// Default permission mode. The process cwd is the workspace. We do not pass a bypass flag.
	argv := []string{"-p", req.Prompt, "--output-format", "stream-json", "--verbose", "--permission-mode", "default"}
	if req.Workspace != "" {
		argv = append(argv, "--add-dir", req.Workspace)
	}
	return runCLI(ctx, "claude", argv, req, emit, parseClaudeLine)
}

type OpenCode struct{}

func (OpenCode) ID() string { return "opencode" }

func (OpenCode) Detect(ctx context.Context) Detection {
	return detectBin(ctx, "opencode", "opencode")
}

func (OpenCode) Start(ctx context.Context, req RunRequest, emit func(protocol.Event)) (RunResult, error) {
	// Official non-interactive run. No unrestricted auto-approve flag.
	argv := []string{"run", "--format", "json", req.Prompt}
	return runCLI(ctx, "opencode", argv, req, emit, parseOpenCodeLine)
}

// Fake is an explicit test double. It is never reported as a real harness.
type Fake struct {
	Bin string
}

func (f Fake) ID() string { return "fake" }

func (f Fake) Detect(ctx context.Context) Detection {
	path := f.Bin
	if path == "" {
		path, _ = exec.LookPath("omnoda-fake-harness")
	}
	if path == "" {
		return Detection{Info: protocol.HarnessInfo{ID: "fake", Present: false, Error: "not installed"}}
	}
	return Detection{Info: protocol.HarnessInfo{ID: "fake", Present: true, Path: path, Version: "test"}}
}

func (f Fake) Start(ctx context.Context, req RunRequest, emit func(protocol.Event)) (RunResult, error) {
	bin := f.Bin
	if bin == "" {
		var err error
		bin, err = exec.LookPath("omnoda-fake-harness")
		if err != nil {
			return RunResult{}, errors.New("fake harness binary not found")
		}
	}
	return runCLI(ctx, bin, []string{req.Prompt}, req, emit, parseGenericJSON)
}

func detectBin(ctx context.Context, id, bin string) Detection {
	path, err := exec.LookPath(bin)
	if err != nil {
		return Detection{Info: protocol.HarnessInfo{ID: id, Present: false, Error: "not found on PATH"}}
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, path, "--version").CombinedOutput()
	ver := strings.TrimSpace(string(out))
	if err != nil && ver == "" {
		return Detection{Info: protocol.HarnessInfo{ID: id, Present: true, Path: path, Error: "version check failed: " + err.Error()}}
	}
	if i := strings.IndexByte(ver, '\n'); i >= 0 {
		ver = ver[:i]
	}
	return Detection{Info: protocol.HarnessInfo{ID: id, Present: true, Path: path, Version: ver}}
}

// Normalize is exported so tests can lock the event mapping without running a CLI.
func Normalize(adapterID, line string) (protocol.EventType, map[string]any, string) {
	switch adapterID {
	case "codex":
		return parseCodexLine(line)
	case "claude-code":
		return parseClaudeLine(line)
	default:
		return parseGenericJSON(line)
	}
}

func runCLI(ctx context.Context, bin string, argv []string, req RunRequest, emit func(protocol.Event), parse func(string) (protocol.EventType, map[string]any, string)) (RunResult, error) {
	if _, err := exec.LookPath(bin); err != nil && !strings.Contains(bin, "/") {
		return RunResult{ExitCode: 127, Err: errors.New(bin + " is not installed")}, nil
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command(bin, argv...)
	if req.Workspace != "" {
		cmd.Dir = req.Workspace
	}
	cmd.Env = os.Environ()
	setGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return RunResult{ExitCode: 127, Err: err}, nil
	}
	seq := int64(0)
	next := func(t protocol.EventType, payload map[string]any, raw string) {
		seq++
		ev := protocol.Event{
			EventID: req.RunID + ":" + itoa(seq), RunID: req.RunID, Timestamp: time.Now().UTC(),
			Sequence: seq, EventType: t, Payload: payload, Origin: "harness",
		}
		if raw != "" {
			ev.Raw = map[string]any{"line": redactLine(raw)}
		}
		emit(ev)
	}
	next(protocol.EvRunStarted, map[string]any{"bin": bin}, "")
	var summary strings.Builder
	var session string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if len(line) > 8000 {
				line = line[:8000]
			}
			et, payload, sess := parse(line)
			if sess != "" {
				session = sess
			}
			if payload == nil {
				payload = map[string]any{}
			}
			if et == "" {
				et = protocol.EvLog
				payload["line"] = line
			}
			if et == protocol.EvMessageDelta {
				if d, ok := payload["text"].(string); ok {
					summary.WriteString(d)
				}
			}
			next(et, payload, line)
		}
	}()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var result RunResult
	select {
	case <-cctx.Done():
		killGroup(cmd)
		<-wait
		wg.Wait()
		result.ExitCode = 124
		result.Err = errors.New("harness timed out or cancelled")
		next(protocol.EvRunCancelled, map[string]any{"error": result.Err.Error()}, "")
	case err := <-wait:
		wg.Wait()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				result.ExitCode = ee.ExitCode()
			} else {
				result.ExitCode = 1
			}
			result.Err = err
			next(protocol.EvRunFailed, map[string]any{"error": err.Error(), "exit_code": result.ExitCode}, "")
		} else {
			next(protocol.EvRunCompleted, map[string]any{"exit_code": 0}, "")
		}
	}
	result.Summary = strings.TrimSpace(summary.String())
	result.Session = session
	// Drop raw accumulation: callers persist normalized events only.
	_ = io.EOF
	return result, nil
}

func parseCodexLine(line string) (protocol.EventType, map[string]any, string) {
	var m map[string]any
	if json.Unmarshal([]byte(line), &m) != nil {
		return protocol.EvLog, map[string]any{"line": line}, ""
	}
	t, _ := m["type"].(string)
	switch t {
	case "thread.started", "session.created":
		sid, _ := m["thread_id"].(string)
		if sid == "" {
			sid, _ = m["session_id"].(string)
		}
		return protocol.EvRunStarted, map[string]any{"session_id": sid}, sid
	case "item.completed", "agent_message", "message":
		text := findText(m)
		return protocol.EvMessageDelta, map[string]any{"text": text}, ""
	case "exec_command", "tool_call":
		return protocol.EvToolStarted, map[string]any{"raw_type": t}, ""
	default:
		if strings.Contains(t, "complete") || t == "turn.completed" {
			return protocol.EvRunCompleted, map[string]any{"raw_type": t}, ""
		}
		return protocol.EvLog, map[string]any{"raw_type": t}, ""
	}
}

func parseClaudeLine(line string) (protocol.EventType, map[string]any, string) {
	var m map[string]any
	if json.Unmarshal([]byte(line), &m) != nil {
		return protocol.EvLog, map[string]any{"line": line}, ""
	}
	t, _ := m["type"].(string)
	sid, _ := m["session_id"].(string)
	switch t {
	case "assistant":
		return protocol.EvMessageDelta, map[string]any{"text": findText(m)}, sid
	case "content_block_delta":
		return protocol.EvMessageDelta, map[string]any{"text": findText(m)}, sid
	case "tool_use", "tool_call":
		return protocol.EvToolStarted, map[string]any{"raw_type": t}, sid
	case "tool_result":
		return protocol.EvToolCompleted, map[string]any{"raw_type": t}, sid
	case "result":
		return protocol.EvMessageDelta, map[string]any{"text": findText(m)}, sid
	default:
		return protocol.EvLog, map[string]any{"raw_type": t}, sid
	}
}

func parseOpenCodeLine(line string) (protocol.EventType, map[string]any, string) {
	return parseGenericJSON(line)
}

func parseGenericJSON(line string) (protocol.EventType, map[string]any, string) {
	var m map[string]any
	if json.Unmarshal([]byte(line), &m) != nil {
		return protocol.EvMessageDelta, map[string]any{"text": line}, ""
	}
	t, _ := m["type"].(string)
	sid, _ := m["session_id"].(string)
	switch t {
	case "started":
		return protocol.EvRunStarted, m, sid
	case "delta", "message":
		text, _ := m["text"].(string)
		return protocol.EvMessageDelta, map[string]any{"text": text}, sid
	case "completed":
		return protocol.EvRunCompleted, m, sid
	case "failed":
		return protocol.EvRunFailed, m, sid
	default:
		if text, ok := m["text"].(string); ok {
			return protocol.EvMessageDelta, map[string]any{"text": text}, sid
		}
		return protocol.EvLog, map[string]any{"raw_type": t}, sid
	}
}

func findText(m map[string]any) string {
	if s, ok := m["text"].(string); ok {
		return s
	}
	if msg, ok := m["message"].(map[string]any); ok {
		if c, ok := msg["content"].([]any); ok {
			var b strings.Builder
			for _, item := range c {
				im, _ := item.(map[string]any)
				if im["type"] == "text" {
					if t, ok := im["text"].(string); ok {
						b.WriteString(t)
					}
				}
			}
			return b.String()
		}
	}
	if d, ok := m["delta"].(map[string]any); ok {
		if t, ok := d["text"].(string); ok {
			return t
		}
	}
	if r, ok := m["result"].(string); ok {
		return r
	}
	return ""
}

func redactLine(s string) string {
	low := strings.ToLower(s)
	for _, n := range []string{"api_key", "sk-", "authorization", "bearer ", "secret", "token"} {
		if strings.Contains(low, n) {
			return "[redacted harness line]"
		}
	}
	if len(s) > 2000 {
		return s[:2000]
	}
	return s
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
