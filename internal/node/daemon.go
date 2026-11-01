// Package node is the device agent. It dials out, heartbeats, and executes
// only what local policy allows.
package node

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/JimiZhou/Omnoda/internal/adapters"
	"github.com/JimiZhou/Omnoda/internal/agent"
	"github.com/JimiZhou/Omnoda/internal/identity"
	"github.com/JimiZhou/Omnoda/internal/policy"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	rt "github.com/JimiZhou/Omnoda/internal/runtime"
	"github.com/gorilla/websocket"
)

type Config struct {
	Server          string
	ConfigDir       string
	DisplayName     string
	Workspaces      []string
	Labels          map[string]string
	AllowPrivileged bool
	CommandAllow    []string
	Version         string
	// Native model is optional and local to the node. The key stays here.
	ModelBaseURL   string
	ModelName      string
	ModelAPIKey    string
	EnrollmentCode string
}

type Daemon struct {
	Cfg    Config
	key    ed25519.PrivateKey
	nodeID string
	pol    policy.Policy
	mu     sync.Mutex
	runs   map[string]context.CancelFunc
	active int
}

func (d *Daemon) Load() error {
	if d.Cfg.ConfigDir == "" {
		home, _ := os.UserHomeDir()
		d.Cfg.ConfigDir = filepath.Join(home, ".omnoda")
	}
	if err := os.MkdirAll(d.Cfg.ConfigDir, 0o700); err != nil {
		return err
	}
	priv, err := identity.LoadOrCreate(d.Cfg.ConfigDir)
	if err != nil {
		return err
	}
	d.key = priv
	d.nodeID = identity.NodeID(identity.PublicFromPrivate(priv))
	d.pol = policy.Policy{Workspaces: d.Cfg.Workspaces, AllowPrivileged: d.Cfg.AllowPrivileged, CommandAllow: d.Cfg.CommandAllow}
	d.runs = map[string]context.CancelFunc{}
	_ = os.WriteFile(filepath.Join(d.Cfg.ConfigDir, "node.id"), []byte(d.nodeID+"\n"), 0o644)
	return d.saveMeta()
}

func (d *Daemon) saveMeta() error {
	meta := map[string]any{
		"node_id": d.nodeID, "display_name": d.Cfg.DisplayName, "server": d.Cfg.Server,
		"workspaces": d.Cfg.Workspaces, "labels": d.Cfg.Labels,
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(filepath.Join(d.Cfg.ConfigDir, "node.json"), b, 0o600)
}

func (d *Daemon) ID() string { return d.nodeID }

func (d *Daemon) Run(ctx context.Context) error {
	if err := d.Load(); err != nil {
		return err
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := d.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Exponential backoff with jitter. Cap at 60s so a dead server is not hammered.
		jitter := time.Duration(rand.Intn(500)) * time.Millisecond
		wait := backoff + jitter
		if wait > 60*time.Second {
			wait = 60 * time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if err != nil {
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
		} else {
			backoff = time.Second
		}
		_ = err
	}
}

func (d *Daemon) session(ctx context.Context) error {
	u := strings.TrimRight(d.Cfg.Server, "/")
	u = strings.Replace(u, "https://", "wss://", 1)
	u = strings.Replace(u, "http://", "ws://", 1)
	u += "/api/v1/node/ws"
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, u, http.Header{})
	if err != nil {
		return err
	}
	defer conn.Close()
	hello := d.hello()
	if err := conn.WriteJSON(protocol.Envelope{V: protocol.WireVersion, Type: protocol.MsgHello, Timestamp: time.Now().UTC(), NodeID: d.nodeID, Payload: mustMap(hello)}); err != nil {
		return err
	}
	var ch protocol.Envelope
	if err := conn.ReadJSON(&ch); err != nil {
		return err
	}
	if ch.Type == protocol.MsgError {
		return fmt.Errorf("server: %s", ch.Error.Message)
	}
	nonce, _ := ch.Payload["nonce"].(string)
	sig := identity.Sign(d.key, []byte(nonce))
	resp := protocol.Envelope{V: 1, Type: "node.challenge_response", Timestamp: time.Now().UTC(), Payload: map[string]any{
		"signature": sig, "enrollment_code": d.Cfg.EnrollmentCode,
	}}
	if err := conn.WriteJSON(resp); err != nil {
		return err
	}
	// Code is single-use. Do not send it again on reconnect.
	d.Cfg.EnrollmentCode = ""
	var ack protocol.Envelope
	if err := conn.ReadJSON(&ack); err != nil {
		return err
	}
	if ack.Type != protocol.MsgHelloAck {
		if ack.Error != nil {
			return errors.New(ack.Error.Message)
		}
		return errors.New("hello rejected")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go d.heartbeats(ctx, conn)
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			return err
		}
		switch env.Type {
		case protocol.MsgHeartbeatAck:
		case "node.revoked":
			return errors.New("revoked")
		case protocol.MsgTaskAssign:
			var spec protocol.TaskSpec
			b, _ := json.Marshal(env.Payload["task"])
			if json.Unmarshal(b, &spec) != nil {
				continue
			}
			go d.execute(ctx, conn, spec)
		case protocol.MsgTaskCancel:
			id, _ := env.Payload["task_id"].(string)
			d.mu.Lock()
			if c, ok := d.runs[id]; ok {
				c()
			}
			d.mu.Unlock()
			_ = conn.WriteJSON(protocol.Envelope{V: 1, Type: protocol.MsgCancelAck, Timestamp: time.Now().UTC(), Payload: map[string]any{"task_id": id}})
		}
	}
}

