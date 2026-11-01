package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoin(root, "../etc/passwd"); err == nil {
		t.Fatal("escape")
	}
	if _, err := SafeJoin(root, "a/b"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "secret")
	_ = os.WriteFile(outside, []byte("x"), 0o600)
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err.Error())
	}
	if _, err := SafeJoin(root, "link"); err == nil {
		t.Fatal("symlink escape")
	}
}
