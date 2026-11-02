package seed

import (
	"runtime"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

// EnsureLocalNode registers this process as an online execution node.
// The first task does not wait for a separate enrollment.
// The workspace is the directory the operator named. It is not the home directory.
func EnsureLocalNode(db *storage.DB, workspace string) (storage.Node, error) {
	id, saved, err := db.SeedLocal()
	if err != nil {
		return storage.Node{}, err
	}
	if saved != "" {
		workspace = saved
	}
	if id == "" {
		id = storage.NewID("node")
	}
	now := time.Now().UTC()
	n := storage.Node{
		ID: id, DisplayName: "This computer", OS: runtime.GOOS, Arch: runtime.GOARCH,
		Version: protocol.Version, Hostname: "local", Status: protocol.NodeOnline,
		PublicKey: "seed-local", Labels: map[string]string{"role": "seed"},
		Capabilities: []protocol.Capability{{Name: "omnoda.system-info", Present: true}},
		Workspaces:   []string{}, LastSeen: &now,
	}
	if workspace != "" {
		n.Workspaces = []string{workspace}
	}
	if err := db.UpsertNode(n); err != nil {
		return n, err
	}
	if err := db.SetSeedLocal(id, workspace); err != nil {
		return n, err
	}
	return n, nil
}
