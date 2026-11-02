package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestBwrapDeniesWriteAndNetwork(t *testing.T) {
	b := Select()
	if b.Name() != "bwrap" {
		t.Skip("bwrap not installed")
	}
	work := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escaped.txt")
	res := b.Run(context.Background(), Request{
		Argv: []string{"python3", "-c",
			"open(r'" + outside + "','w').write('x'); import urllib.request; urllib.request.urlopen('http://127.0.0.1:9', timeout=1)"},
		WorkDir: work,
		Network: false,
		Timeout: 5 * time.Second,
	})
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("sandbox wrote outside the work dir")
	}
	if res.ExitCode == 0 {
		t.Fatal("network call succeeded")
	}
}

func TestNonLinuxHasNoBackend(t *testing.T) {
	if runtimeGOOS() == "linux" {
		t.Skip("linux may have bwrap")
	}
	if Select().Available() {
		t.Fatal("non-linux must not offer a generated-code sandbox")
	}
}

func runtimeGOOS() string { return runtimeOS }

func TestDeniedPath(t *testing.T) {
	if !DeniedPath("../etc/passwd") || !DeniedPath("/etc/passwd") {
		t.Fatal("path")
	}
	if DeniedPath("logs/a.txt") {
		t.Fatal("relative")
	}
}
