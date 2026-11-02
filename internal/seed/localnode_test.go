package seed

import (
	"testing"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

func TestBootstrapRegistersLocalNode(t *testing.T) {
	rt, err := Bootstrap(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rt.DB.Close() })
	nodes, err := rt.DB.ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Status != protocol.NodeOnline {
		t.Fatalf("%+v", nodes)
	}
	if len(nodes[0].Workspaces) != 0 {
		t.Fatal("workspace must be chosen by the operator")
	}
	again, err := EnsureLocalNode(rt.DB, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != nodes[0].ID {
		t.Fatal("local node id changed")
	}
}

func TestOldDatabaseKeepsTasks(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(dir + "/omnoda.db")
	if err != nil {
		t.Fatal(err)
	}
	spec := protocol.TaskSpec{TaskID: "task_old", Kind: "command", Idempotency: "old"}
	if err := db.InsertTask(spec); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = storage.Open(dir + "/omnoda.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	row, _, err := db.GetTask("task_old")
	if err != nil || row.Kind != "command" {
		t.Fatal(err, row.Kind)
	}
	if _, err := db.CreateConversation("usr", "kept"); err != nil {
		t.Fatal(err)
	}
}
