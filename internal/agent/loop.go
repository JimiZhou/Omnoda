// Package agent is the built-in loop used when no external harness is installed.
// If the model provider is not configured, it returns an error. It never invents a result.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/policy"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/runtime"
)

type Config struct {
	BaseURL     string
	Model       string
	APIKey      string
	MaxSteps    int
	Timeout     time.Duration
	TokenBudget int
}

type ToolContext struct {
	Policy    policy.Policy
	Workspace string
}

func Run(ctx context.Context, cfg Config, goal string, tools ToolContext, emit func(protocol.Event)) (string, error) {
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
	cctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	sys := `You are Omnoda Native Agent running on the device owner's machine.
Reply with JSON only: {"final":"..."} or {"tool":"system_info|read_file|list_directory|execute_command|git_status","args":{}}.
execute_command args: {"argv":["binary","arg"]}. Paths must stay inside the workspace.
Never request credentials. Max tools are limited.`
	var messages []map[string]string
	messages = append(messages, map[string]string{"role": "system", "content": sys})
	messages = append(messages, map[string]string{"role": "user", "content": goal})
	used := 0
	for step := 1; step <= cfg.MaxSteps; step++ {
		if used > cfg.TokenBudget {
			return "", errors.New("token budget exceeded")
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
			return "", fmt.Errorf("model did not return a valid agent decision")
		}
		if decision.Final != "" {
			return decision.Final, nil
		}
		emit(protocol.Event{EventType: protocol.EvToolStarted, Sequence: int64(step), Timestamp: time.Now().UTC(), Payload: map[string]any{"tool": decision.Tool}, Origin: "omnoda"})
		out, err := execTool(cctx, tools, decision.Tool, decision.Args)
		if err != nil {
			out = "error: " + err.Error()
		}
		if len(out) > 8000 {
			out = out[:8000] + "…[truncated]"
		}
		emit(protocol.Event{EventType: protocol.EvToolCompleted, Sequence: int64(step), Timestamp: time.Now().UTC(), Payload: map[string]any{"tool": decision.Tool, "output": out}, Origin: "omnoda"})
		messages = append(messages, map[string]string{"role": "assistant", "content": content})
		messages = append(messages, map[string]string{"role": "user", "content": "Tool result: " + out})
	}
	return "", errors.New("native agent exceeded max steps without a final answer")
}

func execTool(ctx context.Context, tc ToolContext, name string, args map[string]any) (string, error) {
	switch name {
	case "system_info":
		r := runtime.Run(ctx, tc.Policy, runtime.Request{Argv: []string{"uname", "-a"}, Dir: tc.Workspace, Level: protocol.LevelReadOnly, Timeout: 10 * time.Second})
		if r.Err != nil && r.Output == "" {
			return "", r.Err
		}
		return r.Output, nil
	case "git_status":
		r := runtime.Run(ctx, tc.Policy, runtime.Request{Argv: []string{"git", "status", "--short"}, Dir: tc.Workspace, Level: protocol.LevelReadOnly, Timeout: 15 * time.Second})
		return r.Output, r.Err
	case "list_directory":
		rel, _ := args["path"].(string)
		if rel == "" {
			rel = "."
		}
		full, err := runtime.SafeJoin(tc.Workspace, rel)
		if err != nil {
			return "", err
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		for i, e := range entries {
			if i > 200 {
				b.WriteString("…\n")
				break
			}
			b.WriteString(e.Name())
			b.WriteByte('\n')
		}
		return b.String(), nil
	case "read_file":
		rel, _ := args["path"].(string)
		full, err := runtime.SafeJoin(tc.Workspace, rel)
		if err != nil {
			return "", err
		}
		f, err := os.Open(full)
		if err != nil {
			return "", err
		}
		defer f.Close()
		buf, err := io.ReadAll(io.LimitReader(f, 64*1024))
		return string(buf), err
	case "execute_command":
		raw, _ := json.Marshal(args["argv"])
		var argv []string
		if json.Unmarshal(raw, &argv) != nil || len(argv) == 0 {
			return "", errors.New("argv required")
		}
		r := runtime.Run(ctx, tc.Policy, runtime.Request{Argv: argv, Dir: tc.Workspace, Level: protocol.LevelReadOnly, Timeout: 20 * time.Second})
		if r.Err != nil {
			return r.Output, r.Err
		}
		return r.Output, nil
	default:
		return "", errors.New("tool not allowed: " + name)
	}
}

func chat(ctx context.Context, cfg Config, messages []map[string]string) (string, error) {
	body := map[string]any{
		"model": cfg.Model, "messages": messages, "temperature": 0,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("model provider status %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("model returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '{'); i >= 0 {
		if j := strings.LastIndexByte(s, '}'); j > i {
			return s[i : j+1]
		}
	}
	return s
}
