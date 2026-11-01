package policy

import (
	"path/filepath"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/protocol"
)

func TestPathEscape(t *testing.T) {
	root := t.TempDir()
	p := Policy{Workspaces: []string{root}}
	if _, err := p.ResolveWorkspace(filepath.Join(root, "..", "etc")); err == nil {
		t.Fatal("escape accepted")
	}
	if _, err := p.ResolveWorkspace(filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCommand(protocol.LevelReadOnly, []string{"cat", filepath.Join(root, ".ssh", "id_rsa")}); err == nil {
		t.Fatal("secret path accepted")
	}
	if err := p.CheckCommand(protocol.LevelReadOnly, []string{"uname", "-a"}); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCommand(protocol.LevelPrivileged, []string{"rm", "-rf", "/"}); err != ErrPrivileged {
		t.Fatal(err)
	}
}

func TestGitWriteBlockedOnReadOnly(t *testing.T) {
	p := Policy{}
	if err := p.CheckCommand(protocol.LevelReadOnly, []string{"git", "push"}); err == nil {
		t.Fatal("git push allowed")
	}
	if err := p.CheckCommand(protocol.LevelReadOnly, []string{"git", "status"}); err != nil {
		t.Fatal(err)
	}
}
