package evolver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
	"github.com/JimiZhou/Omnoda/internal/sandbox"
)

func TestAlwaysPassVerifierDoesNotGrantVerification(t *testing.T) {
	if !sandbox.Select().Available() {
		t.Skip("requires a real sandbox; without one the candidate stays unverified")
	}
	reg, _ := registry.Open(t.TempDir())
	st, _ := Open(t.TempDir(), reg)
	rec, _ := st.Start("parse OMN1 logs", "local.sensor-log", nil)
	got, err := st.Run(context.Background(), rec.ID, lyingGen{}, 2)
	if err != nil && got.State == "" {
		t.Fatal(err)
	}
	if got.State == StAwaitingApproval || got.State == StActivated {
		t.Fatalf("lying verifier reached %s: %s", got.State, got.Error)
	}
	if got.TestResults["builder"] != true {
		t.Fatalf("builder diagnostic missing: %#v", got.TestResults)
	}
	if got.TestResults["verifier"] {
		t.Fatal("system verifier accepted a wrong result")
	}
}

func TestNoContractStaysUnverified(t *testing.T) {
	reg, _ := registry.Open(t.TempDir())
	st, _ := Open(t.TempDir(), reg)
	rec, _ := st.Start("do something else", "local.other", nil)
	got, err := st.Run(context.Background(), rec.ID, otherGen{}, 1)
	if got.State == StAwaitingApproval || got.State == StActivated {
		t.Fatalf("uncontracted capability reached %s", got.State)
	}
	active, getErr := reg.Get("local.other", "")
	if getErr != nil {
		t.Fatal(err, getErr, got.Error)
	}
	if active.VerificationStatus != registry.VerifyUnverified || active.ActivationStatus == registry.StatusEnabled {
		t.Fatalf("status %s %s", active.VerificationStatus, active.ActivationStatus)
	}
	if _, err := reg.SetStatus(active.CapabilityID, active.Version, registry.StatusEnabled); err == nil {
		t.Fatal("activated a package with no trusted contract")
	}
}

func TestTamperBeforeActivateIsRejected(t *testing.T) {
	if !sandbox.Select().Available() {
		t.Skip("requires a real sandbox to reach awaiting approval")
	}
	reg, _ := registry.Open(t.TempDir())
	st, _ := Open(t.TempDir(), reg)
	rec, _ := st.Start("parse OMN1 logs", "local.sensor-log", nil)
	got, err := st.Run(context.Background(), rec.ID, FixtureGenerator{}, 2)
	if err != nil || got.State != StAwaitingApproval {
		t.Fatalf("state %s err %v %s", got.State, err, got.Error)
	}
	installed, err := reg.Get(got.CapabilityID, got.Version)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installed.Dir, "src", "main.py")
	if err := os.WriteFile(target, []byte("print('tampered')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Approve(got.ID); err == nil {
		t.Fatal("approved a package edited after verification")
	}
	again, err := reg.Get(got.CapabilityID, got.Version)
	if err == nil && again.ActivationStatus == registry.StatusEnabled {
		t.Fatal("tampered package is enabled")
	}
}

func TestFailedActivationKeepsPreviousVersion(t *testing.T) {
	reg, _ := registry.Open(t.TempDir())
	oldDir := writeSensor(t, "0.1.0", "print('old')\n")
	old, err := reg.Install(oldDir, "local", "test", registry.VerifyPassed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetStatus(old.CapabilityID, old.Version, registry.StatusEnabled); err != nil {
		t.Fatal(err)
	}
	nextDir := writeSensor(t, "0.2.0", "print('new')\n")
	next, err := reg.Install(nextDir, "generated", "evolver", registry.VerifyUnverified)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetStatus(next.CapabilityID, next.Version, registry.StatusEnabled); err == nil {
		t.Fatal("unverified version replaced the active one")
	}
	cur, err := reg.Get(old.CapabilityID, "")
	if err != nil {
		t.Fatal(err)
	}
	if cur.Version != "0.1.0" || cur.ActivationStatus != registry.StatusEnabled {
		t.Fatalf("previous version lost: %s %s", cur.Version, cur.ActivationStatus)
	}
}

func TestGeneratedScriptCannotReplaceSystemFixture(t *testing.T) {
	if !sandbox.Select().Available() {
		t.Skip("requires a real sandbox")
	}
	dir := t.TempDir()
	// The hostile package tries to rewrite a sibling expected.json. The system
	// verifier does not read that file, and the sandbox must not create it.
	outside := filepath.Join(t.TempDir(), "expected.json")
	script := "import pathlib\np=pathlib.Path(" + quote(outside) + ")\n"
	script += "try:\n p.write_text('{\"samples\":0}')\n print('wrote')\nexcept Exception as exc:\n print('denied:'+type(exc).__name__)\n"
	if err := os.WriteFile(filepath.Join(dir, "builder_test.py"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	_, detail := runBuilder(dir)
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("generated script replaced a fixture outside the package")
	}
	if strings.Contains(detail, "wrote") && !strings.Contains(detail, "denied") {
		t.Fatalf("fixture write looked successful: %s", detail)
	}
}

type lyingGen struct{}

func (lyingGen) Generate(ctx context.Context, goal, dest string) (int, error) {
	n, err := (FixtureGenerator{}).Generate(ctx, goal, dest)
	if err != nil {
		return n, err
	}
	// Self-check prints success. The real parser returns the wrong counts.
	if err := os.WriteFile(filepath.Join(dest, "builder_test.py"), []byte("print('builder ok')\n"), 0o600); err != nil {
		return n, err
	}
	if err := os.WriteFile(filepath.Join(dest, "verifier_test.py"), []byte("print('verifier ok')\n"), 0o600); err != nil {
		return n, err
	}
	wrong := `import argparse, json, pathlib
p = argparse.ArgumentParser()
p.add_argument("--input", required=True)
p.add_argument("--output", required=True)
a = p.parse_args()
pathlib.Path(a.output).write_text('{"samples":0,"anomalies":0,"temperature_avg":0}')
print("success")
`
	return n, os.WriteFile(filepath.Join(dest, "src", "main.py"), []byte(wrong), 0o600)
}

type otherGen struct{}

func (otherGen) Generate(_ context.Context, _, dest string) (int, error) {
	files := map[string]string{
		"capability.json": `{
  "apiVersion": "omnoda.dev/v1alpha1",
  "kind": "Capability",
  "metadata": {"id": "local.other", "name": "Other", "version": "0.1.0", "description": "no trusted contract"},
  "spec": {"runtime": "python", "entrypoint": "src/main.py", "platforms": ["linux/amd64"], "inputs": {"type": "object"}, "permissions": {"filesystem": {"read": ["workspace"], "write": ["output"]}, "network": false}, "limits": {"timeout_seconds": 30, "memory_mb": 128}}
}`,
		"src/main.py":      "print('ok')\n",
		"builder_test.py":  "print('builder ok')\n",
		"verifier_test.py": "print('verifier ok')\n",
	}
	for name, body := range files {
		path := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return 0, err
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func writeSensor(t *testing.T, version, source string) string {
	t.Helper()
	dir := t.TempDir()
	body := strings.Replace(fixtureManifest, `"version": "0.1.0"`, `"version": "`+version+`"`, 1)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "capability.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}
