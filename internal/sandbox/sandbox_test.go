package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUnavailableIsNotASandbox(t *testing.T) {
	b := Select()
	if b.Name() == "none" {
		res := b.Run(context.Background(), Request{Argv: []string{"echo", "hi"}})
		if res.Err == nil || res.Backend == "path-check" {
			t.Fatal("plain subprocess must not count as a sandbox")
		}
	}
}

func TestBwrapDeniesHostFile(t *testing.T) {
	b := Select()
	if b.Name() != "bwrap" {
		t.Skip("bwrap not installed")
	}
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("hidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	res := b.Run(context.Background(), Request{
		Argv:    []string{"python3", "-c", "print(open('" + secret + "').read())"},
		WorkDir: work,
		Network: false,
	})
	if res.ExitCode == 0 && res.Output == "hidden\n" {
		t.Fatal("sandbox read a host file")
	}
}

func TestDeniedPath(t *testing.T) {
	if !DeniedPath("../etc/passwd") || !DeniedPath("/etc/passwd") {
		t.Fatal("path")
	}
	if DeniedPath("logs/a.txt") {
		t.Fatal("relative")
	}
}
