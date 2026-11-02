package evolver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/sandbox"
)

func TestHostileGeneratedScriptsStayInsideSandbox(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("hostile sandbox proof runs on Linux with bwrap")
	}
	if sandbox.Select().Name() != "bwrap" {
		t.Skip("bwrap not installed; untrusted scripts fail closed")
	}
	root := t.TempDir()
	secretDir := filepath.Join(root, "host-secret")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "marker.txt")
	if err := os.WriteFile(secret, []byte("HOST-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "escaped.txt")
	pkg := filepath.Join(root, "pkg")
	if err := os.MkdirAll(filepath.Join(pkg, "verifier"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := `import os, pathlib, socket, urllib.request
secret = pathlib.Path(` + quote(secret) + `)
outside = pathlib.Path(` + quote(outside) + `)
try:
    data = secret.read_text()
except Exception as exc:
    data = "denied:" + type(exc).__name__
try:
    outside.write_text("escaped")
    wrote = "wrote"
except Exception as exc:
    wrote = "denied:" + type(exc).__name__
try:
    urllib.request.urlopen("http://127.0.0.1:9", timeout=1).read()
    net = "open"
except Exception as exc:
    net = "denied:" + type(exc).__name__
print(data)
print(wrote)
print(net)
`
	if err := os.WriteFile(filepath.Join(pkg, "builder_test.py"), []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, detail := runBuilder(pkg)
	if strings.Contains(detail, "HOST-SECRET") {
		t.Fatalf("builder read the host marker: %s", detail)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("builder wrote a file outside the package")
	}
	if !strings.Contains(detail, "denied") && ok {
		t.Fatalf("hostile builder was treated as a clean pass: %s", detail)
	}
	if strings.Contains(detail, "net=open") || strings.Contains(detail, "\nopen") {
		t.Fatalf("hostile builder reached the network: %s", detail)
	}

	loop := "while True:\n    pass\n"
	if err := os.WriteFile(filepath.Join(pkg, "verifier_test.py"), []byte(loop), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "verifier", "expected.json"), []byte(`{"temperature_count":1,"samples":1,"anomalies":0,"temperature_avg":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, detail = runVerifier(pkg)
	if ok {
		t.Fatal("infinite verifier passed")
	}
	if !strings.Contains(detail, "timeout") && !strings.Contains(detail, "sandbox") {
		t.Fatalf("verifier loop was not bounded: %s", detail)
	}
	_ = context.Background()
}

func TestNoBackendRefusesGeneratedScript(t *testing.T) {
	if sandbox.Select().Available() {
		t.Skip("a sandbox backend is present; fail-closed path is the other test")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "builder_test.py"), []byte("print('hi')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, detail := runBuilder(dir)
	if ok || !strings.Contains(detail, "sandbox unavailable") {
		t.Fatalf("expected fail closed, got %v %s", ok, detail)
	}
}

func quote(p string) string {
	return `"` + strings.ReplaceAll(p, `\`, `\\`) + `"`
}
