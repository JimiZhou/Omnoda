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

func TestCancelBeatsALateSuccess(t *testing.T) {
	db := openDB(t)
	started := make(chan struct{})
	release := make(chan struct{})
	sup := New(context.Background(), db, func(ctx context.Context, task storage.TaskRow, spec protocol.TaskSpec) protocol.TaskResult {
		close(started)
		<-release
		return protocol.TaskResult{TaskID: task.ID, Status: protocol.TaskSucceeded, Summary: "should-not-win", ExitCode: 0}
	})
	spec := protocol.TaskSpec{TaskID: storage.NewID("task"), Kind: "builtin", Idempotency: "late"}
	if err := db.InsertOwnedTask(spec, "usr", "", "wait", false); err != nil {
		t.Fatal(err)
	}
	row, got, _ := db.GetTask(spec.TaskID)
	sup.Start(row, got)
	<-started
	if err := db.SetTaskStatus(spec.TaskID, protocol.TaskCancelled, 130, "cancelled by user", "", ""); err != nil {
		t.Fatal(err)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for sup.Running(spec.TaskID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	row, _, _ = db.GetTask(spec.TaskID)
	if row.Status != protocol.TaskCancelled {
		t.Fatalf("status %s summary %s", row.Status, row.Summary)
	}
	if row.Summary == "should-not-win" {
		t.Fatal("late success overwrote the cancel summary")
	}
}

func TestRunnerPanicMarksFailed(t *testing.T) {
	db := openDB(t)
	sup := New(context.Background(), db, func(ctx context.Context, task storage.TaskRow, spec protocol.TaskSpec) protocol.TaskResult {
		panic("boom")
	})
	spec := protocol.TaskSpec{TaskID: storage.NewID("task"), Kind: "builtin", Idempotency: "panic"}
	_ = db.InsertOwnedTask(spec, "usr", "", "x", false)
	row, got, _ := db.GetTask(spec.TaskID)
	sup.Start(row, got)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		row, _, _ = db.GetTask(spec.TaskID)
		if row.Status == protocol.TaskFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if row.Status != protocol.TaskFailed {
		t.Fatalf("status %s", row.Status)
	}
}

func TestRepeatedCancelStaysCancelled(t *testing.T) {
	db := openDB(t)
	spec := protocol.TaskSpec{TaskID: "task_repeat", Kind: "builtin", Idempotency: "rep"}
	_ = db.InsertOwnedTask(spec, "usr", "", "x", false)
	if err := db.SetTaskStatus(spec.TaskID, protocol.TaskCancelled, 130, "cancelled by user", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTaskStatus(spec.TaskID, protocol.TaskCancelled, 130, "cancelled by user", "", ""); err != nil {
		t.Fatal(err)
	}
	row, _, _ := db.GetTask(spec.TaskID)
	if row.Status != protocol.TaskCancelled {
		t.Fatal(row.Status)
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
	live := protocol.TaskSpec{TaskID: "task_live", Kind: "builtin", Idempotency: "live"}
	if err := db.InsertOwnedTask(live, "u", "", "info", false); err != nil {
		t.Fatal(err)
	}
	run, err := db.StartRun(live.TaskID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().Exec(`UPDATE tasks SET status='running' WHERE id=?`, live.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecoverInflight(); err != nil {
		t.Fatal(err)
	}
	liveRow, _, _ := db.GetTask(live.TaskID)
	if liveRow.Status != protocol.TaskUncertain {
		t.Fatalf("restarted running task %s", liveRow.Status)
	}
	runs, _ := db.Runs(live.TaskID)
	if len(runs) != 1 || runs[0].ID != run.ID || runs[0].Status != string(protocol.TaskUncertain) {
		t.Fatalf("orphan run %+v", runs)
	}
	safeRow, _, _ = db.GetTask(safe.TaskID)
	if safeRow.Status != protocol.TaskQueued {
		t.Fatalf("queued read-only task changed to %s", safeRow.Status)
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
