package supervisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

func TestDisconnectDoesNotCancelAndRetryDoesNotReplay(t *testing.T) {
	db := openDB(t)
	var runs atomic.Int32
	sup := New(context.Background(), db, func(ctx context.Context, task storage.TaskRow, spec protocol.TaskSpec) protocol.TaskResult {
		runs.Add(1)
		select {
		case <-ctx.Done():
			return protocol.TaskResult{TaskID: task.ID, Status: protocol.TaskCancelled, Error: "cancelled"}
		case <-time.After(400 * time.Millisecond):
			return protocol.TaskResult{TaskID: task.ID, Status: protocol.TaskSucceeded, Summary: "done", ExitCode: 0}
		}
	})
	spec := protocol.TaskSpec{TaskID: storage.NewID("task"), Kind: "builtin", Idempotency: "same-key", TimeoutSec: 30}
	if err := db.InsertOwnedTask(spec, "usr", "conv", "wait", false); err != nil {
		t.Fatal(err)
	}
	row, got, err := db.GetTask(spec.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	sup.Start(row, got)
	// A second start of the same task must not launch another run.
	sup.Start(row, got)
	time.Sleep(50 * time.Millisecond)
	if !sup.Running(spec.TaskID) {
		t.Fatal("task stopped when the caller returned")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		row, _, _ = db.GetTask(spec.TaskID)
		if row.Status == protocol.TaskSucceeded {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if row.Status != protocol.TaskSucceeded {
		t.Fatalf("status %s", row.Status)
	}
	if runs.Load() != 1 {
		t.Fatalf("runs %d", runs.Load())
	}
	id, st, ok := db.TaskByIdem("same-key")
	if !ok || id != spec.TaskID || st != protocol.TaskSucceeded {
		t.Fatalf("idem %s %s %v", id, st, ok)
	}
}

func TestExplicitCancelStopsTheRun(t *testing.T) {
	db := openDB(t)
	sup := New(context.Background(), db, func(ctx context.Context, task storage.TaskRow, spec protocol.TaskSpec) protocol.TaskResult {
		<-ctx.Done()
		return protocol.TaskResult{TaskID: task.ID, Status: protocol.TaskCancelled, Error: "cancelled"}
	})
	spec := protocol.TaskSpec{TaskID: storage.NewID("task"), Kind: "builtin", Idempotency: "cancel-me"}
	_ = db.InsertOwnedTask(spec, "usr", "", "stop", false)
	row, got, _ := db.GetTask(spec.TaskID)
	sup.Start(row, got)
	time.Sleep(30 * time.Millisecond)
	if !sup.Cancel(spec.TaskID) {
		t.Fatal("not running")
	}
	deadline := time.Now().Add(2 * time.Second)
	var status protocol.TaskStatus
	for time.Now().Before(deadline) {
		row, _, _ = db.GetTask(spec.TaskID)
		status = row.Status
		if status == protocol.TaskCancelled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status != protocol.TaskCancelled {
		t.Fatalf("status %s", status)
	}
}

func TestRestartMarksSideEffectUncertain(t *testing.T) {
	db := openDB(t)
	safe := protocol.TaskSpec{TaskID: "task_safe", Kind: "builtin", Idempotency: "safe"}
	hot := protocol.TaskSpec{TaskID: "task_hot", Kind: "command", Idempotency: "hot"}
	if err := db.InsertOwnedTask(safe, "u", "", "info", false); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertOwnedTask(hot, "u", "", "write", true); err != nil {
		t.Fatal(err)
	}
	_ = db.SetTaskStatus(hot.TaskID, protocol.TaskRunning, 0, "", "", "")
	if _, err := db.RecoverInflight(); err != nil {
		t.Fatal(err)
	}
	safeRow, _, _ := db.GetTask(safe.TaskID)
	hotRow, _, _ := db.GetTask(hot.TaskID)
	if safeRow.Status != protocol.TaskQueued {
		t.Fatalf("safe %s", safeRow.Status)
	}
	if hotRow.Status != protocol.TaskUncertain {
		t.Fatalf("hot %s", hotRow.Status)
	}
}

func openDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