func (d *Daemon) heartbeats(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.mu.Lock()
			n := d.active
			d.mu.Unlock()
			_ = conn.WriteJSON(protocol.Envelope{V: 1, Type: protocol.MsgHeartbeat, Timestamp: time.Now().UTC(), NodeID: d.nodeID, Payload: map[string]any{
				"node_id": d.nodeID, "running": n,
			}})
		}
	}
}

func (d *Daemon) execute(ctx context.Context, conn *websocket.Conn, spec protocol.TaskSpec) {
	d.mu.Lock()
	if d.active >= 2 {
		d.mu.Unlock()
		d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, ExitCode: 1, Error: "node concurrency limit"})
		return
	}
	d.active++
	tctx, cancel := context.WithCancel(ctx)
	d.runs[spec.TaskID] = cancel
	d.mu.Unlock()
	defer func() {
		cancel()
		d.mu.Lock()
		d.active--
		delete(d.runs, spec.TaskID)
		d.mu.Unlock()
	}()
	seq := int64(0)
	emit := func(ev protocol.Event) {
		seq++
		ev.Sequence = seq
		ev.TaskID = spec.TaskID
		ev.NodeID = d.nodeID
		ev.RunID = spec.TaskID
		if ev.Timestamp.IsZero() {
			ev.Timestamp = time.Now().UTC()
		}
		if ev.EventID == "" {
			ev.EventID = fmt.Sprintf("%s-%d", spec.TaskID, seq)
		}
		_ = conn.WriteJSON(protocol.Envelope{V: 1, Type: protocol.MsgTaskEvent, Timestamp: time.Now().UTC(), Payload: map[string]any{"task_id": spec.TaskID, "event": ev}})
	}
	emit(protocol.Event{EventType: protocol.EvRunStarted, Origin: "omnoda", Payload: map[string]any{"kind": spec.Kind}})
	timeout := time.Duration(spec.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Minute
	}
	tctx, cancel2 := context.WithTimeout(tctx, timeout)
	defer cancel2()
	switch spec.Kind {
	case "harness":
		d.runHarness(tctx, conn, spec, emit)
	case "native":
		d.runNative(tctx, conn, spec, emit)
	default:
		d.runCommand(tctx, conn, spec, emit)
	}
}

