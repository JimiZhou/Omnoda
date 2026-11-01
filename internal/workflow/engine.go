// Package workflow runs a persisted DAG. Side-effecting steps are not replayed
// after a crash; uncertain steps stay uncertain until a person looks at them.
package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/scheduler"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

// Runner executes one concrete task on a node and waits for a terminal result.
type Runner interface {
	Run(ctx context.Context, nodeID string, spec protocol.TaskSpec) (protocol.TaskResult, error)
}

type Engine struct {
	DB     *storage.DB
	Run    Runner
	Load   func() []scheduler.Candidate
	mu     sync.Mutex
	cancel map[string]context.CancelFunc
}

func New(db *storage.DB, run Runner) *Engine {
	return &Engine{DB: db, Run: run, cancel: map[string]context.CancelFunc{}}
}

type StartResult struct {
	ID   string
	Plan protocol.Plan
}

func (e *Engine) Start(ctx context.Context, plan protocol.Plan) (StartResult, error) {
	id := storage.NewID("wf")
	b, _ := json.Marshal(plan)
	if err := e.DB.InsertWorkflow(id, plan.Goal, plan.Planner, string(b)); err != nil {
		return StartResult{}, err
	}
	for _, s := range plan.Steps {
		in, _ := json.Marshal(s.Input)
		st := protocol.StepPending
		if len(s.Dependencies) == 0 {
			st = protocol.StepReady
		}
		if err := e.DB.UpsertStep(storage.StepRow{
			WorkflowID: id, StepID: s.StepID, Status: st, InputJSON: string(in),
		}); err != nil {
			return StartResult{}, err
		}
	}
	e.DB.Audit("system", "", "", id, "workflow.start", "", "ok", plan.Planner)
	runCtx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.cancel[id] = cancel
	e.mu.Unlock()
	go e.loop(runCtx, id, plan)
	return StartResult{ID: id, Plan: plan}, nil
}

func (e *Engine) Cancel(id string) {
	e.mu.Lock()
	if c, ok := e.cancel[id]; ok {
		c()
	}
	e.mu.Unlock()
	_ = e.DB.UpdateWorkflowStatus(id, "cancelled", "")
}

func (e *Engine) loop(ctx context.Context, id string, plan protocol.Plan) {
	defer func() {
		e.mu.Lock()
		delete(e.cancel, id)
		e.mu.Unlock()
	}()
	_ = e.DB.UpdateWorkflowStatus(id, "running", "")
	byID := map[string]protocol.PlanStep{}
	for _, s := range plan.Steps {
		byID[s.StepID] = s
	}
	outputs := map[string]map[string]any{}
	for {
		if ctx.Err() != nil {
			return
		}
		steps, err := e.DB.Steps(id)
		if err != nil {
			_ = e.DB.UpdateWorkflowStatus(id, "failed", err.Error())
			return
		}
		state := map[string]protocol.StepStatus{}
		for _, s := range steps {
			state[s.StepID] = s.Status
			if s.OutputJSON != "" {
				var m map[string]any
				if json.Unmarshal([]byte(s.OutputJSON), &m) == nil {
					outputs[s.StepID] = m
				}
			}
		}
		if done, status := terminal(plan, state); done {
			report := e.report(id, plan, steps, outputs, status)
			rb, _ := json.Marshal(report)
			_ = e.DB.UpdateWorkflowStatus(id, status, string(rb))
			return
		}
		progressed := false
		var ready []protocol.PlanStep
		for _, s := range plan.Steps {
			st := state[s.StepID]
			if st != protocol.StepPending && st != protocol.StepReady {
				continue
			}
			if depFailed(s, state) {
				_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: s.StepID, Status: protocol.StepSkipped, InputJSON: mustRaw(s.Input), Error: "dependency failed"})
				progressed = true
				continue
			}
			if !depsDone(s, state) {
				continue
			}
			if !conditionMet(s.Condition, outputs, state) {
				_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: s.StepID, Status: protocol.StepSkipped, InputJSON: mustRaw(s.Input)})
				progressed = true
				continue
			}
			ready = append(ready, s)
		}
		if len(ready) == 0 {
			if !progressed {
				// Waiting on approval or running steps.
				if waiting(state) {
					select {
					case <-ctx.Done():
						return
					case <-time.After(400 * time.Millisecond):
					}
					continue
				}
				_ = e.DB.UpdateWorkflowStatus(id, "failed", "")
				return
			}
			continue
		}
		var wg sync.WaitGroup
		for _, step := range ready {
			step := step
			wg.Add(1)
			go func() {
				defer wg.Done()
				e.execStep(ctx, id, step, outputs)
			}()
		}
		wg.Wait()
	}
}

