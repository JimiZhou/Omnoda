package scheduler

import (
	"strings"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

func TestMatchHarnessAndExplain(t *testing.T) {
	nodes := []Candidate{
		{Node: storage.Node{ID: "a", DisplayName: "arm", OS: "linux", Arch: "arm64", Status: protocol.NodeOnline, Labels: map[string]string{"role": "monitor"}, Workspaces: []string{"/w"}}},
		{Node: storage.Node{ID: "b", DisplayName: "Mac-mini", OS: "darwin", Arch: "arm64", Status: protocol.NodeOnline, Labels: map[string]string{"role": "developer"},
			Harnesses: []protocol.HarnessInfo{{ID: "claude-code", Present: true}}, Workspaces: []string{"/proj"}}},
		{Node: storage.Node{ID: "c", DisplayName: "win", OS: "windows", Arch: "amd64", Status: protocol.NodeOffline, Labels: map[string]string{"role": "tester"}}},
	}
	r, err := Place(protocol.NodeSelector{Role: "developer", Harness: "claude-code"}, nodes)
	if err != nil {
		t.Fatal(err)
	}
	if r.NodeID != "b" || !strings.Contains(r.Reason, "Mac-mini") || !strings.Contains(r.Reason, "claude-code") {
		t.Fatal(r)
	}
	if _, err := Place(protocol.NodeSelector{Role: "tester"}, nodes); err == nil {
		t.Fatal("offline tester selected")
	}
}
