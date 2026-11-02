package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
)

// v2 keeps every v1 row. New tables hold conversations, messages, and runs.
// Existing tasks gain owner and conversation columns without a rebuild.

func (d *DB) migrateV2() error {
	var version int
	_ = d.sql.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version)
	if version >= 3 {
		return nil
	}
	return d.applyMigration(version)
}

func v2Stmts() []string {
	return []string{
		`ALTER TABLE tasks ADD COLUMN conversation_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN owner_id TEXT`,
		`ALTER TABLE tasks ADD COLUMN goal TEXT`,
		`ALTER TABLE tasks ADD COLUMN side_effect INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			title TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			task_id TEXT,
			created_at TEXT NOT NULL,
			UNIQUE(conversation_id, seq)
		)`,
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			node_id TEXT,
			adapter TEXT,
			external_session TEXT,
			status TEXT NOT NULL,
			resumed INTEGER NOT NULL DEFAULT 0,
			started_at TEXT NOT NULL,
			ended_at TEXT,
			result TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS seed_local (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			node_id TEXT NOT NULL,
			workspace TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (2)`,
	}
}

func v3Stmts() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS event_seq (
			task_id TEXT PRIMARY KEY,
			next_seq INTEGER NOT NULL
		)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (3)`,
	}
}

func (d *DB) applyMigration(version int) error {
	var stmts []string
	if version < 2 {
		stmts = append(stmts, v2Stmts()...)
	}
	if version < 3 {
		stmts = append(stmts, v3Stmts()...)
	}
	for _, s := range stmts {
		if _, err := d.sql.Exec(s); err != nil {
			// Adding a column twice is not an error on a retried migrate.
			if !isDupColumn(err) {
				return err
			}
		}
	}
	if version < 3 {
		if _, err := d.sql.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS tasks_owner_idem ON tasks(owner_id, idempotency_key) WHERE owner_id IS NOT NULL AND idempotency_key IS NOT NULL AND idempotency_key <> ''`); err != nil {
			return err
		}
		if _, err := d.sql.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS task_events_task_seq ON task_events(task_id, seq)`); err != nil {
			return err
		}
	}
	return nil
}

// ErrIdempotencyConflict means the key exists for this owner with a different body.
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")

// ErrTerminal is returned when a write would replace a finished task status.
var ErrTerminal = errors.New("task is already in a terminal status")