func (e *Engine) execStep(ctx context.Context, id string, step protocol.PlanStep, outputs map[string]map[string]any) {
	if step.Type == "report" {
		_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepSucceeded, InputJSON: mustRaw(step.Input), OutputJSON: `{"reported":true}`})
		return
	}
	var nodeID, reason string
	if e.Load != nil && step.Type != "report" {
		place, err := scheduler.Place(step.Selector, e.Load())
		if err != nil {
			_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepFailed, Error: err.Error(), InputJSON: mustRaw(step.Input)})
			return
		}
		nodeID, reason = place.NodeID, place.Reason
		if ws, _ := step.Input["workspace"].(string); ws == "" {
			for _, c := range e.Load() {
				if c.Node.ID == nodeID && len(c.Node.Workspaces) > 0 {
					if step.Input == nil {
						step.Input = map[string]any{}
					}
					step.Input["workspace"] = c.Node.Workspaces[0]
				}
			}
		}
	}
	if step.Approval == "required" {
		args, _ := json.Marshal(step.Input)
		ap, err := e.DB.CreateApproval(storage.Approval{
			WorkflowID: id, StepID: step.StepID, NodeID: nodeID, Action: step.Type, ArgsJSON: string(args),
		}, 30*time.Minute)
		if err != nil {
			_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepFailed, Error: err.Error()})
			return
		}
		_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepWaitingApproval, NodeID: nodeID, Reason: reason, InputJSON: mustRaw(step.Input)})
		deadline := time.Now().Add(30 * time.Minute)
		for {
			if ctx.Err() != nil || time.Now().After(deadline) {
				_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepCancelled, NodeID: nodeID, Reason: reason, Error: "approval not granted"})
				return
			}
			a, err := e.DB.GetApproval(ap.ID)
			if err == nil && a.Status == "approved" {
				break
			}
			if err == nil && (a.Status == "rejected" || a.Status == "expired") {
				_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepCancelled, NodeID: nodeID, Reason: reason, Error: "approval " + a.Status})
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
	attempts := step.Retry.MaxAttempts + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr string
	for attempt := 1; attempt <= attempts; attempt++ {
		if step.Retry.Class == protocol.RetryNever && attempt > 1 {
			break
		}
		if step.Retry.Class == protocol.RetryConfirm && attempt > 1 {
			// Controlled retry is allowed once by the plan's max_attempts.
			// The class records that a human should notice it; POC auto-allows the single budgeted retry
			// and writes that fact into the step error trail.
			e.DB.Audit("system", nodeID, "", id, "workflow.retry", string(step.Level), "confirm-budget", step.StepID)
		}
		spec := e.buildSpec(id, step, nodeID, outputs)
		_ = e.DB.UpsertStep(storage.StepRow{
			WorkflowID: id, StepID: step.StepID, Status: protocol.StepRunning,
			NodeID: nodeID, Reason: reason, TaskID: spec.TaskID, InputJSON: mustRaw(step.Input), Attempt: attempt,
		})
		if e.Run == nil {
			lastErr = "no runner"
			break
		}
		res, err := e.Run.Run(ctx, nodeID, spec)
		if err != nil {
			lastErr = err.Error()
			if step.Retry.Class == protocol.RetrySafe && attempt < attempts {
				continue
			}
			break
		}
		out := map[string]any{
			"status": res.Status, "summary": res.Summary, "exit_code": res.ExitCode,
			"output": clip(res.Output, 8000), "error": res.Error,
		}
		blob := strings.ToLower(res.Output + "\n" + res.Summary)
		if strings.Contains(blob, "unhealthy") || (res.ExitCode != 0 && step.StepID == "diagnose") {
			out["unhealthy"] = true
		} else if strings.Contains(blob, "healthy") && res.ExitCode == 0 {
			out["unhealthy"] = false
		}
		ob, _ := json.Marshal(out)
		st := protocol.StepSucceeded
		if res.Status != protocol.TaskSucceeded {
			st = protocol.StepFailed
			if res.Status == protocol.TaskTimedOut {
				st = protocol.StepTimedOut
			}
			if res.Status == protocol.TaskCancelled {
				st = protocol.StepCancelled
			}
			if res.Status == protocol.TaskUncertain {
				st = protocol.StepUncertain
			}
			lastErr = res.Error
			if st == protocol.StepUncertain || step.Retry.Class == protocol.RetryNever {
				_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: st, NodeID: nodeID, Reason: reason, TaskID: spec.TaskID, InputJSON: mustRaw(step.Input), OutputJSON: string(ob), Attempt: attempt, Error: lastErr})
				return
			}
			if attempt < attempts && step.Retry.Class == protocol.RetrySafe {
				continue
			}
			_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: st, NodeID: nodeID, Reason: reason, TaskID: spec.TaskID, InputJSON: mustRaw(step.Input), OutputJSON: string(ob), Attempt: attempt, Error: lastErr})
			return
		}
		_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepSucceeded, NodeID: nodeID, Reason: reason, TaskID: spec.TaskID, InputJSON: mustRaw(step.Input), OutputJSON: string(ob), Attempt: attempt})
		return
	}
	_ = e.DB.UpsertStep(storage.StepRow{WorkflowID: id, StepID: step.StepID, Status: protocol.StepFailed, NodeID: nodeID, Reason: reason, InputJSON: mustRaw(step.Input), Error: lastErr})
}