func (d *Daemon) runCommand(ctx context.Context, conn *websocket.Conn, spec protocol.TaskSpec, emit func(protocol.Event)) {
	argv := []string{}
	if spec.Command != "" {
		argv = append(argv, spec.Command)
	}
	argv = append(argv, spec.Args...)
	dir := spec.Workspace
	if dir == "" && len(d.pol.Workspaces) > 0 {
		dir = d.pol.Workspaces[0]
	}
	res := rt.Run(ctx, d.pol, rt.Request{
		Argv: argv, Dir: dir, Timeout: time.Duration(spec.TimeoutSec) * time.Second, Level: spec.Level,
		OnLine: func(line string) {
			emit(protocol.Event{EventType: protocol.EvLog, Origin: "omnoda", Payload: map[string]any{"line": line}})
		},
	})
	status := protocol.TaskSucceeded
	errStr := ""
	if res.Err != nil {
		errStr = res.Err.Error()
		status = protocol.TaskFailed
		if res.TimedOut {
			status = protocol.TaskTimedOut
		}
		if ctx.Err() == context.Canceled {
			status = protocol.TaskCancelled
		}
	} else if res.ExitCode != 0 {
		status = protocol.TaskFailed
	}
	if status == protocol.TaskSucceeded {
		emit(protocol.Event{EventType: protocol.EvRunCompleted, Origin: "omnoda", Payload: map[string]any{"exit_code": res.ExitCode}})
	} else {
		emit(protocol.Event{EventType: protocol.EvRunFailed, Origin: "omnoda", Payload: map[string]any{"error": errStr, "exit_code": res.ExitCode}})
	}
	d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: status, ExitCode: res.ExitCode, Output: res.Output, Summary: firstLine(res.Output), Error: errStr})
}

func (d *Daemon) runHarness(ctx context.Context, conn *websocket.Conn, spec protocol.TaskSpec, emit func(protocol.Event)) {
	ad, err := adapters.ByID(spec.HarnessID)
	if err != nil {
		d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, ExitCode: 127, Error: err.Error()})
		return
	}
	det := ad.Detect(ctx)
	if !det.Info.Present {
		d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, ExitCode: 127, Error: spec.HarnessID + " is not installed: " + det.Info.Error})
		return
	}
	ws := spec.Workspace
	if ws == "" && len(d.pol.Workspaces) > 0 {
		ws = d.pol.Workspaces[0]
	}
	if ws != "" {
		if _, err := d.pol.ResolveWorkspace(ws); err != nil {
			d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, ExitCode: 126, Error: err.Error()})
			return
		}
	}
	if spec.Level == protocol.LevelReadOnly {
		d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, ExitCode: 126, Error: "harness runs require WORKSPACE_WRITE approval"})
		return
	}
	res, err := ad.Start(ctx, adapters.RunRequest{RunID: spec.TaskID, Prompt: spec.Prompt, Workspace: ws, Timeout: time.Duration(spec.TimeoutSec) * time.Second}, emit)
	if err != nil {
		d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, Error: err.Error()})
		return
	}
	status := protocol.TaskSucceeded
	errStr := ""
	if res.Err != nil || res.ExitCode != 0 {
		status = protocol.TaskFailed
		if res.Err != nil {
			errStr = res.Err.Error()
		}
		if res.ExitCode == 124 {
			status = protocol.TaskTimedOut
		}
	}
	d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: status, ExitCode: res.ExitCode, Summary: res.Summary, Output: res.Summary, Error: errStr, Session: res.Session})
}

