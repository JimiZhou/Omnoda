// Package planner turns a goal into a schema-checked plan.
// Template plans are never presented as LLM intelligence.
package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
)

type Request struct {
	Goal   string
	Nodes  []NodeView
	Prefer string // auto | template | llm
}

type NodeView struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	OS        string            `json:"os"`
	Arch      string            `json:"arch"`
	Status    string            `json:"status"`
	Labels    map[string]string `json:"labels"`
	Harnesses []string          `json:"harnesses"`
}

type LLMConfig struct {
	BaseURL string
	Model   string
	APIKey  string
}

func Plan(ctx context.Context, req Request, llm *LLMConfig) (protocol.Plan, error) {
	if req.Prefer == "template" || llm == nil || llm.APIKey == "" || llm.BaseURL == "" {
		return Template(req.Goal, req.Nodes), nil
	}
	p, err := llmPlan(ctx, req, *llm)
	if err != nil {
		return protocol.Plan{}, err
	}
	if err := Validate(p); err != nil {
		return protocol.Plan{}, err
	}
	return p, nil
}

// Template matches known demo phrases. Unknown goals become a single
// read-only diagnose step on an auto-selected node. The planner name is "template".
func Template(goal string, nodes []NodeView) protocol.Plan {
	g := strings.ToLower(goal)
	if isCrossDemo(g) {
		return crossDemo(goal)
	}
	return protocol.Plan{
		Goal: goal, Planner: "template",
		Steps: []protocol.PlanStep{{
			StepID: "diagnose", Type: "command", Title: "Read-only system diagnose",
			Capabilities: []string{"system_info"},
			Selector:     protocol.NodeSelector{},
			Input: map[string]any{
				"argv": []string{"uname", "-a"},
			},
			TimeoutSec: 30,
			Retry:      protocol.RetryPolicy{MaxAttempts: 1, Class: protocol.RetrySafe},
			Approval:   "none",
			Level:      protocol.LevelReadOnly,
		}},
	}
}

func isCrossDemo(g string) bool {
	return (strings.Contains(g, "arm") || strings.Contains(g, "诊断") || strings.Contains(g, "检查")) &&
		(strings.Contains(g, "mac") || strings.Contains(g, "修复") || strings.Contains(g, "windows") || strings.Contains(g, "测试") || strings.Contains(g, "demo"))
}

func crossDemo(goal string) protocol.Plan {
	return protocol.Plan{
		Goal: goal, Planner: "template",
		Steps: []protocol.PlanStep{
			{
				StepID: "diagnose", Type: "command", Title: "Diagnose demo service on ARM",
				Dependencies: nil,
				Capabilities: []string{"execute_command"},
				Selector:     protocol.NodeSelector{Role: "monitor", Arch: "arm64"},
				Input: map[string]any{
					"argv":               []string{"python3", "healthcheck.py"},
					"workspace_relative": true,
				},
				TimeoutSec: 30,
				Retry:      protocol.RetryPolicy{MaxAttempts: 1, Class: protocol.RetrySafe},
				Approval:   "none",
				Level:      protocol.LevelReadOnly,
			},
			{
				StepID: "fix", Type: "harness", Title: "Fix demo service with Claude Code",
				Dependencies: []string{"diagnose"},
				Capabilities: []string{"claude-code"},
				Selector:     protocol.NodeSelector{Role: "developer", Harness: "claude-code"},
				Condition:    "diagnose.unhealthy",
				Input: map[string]any{
					"harness_id":  "claude-code",
					"prompt_from": "diagnose",
				},
				TimeoutSec: 600,
				Retry:      protocol.RetryPolicy{MaxAttempts: 0, Class: protocol.RetryNever},
				Approval:   "required",
				Level:      protocol.LevelWorkspaceWrite,
			},
			{
				StepID: "test", Type: "command", Title: "Run tests on Windows",
				Dependencies: []string{"fix"},
				Capabilities: []string{"execute_command"},
				Selector:     protocol.NodeSelector{Role: "tester"},
				Condition:    "diagnose.unhealthy",
				Input: map[string]any{
					"argv":               []string{"python3", "run_tests.py"},
					"workspace_relative": true,
				},
				TimeoutSec: 180,
				Retry:      protocol.RetryPolicy{MaxAttempts: 1, Class: protocol.RetryConfirm},
				Approval:   "none",
				Level:      protocol.LevelWorkspaceWrite,
			},
			{
				StepID: "report", Type: "report", Title: "Summarize workflow",
				Dependencies: []string{"diagnose", "fix", "test"},
				Selector:     protocol.NodeSelector{},
				TimeoutSec:   15,
				Retry:        protocol.RetryPolicy{MaxAttempts: 0, Class: protocol.RetrySafe},
				Approval:     "none",
				Level:        protocol.LevelReadOnly,
			},
		},
	}
}