func (e *Engine) buildSpec(wf string, step protocol.PlanStep, nodeID string, outputs map[string]map[string]any) protocol.TaskSpec {
	timeout := step.TimeoutSec
	if timeout <= 0 {
		timeout = 60
	}
	spec := protocol.TaskSpec{
		TaskID: storage.NewID("task"), WorkflowID: wf, StepID: step.StepID,
		TimeoutSec: timeout, Level: step.Level, Idempotency: storage.NewID("idem"),
	}
	switch step.Type {
	case "harness":
		spec.Kind = "harness"
		if h, ok := step.Input["harness_id"].(string); ok {
			spec.HarnessID = h
		}
		spec.Prompt = promptFor(step, outputs)
	case "native":
		spec.Kind = "native"
		spec.Prompt = promptFor(step, outputs)
	default:
		spec.Kind = "command"
		if raw, ok := step.Input["argv"]; ok {
			b, _ := json.Marshal(raw)
			var argv []string
			_ = json.Unmarshal(b, &argv)
			if len(argv) > 0 {
				spec.Command = argv[0]
				if len(argv) > 1 {
					spec.Args = argv[1:]
				}
			}
		}
	}
	if ws, ok := step.Input["workspace"].(string); ok {
		spec.Workspace = ws
	}
	if spec.Kind == "command" && spec.Command != "" && !filepathIsAbs(spec.Command) {
		if rel, _ := step.Input["workspace_relative"].(bool); rel && spec.Workspace != "" {
			spec.Command = spec.Workspace + "/" + spec.Command
		}
	}
	return spec
}

func filepathIsAbs(p string) bool {
	return len(p) > 0 && p[0] == '/' || (len(p) > 2 && p[1] == ':')
}

func promptFor(step protocol.PlanStep, outputs map[string]map[string]any) string {
	if p, ok := step.Input["prompt"].(string); ok && p != "" {
		return p
	}
	diag := outputs["diagnose"]
	b, _ := json.Marshal(diag)
	return "The demo service health check returned: " + string(b) + "\nFix the bug in app.py so /health returns 200 with {\"status\":\"ok\"}. Change only files inside this workspace. Do not touch secrets. Summarize the diff."
}

func depFailed(s protocol.PlanStep, state map[string]protocol.StepStatus) bool {
	for _, d := range s.Dependencies {
		switch state[d] {
		case protocol.StepFailed, protocol.StepTimedOut, protocol.StepCancelled, protocol.StepUncertain:
			return true
		}
	}
	return false
}

func depsDone(s protocol.PlanStep, state map[string]protocol.StepStatus) bool {
	for _, d := range s.Dependencies {
		st := state[d]
		if st != protocol.StepSucceeded && st != protocol.StepSkipped {
			return false
		}
	}
	return true
}

func conditionMet(cond string, outputs map[string]map[string]any, state map[string]protocol.StepStatus) bool {
	if cond == "" {
		return true
	}
	if cond == "diagnose.unhealthy" {
		if state["diagnose"] == protocol.StepSkipped {
			return false
		}
		o := outputs["diagnose"]
		if o == nil {
			return false
		}
		v, ok := o["unhealthy"].(bool)
		return ok && v
	}
	return false
}

func waiting(state map[string]protocol.StepStatus) bool {
	for _, st := range state {
		switch st {
		case protocol.StepRunning, protocol.StepWaitingApproval, protocol.StepReady, protocol.StepPending:
			return true
		}
	}
	return false
}

func terminal(plan protocol.Plan, state map[string]protocol.StepStatus) (bool, string) {
	anyFail := false
	for _, s := range plan.Steps {
		st := state[s.StepID]
		switch st {
		case protocol.StepSucceeded, protocol.StepSkipped:
			continue
		case protocol.StepFailed, protocol.StepTimedOut, protocol.StepCancelled, protocol.StepUncertain:
			// If later steps depend on it they will not run; we can finish once nothing is still active.
			anyFail = true
		default:
			return false, ""
		}
	}
	if anyFail {
		return true, "failed"
	}
	return true, "succeeded"
}

func (e *Engine) report(id string, plan protocol.Plan, steps []storage.StepRow, outputs map[string]map[string]any, status string) map[string]any {
	var nodes []string
	seen := map[string]bool{}
	timeline := []map[string]any{}
	for _, s := range steps {
		if s.NodeID != "" && !seen[s.NodeID] {
			seen[s.NodeID] = true
			nodes = append(nodes, s.NodeID)
		}
		timeline = append(timeline, map[string]any{
			"step_id": s.StepID, "status": s.Status, "node_id": s.NodeID, "reason": s.Reason, "task_id": s.TaskID, "error": s.Error,
		})
	}
	return map[string]any{
		"workflow_id":    id,
		"goal":           plan.Goal,
		"planner":        plan.Planner,
		"executed_nodes": nodes,
		"timeline":       timeline,
		"outputs":        outputs,
		"final_status":   status,
	}
}

func mustRaw(v any) string {
	b, _ := json.Marshal(v)
	if string(b) == "null" {
		return "{}"
	}
	return string(b)
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Explain is a small helper for the API.
func Explain(r scheduler.Result) string { return fmt.Sprintf("%s (%s)", r.NodeID, r.Reason) }
