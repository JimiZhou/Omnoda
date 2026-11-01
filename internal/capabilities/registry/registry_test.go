package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mf = `{
  "apiVersion": "omnoda.dev/v1alpha1",
  "kind": "Capability",
  "metadata": {"id": "local.echo", "name": "Echo", "version": "0.1.0", "description": "echo"},
  "spec": {
    "runtime": "python",
    "entrypoint": "src/main.py",
    "platforms": ["linux/amd64", "linux/arm64", "darwin/arm64", "darwin/amd64", "windows/amd64", "windows/arm64"],
    "inputs": {"type": "object", "properties": {"input_file": {"type": "string"}}, "required": ["input_file"]},
    "permissions": {"filesystem": {"read": ["workspace"], "write": ["output"]}, "network": false},
    "limits": {"timeout_seconds": 30, "memory_mb": 128}
  }
}`

func TestInstallEnableRollback(t *testing.T) {
	root := t.TempDir()
	reg, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	src := writePkg(t, "0.1.0")
	rec, err := reg.Install(src, "local", "test", VerifyPassed)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ActivationStatus != StatusInstalled {
		t.Fatal(rec.ActivationStatus)
	}
	got, err := reg.SetStatus(rec.CapabilityID, rec.Version, StatusEnabled)
	if err != nil || got.ActivationStatus != StatusEnabled {
		t.Fatal(err, got.ActivationStatus)
	}
	src2 := writePkgVersion(t, "0.2.0")
	rec2, err := reg.Install(src2, "local", "test", VerifyPassed)
	if err != nil {
		t.Fatal(err)
	}
	active, err := reg.Get(rec.CapabilityID, "")
	if err != nil || active.Version != "0.1.0" || active.ActivationStatus != StatusEnabled {
		t.Fatalf("silent replace: %+v %v", active, err)
	}
	if _, err := reg.SetStatus(rec2.CapabilityID, rec2.Version, StatusEnabled); err != nil {
		t.Fatal(err)
	}
	rolled, err := reg.Rollback(rec.CapabilityID, "0.1.0")
	if err != nil || rolled.Version != "0.1.0" || rolled.ActivationStatus != StatusEnabled {
		t.Fatal(err, rolled.Version, rolled.ActivationStatus)
	}
	cur, _ := reg.Get(rec.CapabilityID, "0.2.0")
	if cur.ActivationStatus == StatusEnabled {
		t.Fatal("old version still enabled alongside new")
	}
}

func TestUnverifiedCannotEnable(t *testing.T) {
	reg, _ := Open(t.TempDir())
	rec, err := reg.Install(writePkg(t, "0.1.0"), "generated", "evolver", VerifyNone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetStatus(rec.CapabilityID, rec.Version, StatusEnabled); err == nil {
		t.Fatal("enabled unverified")
	}
}

func writePkg(t *testing.T, version string) string {
	t.Helper()
	return writePkgVersion(t, version)
}

func writePkgVersion(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	body := mf
	if version != "0.1.0" {
		body = strings.Replace(body, `"version": "0.1.0"`, `"version": "`+version+`"`, 1)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "capability.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.py"), []byte("print('ok')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}