func isDupColumn(err error) bool {
	return err != nil && (contains(err.Error(), "duplicate column") || contains(err.Error(), "already exists"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type Conversation struct {
	ID        string    `json:"id"`
	OwnerID   string    `json:"owner_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Seq            int       `json:"seq"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	TaskID         string    `json:"task_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type RunRow struct {
	ID              string `json:"id"`
	TaskID          string `json:"task_id"`
	NodeID          string `json:"node_id,omitempty"`
	Adapter         string `json:"adapter,omitempty"`
	ExternalSession string `json:"external_session,omitempty"`
	Status          string `json:"status"`
	Resumed         bool   `json:"resumed"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at,omitempty"`
	Result          string `json:"result,omitempty"`
}

func (d *DB) CreateConversation(owner, title string) (Conversation, error) {
	c := Conversation{ID: NewID("conv"), OwnerID: owner, Title: clip(title, 80)}
	if c.Title == "" {
		c.Title = "New conversation"
	}
	_, err := d.sql.Exec(`INSERT INTO conversations(id,owner_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`,
		c.ID, c.OwnerID, c.Title, now(), now())
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	return c, err
}

func (d *DB) GetConversation(id string) (Conversation, error) {
	var c Conversation
	var created, updated string
	err := d.sql.QueryRow(`SELECT id,owner_id,title,created_at,updated_at FROM conversations WHERE id=?`, id).
		Scan(&c.ID, &c.OwnerID, &c.Title, &created, &updated)
	if err != nil {
		return c, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return c, nil
}

func (d *DB) ListConversations(owner string, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,owner_id,title,created_at,updated_at FROM conversations WHERE owner_id=? ORDER BY updated_at DESC LIMIT ?`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		var created, updated string
		if err := rows.Scan(&c.ID, &c.OwnerID, &c.Title, &created, &updated); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) AddMessage(conversationID, role, content, taskID string) (Message, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return Message{}, err
	}
	defer tx.Rollback()
	var seq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE conversation_id=?`, conversationID).Scan(&seq); err != nil {
		return Message{}, err
	}
	m := Message{ID: NewID("msg"), ConversationID: conversationID, Seq: seq, Role: role, Content: content, TaskID: taskID}
	if _, err := tx.Exec(`INSERT INTO messages(id,conversation_id,seq,role,content,task_id,created_at) VALUES(?,?,?,?,?,?,?)`,
		m.ID, m.ConversationID, m.Seq, m.Role, m.Content, nullIfEmpty(m.TaskID), now()); err != nil {
		return Message{}, err
	}
	if _, err := tx.Exec(`UPDATE conversations SET updated_at=? WHERE id=?`, now(), conversationID); err != nil {
		return Message{}, err
	}
	m.CreatedAt = time.Now().UTC()
	return m, tx.Commit()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (d *DB) Messages(conversationID string) ([]Message, error) {
	rows, err := d.sql.Query(`SELECT id,conversation_id,seq,role,content,COALESCE(task_id,''),created_at FROM messages WHERE conversation_id=? ORDER BY seq`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var created string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.Role, &m.Content, &m.TaskID, &created); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) InsertOwnedTask(spec protocol.TaskSpec, owner, conversation, goal string, sideEffect bool) error {
	_, _, err := d.CreateMessageTask(conversation, owner, goal, spec, sideEffect)
	return err
}

// CreateMessageTask writes the user message and the queued task in one transaction.
// A repeated key for the same owner and the same body returns the original task.
// A repeated key with a different body is rejected. Another owner's key is invisible.
func (d *DB) CreateMessageTask(conversation, owner, content string, spec protocol.TaskSpec, sideEffect bool) (Message, protocol.TaskSpec, error) {
	msg, spec, _, err := d.createMessageTask(conversation, owner, content, spec, sideEffect)
	return msg, spec, err
}

// CreateMessageTaskReplay is CreateMessageTask plus whether the row already existed.
func (d *DB) CreateMessageTaskReplay(conversation, owner, content string, spec protocol.TaskSpec, sideEffect bool) (Message, protocol.TaskSpec, bool, error) {
	return d.createMessageTask(conversation, owner, content, spec, sideEffect)
}

func (d *DB) createMessageTask(conversation, owner, content string, spec protocol.TaskSpec, sideEffect bool) (Message, protocol.TaskSpec, bool, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return Message{}, spec, false, err
	}
	defer tx.Rollback()
	if spec.Idempotency != "" && owner != "" {
		var existingID, existingSpec string
		err := tx.QueryRow(`SELECT id, spec_json FROM tasks WHERE owner_id=? AND idempotency_key=?`, owner, spec.Idempotency).Scan(&existingID, &existingSpec)
		if err == nil {
			var prev protocol.TaskSpec
			_ = json.Unmarshal([]byte(existingSpec), &prev)
			if !sameRequest(prev, spec) {
				return Message{}, spec, false, ErrIdempotencyConflict
			}
			spec.TaskID = existingID
			var msg Message
			var created string
			qerr := tx.QueryRow(`SELECT id,conversation_id,seq,role,content,COALESCE(task_id,''),created_at FROM messages WHERE task_id=? AND role='user' ORDER BY seq LIMIT 1`, existingID).
				Scan(&msg.ID, &msg.ConversationID, &msg.Seq, &msg.Role, &msg.Content, &msg.TaskID, &created)
			if qerr == nil {
				msg.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			}
			return msg, spec, true, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return Message{}, spec, false, err
		}
	}
	var seq int
	if conversation != "" {
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM messages WHERE conversation_id=?`, conversation).Scan(&seq); err != nil {
			return Message{}, spec, false, err
		}
	}
	if spec.TaskID == "" {
		spec.TaskID = NewID("task")
	}
	if spec.Idempotency == "" {
		spec.Idempotency = NewID("idem")
	}
	b, _ := json.Marshal(spec)
	flag := 0
	if sideEffect {
		flag = 1
	}
	if _, err := tx.Exec(`INSERT INTO tasks(id,node_id,workflow_id,step_id,kind,spec_json,status,idempotency_key,created_at,updated_at,conversation_id,owner_id,goal,side_effect)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		spec.TaskID, "", spec.WorkflowID, spec.StepID, spec.Kind, string(b), protocol.TaskQueued, spec.Idempotency, now(), now(),
		nullIfEmpty(conversation), nullIfEmpty(owner), content, flag); err != nil {
		return Message{}, spec, false, err
	}
	msg := Message{}
	if conversation != "" {
		msg = Message{ID: NewID("msg"), ConversationID: conversation, Seq: seq, Role: "user", Content: content, TaskID: spec.TaskID}
		if _, err := tx.Exec(`INSERT INTO messages(id,conversation_id,seq,role,content,task_id,created_at) VALUES(?,?,?,?,?,?,?)`,
			msg.ID, msg.ConversationID, msg.Seq, msg.Role, msg.Content, spec.TaskID, now()); err != nil {
			return Message{}, spec, false, err
		}
		if _, err := tx.Exec(`UPDATE conversations SET updated_at=? WHERE id=?`, now(), conversation); err != nil {
			return Message{}, spec, false, err
		}
		msg.CreatedAt = time.Now().UTC()
	}
	return msg, spec, false, tx.Commit()
}

func sameRequest(prev, next protocol.TaskSpec) bool {
	return prev.Prompt == next.Prompt && prev.Kind == next.Kind && prev.Command == next.Command && prev.Workspace == next.Workspace && prev.HarnessID == next.HarnessID
}

func (d *DB) StartRun(taskID, nodeID, adapter string) (RunRow, error) {
	r := RunRow{ID: NewID("run"), TaskID: taskID, NodeID: nodeID, Adapter: adapter, Status: string(protocol.TaskRunning), StartedAt: now()}
	_, err := d.sql.Exec(`INSERT INTO runs(id,task_id,node_id,adapter,status,started_at) VALUES(?,?,?,?,?,?)`,
		r.ID, r.TaskID, nullIfEmpty(r.NodeID), nullIfEmpty(r.Adapter), r.Status, r.StartedAt)
	return r, err
}

func (d *DB) FinishRun(id, status, result, external string, resumed bool) error {
	flag := 0
	if resumed {
		flag = 1
	}
	_, err := d.sql.Exec(`UPDATE runs SET status=?, result=?, external_session=?, resumed=?, ended_at=? WHERE id=?`,
		status, clip(result, 20_000), external, flag, now(), id)
	return err
}

func (d *DB) Runs(taskID string) ([]RunRow, error) {
	rows, err := d.sql.Query(`SELECT id,task_id,COALESCE(node_id,''),COALESCE(adapter,''),COALESCE(external_session,''),status,resumed,started_at,COALESCE(ended_at,''),COALESCE(result,'') FROM runs WHERE task_id=? ORDER BY started_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRow
	for rows.Next() {
		var r RunRow
		var resumed int
		if err := rows.Scan(&r.ID, &r.TaskID, &r.NodeID, &r.Adapter, &r.ExternalSession, &r.Status, &resumed, &r.StartedAt, &r.EndedAt, &r.Result); err != nil {
			return nil, err
		}
		r.Resumed = resumed == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) EventsAfter(taskID string, after int64) ([]protocol.Event, error) {
	rows, err := d.sql.Query(`SELECT event_json FROM task_events WHERE task_id=? AND seq>? ORDER BY seq`, taskID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Event
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		var ev protocol.Event
		if json.Unmarshal([]byte(s), &ev) == nil {
			out = append(out, ev)
		}
	}
	return out, rows.Err()
}

// NextEventSeq allocates the next sequence inside a write transaction.
// Concurrent callers cannot observe the same value.
func (d *DB) NextEventSeq(taskID string) (int64, error) {
	tx, err := d.sql.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int64
	err = tx.QueryRow(`SELECT next_seq FROM event_seq WHERE task_id=?`, taskID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		var max int64
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM task_events WHERE task_id=?`, taskID).Scan(&max); err != nil {
			return 0, err
		}
		n = max + 1
		if _, err := tx.Exec(`INSERT INTO event_seq(task_id, next_seq) VALUES(?,?)`, taskID, n+1); err != nil {
			return 0, err
		}
		return n, tx.Commit()
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE event_seq SET next_seq=? WHERE task_id=?`, n+1, taskID); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

func (d *DB) AppendEventOnce(ev protocol.Event) error {
	b, _ := json.Marshal(ev)
	_, err := d.sql.Exec(`INSERT INTO task_events(task_id,seq,event_json,created_at) VALUES(?,?,?,?)`,
		ev.TaskID, ev.Sequence, string(b), now())
	if err != nil && isUnique(err) {
		return nil
	}
	return err
}

func isUnique(err error) bool {
	return err != nil && (contains(err.Error(), "UNIQUE") || contains(err.Error(), "unique"))
}

func (d *DB) QueuedTasks() ([]TaskRow, error) {
	rows, err := d.sql.Query(`SELECT id,COALESCE(node_id,''),COALESCE(workflow_id,''),COALESCE(step_id,''),COALESCE(kind,''),spec_json,status,COALESCE(exit_code,0),COALESCE(summary,''),COALESCE(output,''),COALESCE(error,''),COALESCE(idempotency_key,'') FROM tasks WHERE status=? ORDER BY created_at`, protocol.TaskQueued)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func scanTasks(rows *sql.Rows) ([]TaskRow, error) {
	var out []TaskRow
	for rows.Next() {
		var t TaskRow
		if err := rows.Scan(&t.ID, &t.NodeID, &t.WorkflowID, &t.StepID, &t.Kind, &t.SpecJSON, &t.Status, &t.ExitCode, &t.Summary, &t.Output, &t.Error, &t.IdemKey); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// OwnsTask reports whether userID may read or cancel the task.
// A row with an empty owner is legacy data. Only an empty caller, which is
// the in-process supervisor, may touch it. A logged-in user may not.
func (d *DB) OwnsTask(id, userID string) (bool, error) {
	_, owner, _, _, err := d.TaskMeta(id)
	if err != nil {
		return false, err
	}
	if owner == "" {
		return userID == "", nil
	}
	return owner == userID, nil
}

func (d *DB) TaskMeta(id string) (conversation, owner, goal string, sideEffect bool, err error) {
	var flag int
	err = d.sql.QueryRow(`SELECT COALESCE(conversation_id,''),COALESCE(owner_id,''),COALESCE(goal,''),COALESCE(side_effect,0) FROM tasks WHERE id=?`, id).
		Scan(&conversation, &owner, &goal, &flag)
	return conversation, owner, goal, flag == 1, err
}

func (d *DB) SetSeedLocal(nodeID, workspace string) error {
	_, err := d.sql.Exec(`INSERT INTO seed_local(id,node_id,workspace) VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET node_id=excluded.node_id, workspace=excluded.workspace`, nodeID, workspace)
	return err
}

func (d *DB) SeedLocal() (nodeID, workspace string, err error) {
	err = d.sql.QueryRow(`SELECT node_id, workspace FROM seed_local WHERE id=1`).Scan(&nodeID, &workspace)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return nodeID, workspace, err
}

func (d *DB) ApprovalActor(id, actor string) error {
	_, err := d.sql.Exec(`UPDATE approvals SET args_json=COALESCE(args_json,'') WHERE id=?`, id)
	if err != nil {
		return err
	}
	d.Audit(actor, "", "", "", "approval.actor", "", "recorded", id)
	return nil
}
