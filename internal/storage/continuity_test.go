package storage

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	_ "modernc.org/sqlite"
)

func TestMessageAndTaskAreAtomic(t *testing.T) {
	db := openMem(t)
	conv, err := db.CreateConversation("usr_a", "home")
	if err != nil {
		t.Fatal(err)
	}
	spec := protocol.TaskSpec{Kind: "builtin", Prompt: "report system info", Idempotency: "k1", Command: "omnoda.system-info", Level: protocol.LevelReadOnly}
	msg, got, err := db.CreateMessageTask(conv.ID, "usr_a", spec.Prompt, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if msg.TaskID != got.TaskID {
		t.Fatalf("message %s task %s", msg.TaskID, got.TaskID)
	}
	msgs, _ := db.Messages(conv.ID)
	if len(msgs) != 1 {
		t.Fatalf("messages %d", len(msgs))
	}
}

func TestIdempotencyConflictAndOwnerIsolation(t *testing.T) {
	db := openMem(t)
	a, _ := db.CreateConversation("usr_a", "a")
	b, _ := db.CreateConversation("usr_b", "b")
	spec := protocol.TaskSpec{Kind: "builtin", Prompt: "report system info", Idempotency: "same", Command: "omnoda.system-info"}
	_, first, err := db.CreateMessageTask(a.ID, "usr_a", spec.Prompt, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	_, again, err := db.CreateMessageTask(a.ID, "usr_a", spec.Prompt, spec, false)
	if err != nil || again.TaskID != first.TaskID {
		t.Fatalf("retry %s %v", again.TaskID, err)
	}
	other := spec
	other.Prompt = "delete the disk"
	if _, _, err := db.CreateMessageTask(a.ID, "usr_a", other.Prompt, other, true); err != ErrIdempotencyConflict {
		t.Fatalf("conflict err %v", err)
	}
	_, foreign, err := db.CreateMessageTask(b.ID, "usr_b", spec.Prompt, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if foreign.TaskID == first.TaskID {
		t.Fatal("another user received the first task")
	}
	ok, err := db.OwnsTask(first.TaskID, "usr_b")
	if err != nil || ok {
		t.Fatalf("cross owner %v %v", ok, err)
	}
	legacy := protocol.TaskSpec{TaskID: "task_old", Kind: "command", Idempotency: "legacy"}
	if err := db.InsertTask(legacy); err != nil {
		t.Fatal(err)
	}
	ok, err = db.OwnsTask(legacy.TaskID, "usr_a")
	if err != nil || ok {
		t.Fatal("legacy row was readable by a logged-in user")
	}
}

func TestConcurrentIdempotencyCreatesOneTask(t *testing.T) {
	db := openMem(t)
	conv, _ := db.CreateConversation("usr_a", "a")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spec := protocol.TaskSpec{Kind: "builtin", Prompt: "report system info", Idempotency: "race", Command: "omnoda.system-info"}
			_, got, err := db.CreateMessageTask(conv.ID, "usr_a", spec.Prompt, spec, false)
			if err != nil {
				errs <- err
				return
			}
			ids <- got.TaskID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var seen string
	for id := range ids {
		if seen == "" {
			seen = id
		} else if id != seen {
			t.Fatalf("two tasks %s %s", seen, id)
		}
	}
	msgs, _ := db.Messages(conv.ID)
	if len(msgs) != 1 {
		t.Fatalf("messages %d", len(msgs))
	}
}

func TestEventSeqIsMonotonicUnderContention(t *testing.T) {
	db := openMem(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := db.NextEventSeq("task_1")
			if err != nil {
				t.Error(err)
				return
			}
			_ = db.AppendEventOnce(protocol.Event{TaskID: "task_1", Sequence: n, EventType: protocol.EvLog})
		}()
	}
	wg.Wait()
	ev, err := db.Events("task_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 20 {
		t.Fatalf("events %d", len(ev))
	}
	for i, e := range ev {
		if e.Sequence != int64(i+1) {
			t.Fatalf("seq %d at %d", e.Sequence, i)
		}
	}
}

func TestV1DatabaseGainsOwnerColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY);
		INSERT INTO schema_migrations(version) VALUES (1);
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY, node_id TEXT, workflow_id TEXT, step_id TEXT, kind TEXT,
			spec_json TEXT NOT NULL, status TEXT NOT NULL, exit_code INTEGER, summary TEXT,
			output TEXT, error TEXT, idempotency_key TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		INSERT INTO tasks(id,spec_json,status,idempotency_key,created_at,updated_at) VALUES('task_old','{}','queued','old','t','t');
		CREATE TABLE task_events (id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL, seq INTEGER NOT NULL, event_json TEXT NOT NULL, created_at TEXT NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	row, _, err := db.GetTask("task_old")
	if err != nil || row.Status != protocol.TaskQueued {
		t.Fatalf("legacy task %+v %v", row, err)
	}
	ok, err := db.OwnsTask("task_old", "usr_new")
	if err != nil || ok {
		t.Fatal("migrated legacy task became readable")
	}
}

func TestTerminalStatusIsSticky(t *testing.T) {
	db := openMem(t)
	spec := protocol.TaskSpec{TaskID: "task_c", Kind: "builtin", Idempotency: "c", Prompt: "x"}
	if err := db.InsertOwnedTask(spec, "usr", "conv", "x", false); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTaskStatus(spec.TaskID, protocol.TaskCancelled, 130, "cancelled", "", ""); err != nil {
		t.Fatal(err)
	}
	err := db.SetTaskStatus(spec.TaskID, protocol.TaskSucceeded, 0, "late", "", "")
	if err != ErrTerminal {
		t.Fatalf("err %v", err)
	}
	row, _, _ := db.GetTask(spec.TaskID)
	if row.Status != protocol.TaskCancelled || row.Summary == "late" {
		t.Fatalf("status %s summary %s", row.Status, row.Summary)
	}
}

func openMem(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
