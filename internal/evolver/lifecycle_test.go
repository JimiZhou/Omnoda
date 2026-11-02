package evolver

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
	"github.com/JimiZhou/Omnoda/internal/sandbox"
)

func TestEvolutionRequiresApproval(t *testing.T) {
	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(t.TempDir(), reg)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.Start("parse OMN1 logs", "local.sensor-log", nil)
	if err != nil {
		t.Fatal(err)
	}
	// No generator: fail closed. Do not invent a capability.
	got, err := st.Run(context.Background(), rec.ID, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StFailed || got.Error == "" {
		t.Fatalf("state %s", got.State)
	}
}

func TestFixtureEvolutionSeparatedTests(t *testing.T) {
	if _, err := lookPython(); err != nil {
		t.Skip("python3 required")
	}
	if !sandboxAvailable() {
		t.Skip("sandbox backend required for a real independent verification; fail-closed is TestFixtureEvolutionFailClosed")
	}
	reg, _ := registry.Open(t.TempDir())
	st, _ := Open(t.TempDir(), reg)
	rec, _ := st.Start("parse OMN1 logs", "local.sensor-log", nil)
	got, err := st.Run(context.Background(), rec.ID, FixtureGenerator{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StAwaitingApproval {
		t.Fatalf("state %s err %s results %#v", got.State, got.Error, got.TestResults)
	}
	if !got.TestResults["builder"] || !got.TestResults["verifier"] {
		t.Fatal(got.TestResults)
	}
	// Not enabled yet.
	active, err := reg.Get("local.sensor-log", "")
	if err != nil {
		t.Fatal(err)
	}
	if active.ActivationStatus == registry.StatusEnabled {
		t.Fatal("activated before approval")
	}
	approved, err := st.Approve(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != StActivated {
		t.Fatal(approved.State)
	}
	active, _ = reg.Get("local.sensor-log", "")
	if active.ActivationStatus != registry.StatusEnabled {
		t.Fatal(active.ActivationStatus)
	}
	// Second session finds it and must not start another evolution that succeeds as new work.
	again, _ := st.Start("parse more OMN1", "local.sensor-log", nil)
	dup, err := st.Run(context.Background(), again.ID, FixtureGenerator{}, 2)
	if dup.State != StFailed {
		t.Fatalf("redeveloped an existing capability: %s %v", dup.State, err)
	}
}

func TestFixtureEvolutionFailClosed(t *testing.T) {
	if sandbox.Select().Available() {
		t.Skip("sandbox is available; success path is TestFixtureEvolutionSeparatedTests")
	}
	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(t.TempDir(), reg)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.Start("parse OMN1 logs", "local.sensor-log", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Run(context.Background(), rec.ID, FixtureGenerator{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.State == StAwaitingApproval || got.State == StActivated {
		t.Fatalf("missing sandbox activated a capability: %s", got.State)
	}
	if got.State != StFailed {
		t.Fatalf("state %s err %s", got.State, got.Error)
	}
	active, getErr := reg.Get("local.sensor-log", "")
	if getErr == nil && active.ActivationStatus == registry.StatusEnabled {
		t.Fatal("enabled without a sandbox")
	}
}

func TestVerifierFailureDoesNotActivate(t *testing.T) {
	if _, err := execLook("python3"); err != nil {
		t.Skip("python3")
	}
	if !sandboxAvailable() {
		t.Skip("sandbox backend required to observe a candidate failure")
	}
	reg, _ := registry.Open(t.TempDir())
	st, _ := Open(t.TempDir(), reg)
	rec, _ := st.Start("parse", "local.broken", nil)
	got, err := st.Run(context.Background(), rec.ID, brokenGen{}, 1)
	if got.State == StActivated || got.State == StAwaitingApproval {
		t.Fatalf("state %s err %v", got.State, err)
	}
	if got.State != StFailed {
		t.Fatalf("state %s", got.State)
	}
	if _, err := st.Approve(got.ID); err == nil {
		t.Fatal("approved a failed evolution")
	}
}

type brokenGen struct{}

func (brokenGen) Generate(ctx context.Context, goal, dest string) (int, error) {
	n, err := (FixtureGenerator{}).Generate(ctx, goal, dest)
	if err != nil {
		return n, err
	}
	// Break the parser so independent expected values cannot pass.
	return n, osWrite(dest+"/src/main.py", "import sys\nsys.exit(1)\n")
}

func sandboxAvailable() bool { return sandbox.Select().Available() }

func lookPython() (string, error) { return execLook("python3") }

func execLook(name string) (string, error) { return exec.LookPath(name) }

func osWrite(path, body string) error { return os.WriteFile(path, []byte(body), 0o600) }
