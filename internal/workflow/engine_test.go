package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/scheduler"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

type fakeRun struct {
	fn func(spec protocol.TaskSpec) protocol.TaskResult
}

func (f fakeRun) Run(ctx context.Context, nodeID string, spec protocol.TaskSpec) (protocol.TaskResult, error) {
	return f.fn(spec), nil
}

func TestDAGSkipFixWhenHealthy(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	eng := New(db, fakeRun{fn: func(spec protocol.TaskSpec) protocol.TaskResult {
		if spec.StepID == "diagnose" {
			return protocol.TaskResult{TaskID: spec.TaskID, Status: protocol.TaskSucceeded, ExitCode: 0, Output: "HEALTHY\n", Summary: "HEALTHY"}
		}
		t.Errorf("unexpected step %s", spec.StepID)
		return protocol.TaskResult{Status: protocol.TaskFailed}
	}})
	eng.Load = func() []scheduler.Candidate {
		return []scheduler.Candidate{{Node: storage.Node{ID: "arm", DisplayName: "arm", Status: protocol.NodeOnline, Arch: "arm64", OS: "linux", Labels: map[string]string{"role": "monitor"}, Workspaces: []string{"/tmp"}}}}
	}
	plan := protocol.Plan{Goal: "check", Planner: "template", Steps: []protocol.PlanStep{
		{StepID: "diagnose", Type: "command", Selector: protocol.NodeSelector{Role: "monitor"}, Input: map[string]any{"argv": []string{"true"}}, TimeoutSec: 5, Level: protocol.LevelReadOnly, Retry: protocol.RetryPolicy{Class: protocol.RetrySafe}},
		{StepID: "fix", Type: "command", Dependencies: []string{"diagnose"}, Condition: "diagnose.unhealthy", Selector: protocol.NodeSelector{NodeID: "arm"}, Input: map[string]any{"argv": []string{"true"}}, Level: protocol.LevelReadOnly},
		{StepID: "report", Type: "report", Dependencies: []string{"diagnose", "fix"}, Level: protocol.LevelReadOnly},
	}}
	res, err := eng.Start(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w, _ := db.GetWorkflow(res.ID)
		if w.Status == "succeeded" || w.Status == "failed" {
			if w.Status != "succeeded" {
				t.Fatal(w.Status, w.Report)
			}
			steps, _ := db.Steps(res.ID)
			for _, s := range steps {
				if s.StepID == "fix" && s.Status != protocol.StepSkipped {
					t.Fatal("fix not skipped", s.Status)
				}
			}
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timeout")
}