func (d *Daemon) runNative(ctx context.Context, conn *websocket.Conn, spec protocol.TaskSpec, emit func(protocol.Event)) {
	if d.Cfg.ModelAPIKey == "" || d.Cfg.ModelBaseURL == "" {
		d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, ExitCode: 1, Error: "native agent model provider is not configured"})
		return
	}
	ws := spec.Workspace
	if ws == "" && len(d.pol.Workspaces) > 0 {
		ws = d.pol.Workspaces[0]
	}
	answer, err := agent.Run(ctx, agent.Config{
		BaseURL: d.Cfg.ModelBaseURL, Model: d.Cfg.ModelName, APIKey: d.Cfg.ModelAPIKey,
		MaxSteps: 8, Timeout: time.Duration(spec.TimeoutSec) * time.Second, TokenBudget: 8000,
	}, spec.Prompt, agent.ToolContext{Policy: d.pol, Workspace: ws}, emit)
	if err != nil {
		d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskFailed, Error: err.Error()})
		return
	}
	d.finish(conn, protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskSucceeded, Summary: answer, Output: answer})
}

func (d *Daemon) finish(conn *websocket.Conn, res protocol.TaskResult) {
	_ = conn.WriteJSON(protocol.Envelope{V: 1, Type: protocol.MsgTaskResult, Timestamp: time.Now().UTC(), Payload: mustMap(res)})
}

func (d *Daemon) hello() protocol.NodeHello {
	host, _ := os.Hostname()
	name := d.Cfg.DisplayName
	if name == "" {
		name = host
	}
	return protocol.NodeHello{
		NodeID: d.nodeID, DisplayName: name, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Version: d.Cfg.Version, Hostname: host, Capabilities: probeCaps(),
		Harnesses: adapters.DetectAll(context.Background()), Workspaces: d.Cfg.Workspaces,
		Labels: d.Cfg.Labels, PublicKey: identity.PublicB64(identity.PublicFromPrivate(d.key)),
	}
}

func probeCaps() []protocol.Capability {
	caps := []protocol.Capability{
		{Name: "os", Present: true, Detail: runtime.GOOS + "/" + runtime.GOARCH},
		{Name: "cpu", Present: true, Detail: fmt.Sprintf("%d", runtime.NumCPU())},
	}
	for _, bin := range []string{"git", "python3", "node"} {
		_, err := exec.LookPath(bin)
		caps = append(caps, protocol.Capability{Name: bin, Present: err == nil})
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	caps = append(caps, protocol.Capability{Name: "memory_alloc", Present: true, Detail: fmt.Sprintf("%d", m.Alloc)})
	return caps
}

func (d *Daemon) Doctor() []string {
	var lines []string
	if err := d.Load(); err != nil {
		return []string{"identity: FAIL " + err.Error()}
	}
	lines = append(lines, "identity: ok "+d.nodeID)
	st, err := os.Stat(filepath.Join(d.Cfg.ConfigDir, "node.key"))
	if err != nil {
		lines = append(lines, "key file: missing")
	} else if st.Mode().Perm()&0o077 != 0 {
		lines = append(lines, "key file: WARN permissions "+st.Mode().Perm().String())
	} else {
		lines = append(lines, "key file: ok mode "+st.Mode().Perm().String())
	}
	if d.Cfg.Server == "" {
		lines = append(lines, "server: not configured")
	} else {
		lines = append(lines, "server: "+d.Cfg.Server)
	}
	for _, h := range adapters.DetectAll(context.Background()) {
		if h.Present {
			lines = append(lines, fmt.Sprintf("harness %s: %s (%s)", h.ID, h.Version, h.Path))
		} else {
			lines = append(lines, fmt.Sprintf("harness %s: not installed", h.ID))
		}
	}
	if len(d.Cfg.Workspaces) == 0 {
		lines = append(lines, "workspaces: none authorized")
	}
	for _, w := range d.Cfg.Workspaces {
		if st, err := os.Stat(w); err != nil || !st.IsDir() {
			lines = append(lines, "workspace missing: "+w)
		} else {
			lines = append(lines, "workspace: "+w)
		}
	}
	lines = append(lines, "privileged: "+fmt.Sprint(d.Cfg.AllowPrivileged))
	return lines
}

func mustMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if len(s) > 400 {
		return s[:400]
	}
	return s
}
