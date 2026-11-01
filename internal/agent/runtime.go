package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/capabilities/executor"
	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
	"github.com/JimiZhou/Omnoda/internal/protocol"
)

// Host is the capability surface the native loop can see.
// Tools cannot enable a capability or grant network.
type Host struct {
	Reg       *registry.Registry
	Exec      *executor.Executor
	Workspace string
	Request   func(goal, missing string) (string, error)
}

func (h *Host) list() (string, error) {
	if h == nil || h.Reg == nil {
		return "[]", nil
	}
	rows, err := h.Reg.List()
	if err != nil {
		return "", err
	}
	type brief struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	var out []brief
	for _, r := range rows {
		out = append(out, brief{ID: r.CapabilityID, Version: r.Version, Status: string(r.ActivationStatus), Summary: r.Manifest.Metadata.Description})
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (h *Host) inspect(id string) (string, error) {
	rec, err := h.Reg.Get(id, "")
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{
		"id": rec.CapabilityID, "version": rec.Version, "status": rec.ActivationStatus,
		"inputs": json.RawMessage(rec.Manifest.Spec.Inputs), "permissions": rec.RequiredPermissions,
		"platforms": rec.Manifest.Spec.Platforms,
	})
	return string(b), nil
}

func (h *Host) invoke(ctx context.Context, id, version string, input map[string]any) (string, error) {
	if h.Exec == nil {
		h.Exec = &executor.Executor{Reg: h.Reg}
	}
	res := h.Exec.Invoke(ctx, executor.Request{ID: id, Version: version, Input: input, Workspace: h.Workspace})
	b, _ := json.Marshal(res)
	if !res.OK {
		return string(b), os.ErrInvalid
	}
	return string(b), nil
}

func systemPrompt(caps string) string {
	return `You are Omnoda Seed, a local agent.
Reply with JSON only.
To finish: {"final":"..."}.
To use a tool: {"tool":"list_capabilities|inspect_capability|invoke_capability|request_capability|read_file|list_directory","args":{}}.
invoke_capability args: {"id":"...","version":"...","input":{}}.
request_capability args: {"goal":"...","missing":"capability id"} when no enabled capability fits.
Enabled capabilities:
` + caps + `
Do not claim a capability exists if it is not in the list. Do not grant yourself permissions.`
}

// RunHost is the capability-aware loop. emit may be nil.
func RunHost(ctx context.Context, cfg Config, goal string, host *Host, tools ToolContext, emit func(protocol.Event)) (string, error) {
	if cfg.APIKey == "" || cfg.BaseURL == "" || cfg.Model == "" {
		return "", errNoModel
	}
	if cfg.MaxSteps <= 0 || cfg.MaxSteps > 16 {
		cfg.MaxSteps = 8
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}
	if cfg.TokenBudget <= 0 {
		cfg.TokenBudget = 8000
	}
	listed, _ := host.list()
	cctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	messages := []map[string]string{
		{"role": "system", "content": systemPrompt(listed)},
		{"role": "user", "content": goal},
	}
	if emit == nil {
		emit = func(protocol.Event) {}
	}
	used := 0
	for step := 1; step <= cfg.MaxSteps; step++ {
		if used > cfg.TokenBudget {
			return "", errBudget
		}
		content, err := chat(cctx, cfg, messages)
		if err != nil {
			return "", err
		}
		used += len(content) / 4
		emit(protocol.Event{EventType: protocol.EvMessageDelta, Sequence: int64(step), Timestamp: time.Now().UTC(), Payload: map[string]any{"text": content}, Origin: "omnoda"})
		var decision struct {
			Final string         `json:"final"`
			Tool  string         `json:"tool"`
			Args  map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(extractJSON(content)), &decision); err != nil || (decision.Final == "" && decision.Tool == "") {
			return "", errDecision
		}
		if decision.Final != "" {
			return decision.Final, nil
		}
		emit(protocol.Event{EventType: protocol.EvToolStarted, Sequence: int64(step), Timestamp: time.Now().UTC(), Payload: map[string]any{"tool": decision.Tool}, Origin: "omnoda"})
		out, err := dispatch(cctx, host, tools, decision.Tool, decision.Args)
		if err != nil {
			out = "error: " + err.Error() + " " + out
		}
		if len(out) > 8000 {
			out = out[:8000]
		}
		emit(protocol.Event{EventType: protocol.EvToolCompleted, Sequence: int64(step), Timestamp: time.Now().UTC(), Payload: map[string]any{"tool": decision.Tool, "output": out}, Origin: "omnoda"})
		messages = append(messages, map[string]string{"role": "assistant", "content": content}, map[string]string{"role": "user", "content": "Tool result: " + out})
	}
	return "", errSteps
}

func dispatch(ctx context.Context, host *Host, tools ToolContext, name string, args map[string]any) (string, error) {
	switch name {
	case "list_capabilities":
		return host.list()
	case "inspect_capability":
		id, _ := args["id"].(string)
		return host.inspect(id)
	case "invoke_capability":
		id, _ := args["id"].(string)
		ver, _ := args["version"].(string)
		input, _ := args["input"].(map[string]any)
		return host.invoke(ctx, id, ver, input)
	case "request_capability":
		if host.Request == nil {
			return "", errNoEvolver
		}
		goal, _ := args["goal"].(string)
		missing, _ := args["missing"].(string)
		return host.Request(goal, missing)
	default:
		if strings.HasPrefix(name, "agent.") {
			return dispatch(ctx, host, tools, strings.TrimPrefix(name, "agent."), args)
		}
		return execTool(ctx, tools, name, args)
	}
}