func Validate(p protocol.Plan) error {
	if strings.TrimSpace(p.Goal) == "" {
		return errors.New("plan goal required")
	}
	if p.Planner != "llm" && p.Planner != "template" {
		return errors.New("planner must be llm or template")
	}
	if len(p.Steps) == 0 || len(p.Steps) > 16 {
		return errors.New("plan must contain 1 to 16 steps")
	}
	seen := map[string]bool{}
	for _, s := range p.Steps {
		if s.StepID == "" || seen[s.StepID] {
			return fmt.Errorf("bad step id %q", s.StepID)
		}
		seen[s.StepID] = true
		switch s.Level {
		case protocol.LevelReadOnly, protocol.LevelWorkspaceWrite:
		case protocol.LevelPrivileged:
			return errors.New("planner cannot request PRIVILEGED")
		default:
			return fmt.Errorf("step %s has invalid level", s.StepID)
		}
		switch s.Type {
		case "command", "harness", "native", "report", "builtin":
		default:
			return fmt.Errorf("step %s has invalid type", s.Type)
		}
		if s.TimeoutSec < 0 || s.TimeoutSec > 3600 {
			return fmt.Errorf("step %s timeout out of range", s.StepID)
		}
		if s.Retry.MaxAttempts < 0 || s.Retry.MaxAttempts > 2 {
			return fmt.Errorf("step %s retry out of range", s.StepID)
		}
	}
	for _, s := range p.Steps {
		for _, d := range s.Dependencies {
			if !seen[d] {
				return fmt.Errorf("step %s depends on unknown %s", s.StepID, d)
			}
		}
	}
	return nil
}

func llmPlan(ctx context.Context, req Request, cfg LLMConfig) (protocol.Plan, error) {
	nb, _ := json.Marshal(req.Nodes)
	sys := `You are Omnoda's planner. Return ONLY JSON with keys goal, planner, steps.
planner must be "llm".
Each step: step_id, type (command|harness|native|report), title, dependencies (array), required_capabilities, node_selector {node_id, role, os, arch, harness, labels}, input, condition, timeout_sec, retry_policy {max_attempts, class}, approval_policy (none|required), level (READ_ONLY|WORKSPACE_WRITE).
Never use PRIVILEGED. Never invent node ids that are not in the inventory. Max 8 steps.`
	user := "Goal: " + req.Goal + "\nNodes: " + string(nb)
	body := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": sys},
			{"role": "user", "content": user},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return protocol.Plan{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return protocol.Plan{}, fmt.Errorf("llm planner: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return protocol.Plan{}, fmt.Errorf("llm planner status %d", resp.StatusCode)
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return protocol.Plan{}, err
	}
	if len(parsed.Choices) == 0 {
		return protocol.Plan{}, errors.New("llm planner returned no choices")
	}
	var plan protocol.Plan
	if err := json.Unmarshal([]byte(parsed.Choices[0].Message.Content), &plan); err != nil {
		return protocol.Plan{}, fmt.Errorf("llm plan is not valid json: %w", err)
	}
	plan.Planner = "llm"
	plan.Goal = req.Goal
	return plan, nil
}
