package packaging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
)

func TestExportImportRoundTrip(t *testing.T) {
	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	body := []byte(`{
  "apiVersion": "omnoda.dev/v1alpha1",
  "kind": "Capability",
  "metadata": {"id": "local.echo", "name": "Echo", "version": "0.1.0", "description": "echo"},
  "spec": {
    "runtime": "python",
    "entrypoint": "src/main.py",
    "platforms": ["linux/amd64","linux/arm64","darwin/amd64","darwin/arm64","windows/amd64","windows/arm64"],
    "inputs": {"type":"object","properties":{"input_file":{"type":"string"}},"required":["input_file"]},
    "permissions": {"filesystem":{"read":["workspace"],"write":["output"]},"network": false},
    "limits": {"timeout_seconds": 15, "memory_mb": 64}
  }
}`)
	if err := os.MkdirAll(filepath.Join(src, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(src, "capability.json"), body, 0o600)
	_ = os.WriteFile(filepath.Join(src, "src", "main.py"), []byte("print('x')\n"), 0o600)
	rec, err := reg.Install(src, "local", "test", registry.VerifyPassed)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "cap.tar.gz")
	if err := Export(rec, bundle); err != nil {
		t.Fatal(err)
	}
	other, _ := registry.Open(t.TempDir())
	got, err := Import(other, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentHash != rec.ContentHash || got.ActivationStatus == registry.StatusEnabled {
		t.Fatalf("import %+v", got.ActivationStatus)
	}
}

func TestExportRejectsSecret(t *testing.T) {
	reg, _ := registry.Open(t.TempDir())
	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "capability.json"), []byte(`{
  "apiVersion": "omnoda.dev/v1alpha1", "kind": "Capability",
  "metadata": {"id": "local.bad", "name": "Bad", "version": "0.1.0", "description": "no"},
  "spec": {"runtime":"python","entrypoint":"src/main.py","platforms":["linux/amd64"],
    "inputs":{"type":"object","properties":{}},
    "permissions":{"filesystem":{"read":[],"write":[]},"network":false},
    "limits":{"timeout_seconds":10,"memory_mb":32}}}`), 0o600)
	_ = os.MkdirAll(filepath.Join(src, "src"), 0o700)
	_ = os.WriteFile(filepath.Join(src, "src", "main.py"), []byte("key = 'sk-live-secret'\n"), 0o600)
	rec, err := reg.Install(src, "local", "test", registry.VerifyNone)
	if err != nil {
		t.Fatal(err)
	}
	if err := Export(rec, filepath.Join(t.TempDir(), "x.tar.gz")); err == nil {
		t.Fatal("exported a secret-looking file")
	}
}
