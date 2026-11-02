// Package storage is the SQLite persistence layer. Task state is never
// memory-only.
package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/identity"
	"github.com/JimiZhou/Omnoda/internal/protocol"
	_ "modernc.org/sqlite"
)

type DB struct {
	sql *sql.DB
}

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &DB{sql: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) SQL() *sql.DB { return d.sql }

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			csrf TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		)`,
		`CREATE TABLE IF NOT EXISTS nodes (
			id TEXT PRIMARY KEY,
			display_name TEXT NOT NULL,
			os TEXT, arch TEXT, version TEXT, hostname TEXT,
			status TEXT NOT NULL,
			public_key TEXT NOT NULL,
			labels_json TEXT,
			capabilities_json TEXT,
			harnesses_json TEXT,
			workspaces_json TEXT,
			last_seen TEXT,
			revoked_at TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS enrollment_codes (
			id TEXT PRIMARY KEY,
			code_hash TEXT UNIQUE NOT NULL,
			display_name TEXT,
			os_hint TEXT,
			expires_at TEXT NOT NULL,
			consumed_at TEXT,
			revoked_at TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			node_id TEXT,
			workflow_id TEXT,
			step_id TEXT,
			kind TEXT,
			spec_json TEXT NOT NULL,
			status TEXT NOT NULL,
			exit_code INTEGER,
			summary TEXT,
			output TEXT,
			error TEXT,
			idempotency_key TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS task_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			event_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS workflows (
			id TEXT PRIMARY KEY,
			goal TEXT NOT NULL,
			planner TEXT NOT NULL,
			status TEXT NOT NULL,
			plan_json TEXT NOT NULL,
			report_json TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS workflow_steps (
			workflow_id TEXT NOT NULL,
			step_id TEXT NOT NULL,
			status TEXT NOT NULL,
			node_id TEXT,
			placement_reason TEXT,
			task_id TEXT,
			input_json TEXT,
			output_json TEXT,
			attempt INTEGER NOT NULL DEFAULT 0,
			error TEXT,
			PRIMARY KEY (workflow_id, step_id)
		)`,
		`CREATE TABLE IF NOT EXISTS approvals (
			id TEXT PRIMARY KEY,
			workflow_id TEXT,
			step_id TEXT,
			task_id TEXT,
			node_id TEXT,
			action TEXT NOT NULL,
			args_json TEXT,
			status TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			decided_at TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			task_id TEXT,
			workflow_id TEXT,
			name TEXT,
			sha256 TEXT,
			size INTEGER,
			kind TEXT,
			path TEXT,
			expires_at TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			who TEXT,
			node_id TEXT,
			task_id TEXT,
			workflow_id TEXT,
			action TEXT,
			permission TEXT,
			result TEXT,
			detail TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS preferences (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS model_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			base_url TEXT,
			model TEXT,
			api_key_enc TEXT,
			max_steps INTEGER,
			timeout_sec INTEGER,
			token_budget INTEGER
		)`,
	}
	for _, s := range stmts {
		if _, err := d.sql.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	_, _ = d.sql.Exec(`INSERT OR IGNORE INTO schema_migrations(version) VALUES (1)`)
	return d.migrateV2()
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func NewID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

type User struct {
	ID       string
	Username string
}

func (d *DB) CreateUser(username, hash string) (User, error) {
	u := User{ID: NewID("usr"), Username: username}
	_, err := d.sql.Exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES(?,?,?,?)`,
		u.ID, username, hash, now())
	return u, err
}

func (d *DB) UserCount() (int, error) {
	var n int
	err := d.sql.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (d *DB) UserByName(name string) (User, string, error) {
	var u User
	var hash string
	err := d.sql.QueryRow(`SELECT id, username, password_hash FROM users WHERE username=?`, name).
		Scan(&u.ID, &u.Username, &hash)
	return u, hash, err
}

type Session struct {
	ID        string
	UserID    string
	CSRF      string
	ExpiresAt time.Time
}

func (d *DB) CreateSession(userID string, ttl time.Duration) (Session, error) {
	s := Session{
		ID: NewID("ses"), UserID: userID, CSRF: NewID("csrf"),
		ExpiresAt: time.Now().Add(ttl),
	}
	_, err := d.sql.Exec(`INSERT INTO sessions(id,user_id,csrf,expires_at) VALUES(?,?,?,?)`,
		s.ID, s.UserID, s.CSRF, s.ExpiresAt.UTC().Format(time.RFC3339Nano))
	return s, err
}

func (d *DB) Session(id string) (Session, error) {
	var s Session
	var exp string
	err := d.sql.QueryRow(`SELECT id,user_id,csrf,expires_at FROM sessions WHERE id=?`, id).
		Scan(&s.ID, &s.UserID, &s.CSRF, &exp)
	if err != nil {
		return s, err
	}
	s.ExpiresAt, err = time.Parse(time.RFC3339Nano, exp)
	if err != nil {
		return s, err
	}
	if time.Now().After(s.ExpiresAt) {
		return s, sql.ErrNoRows
	}
	return s, nil
}

func (d *DB) DeleteSession(id string) error {
	_, err := d.sql.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

type Enrollment struct {
	ID          string
	DisplayName string
	OSHint      string
	ExpiresAt   time.Time
	Consumed    bool
	Revoked     bool
}

func (d *DB) CreateEnrollment(name, osHint string, ttl time.Duration) (plain string, e Enrollment, err error) {
	plain, hash, err := identity.EnrollmentCode()
	if err != nil {
		return "", e, err
	}
	e = Enrollment{
		ID: NewID("enr"), DisplayName: name, OSHint: osHint,
		ExpiresAt: time.Now().Add(ttl),
	}
	_, err = d.sql.Exec(`INSERT INTO enrollment_codes(id,code_hash,display_name,os_hint,expires_at,created_at) VALUES(?,?,?,?,?,?)`,
		e.ID, hash, name, osHint, e.ExpiresAt.UTC().Format(time.RFC3339Nano), now())
	return plain, e, err
}

// ConsumeEnrollment atomically consumes a live code. Concurrent callers get one success.
func (d *DB) ConsumeEnrollment(plain string) (Enrollment, error) {
	hash := identity.HashCode(plain)
	tx, err := d.sql.Begin()
	if err != nil {
		return Enrollment{}, err
	}
	defer tx.Rollback()
	var e Enrollment
	var exp, consumed, revoked sql.NullString
	err = tx.QueryRow(`SELECT id, display_name, os_hint, expires_at, consumed_at, revoked_at
		FROM enrollment_codes WHERE code_hash=?`, hash).
		Scan(&e.ID, &e.DisplayName, &e.OSHint, &exp, &consumed, &revoked)
	if err != nil {
		return e, err
	}
	e.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp.String)
	if consumed.Valid || revoked.Valid {
		return e, errors.New("enrollment code already used or revoked")
	}
	if time.Now().After(e.ExpiresAt) {
		return e, errors.New("enrollment code expired")
	}
	res, err := tx.Exec(`UPDATE enrollment_codes SET consumed_at=? WHERE id=? AND consumed_at IS NULL AND revoked_at IS NULL`,
		now(), e.ID)
	if err != nil {
		return e, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return e, errors.New("enrollment code already used or revoked")
	}
	if err := tx.Commit(); err != nil {
		return e, err
	}
	e.Consumed = true
	return e, nil
}

func (d *DB) RevokeEnrollment(id string) error {
	_, err := d.sql.Exec(`UPDATE enrollment_codes SET revoked_at=? WHERE id=? AND consumed_at IS NULL`, now(), id)
	return err
}

type Node struct {
	ID           string                 `json:"id"`
	DisplayName  string                 `json:"display_name"`
	OS           string                 `json:"os"`
	Arch         string                 `json:"arch"`
	Version      string                 `json:"version"`
	Hostname     string                 `json:"hostname"`
	Status       protocol.NodeStatus    `json:"status"`
	PublicKey    string                 `json:"public_key"`
	Labels       map[string]string      `json:"labels"`
	Capabilities []protocol.Capability  `json:"capabilities"`
	Harnesses    []protocol.HarnessInfo `json:"harnesses"`
	Workspaces   []string               `json:"workspaces"`
	LastSeen     *time.Time             `json:"last_seen,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
}

func (d *DB) UpsertNode(n Node) error {
	labels, _ := json.Marshal(n.Labels)
	caps, _ := json.Marshal(n.Capabilities)
	har, _ := json.Marshal(n.Harnesses)
	ws, _ := json.Marshal(n.Workspaces)
	var seen any
	if n.LastSeen != nil {
		seen = n.LastSeen.UTC().Format(time.RFC3339Nano)
	}
	_, err := d.sql.Exec(`INSERT INTO nodes(id,display_name,os,arch,version,hostname,status,public_key,labels_json,capabilities_json,harnesses_json,workspaces_json,last_seen,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			display_name=excluded.display_name,
			os=excluded.os, arch=excluded.arch, version=excluded.version, hostname=excluded.hostname,
			status=CASE WHEN nodes.status='revoked' THEN 'revoked' ELSE excluded.status END,
			public_key=excluded.public_key,
			labels_json=excluded.labels_json,
			capabilities_json=excluded.capabilities_json,
			harnesses_json=excluded.harnesses_json,
			workspaces_json=excluded.workspaces_json,
			last_seen=excluded.last_seen`,
		n.ID, n.DisplayName, n.OS, n.Arch, n.Version, n.Hostname, n.Status, n.PublicKey,
		string(labels), string(caps), string(har), string(ws), seen, now())
	return err
}

func scanNode(sc interface{ Scan(...any) error }) (Node, error) {
	var n Node
	var labels, caps, har, ws, seen, created string
	var revoked sql.NullString
	err := sc.Scan(&n.ID, &n.DisplayName, &n.OS, &n.Arch, &n.Version, &n.Hostname, &n.Status, &n.PublicKey,
		&labels, &caps, &har, &ws, &seen, &revoked, &created)
	if err != nil {
		return n, err
	}
	_ = json.Unmarshal([]byte(labels), &n.Labels)
	_ = json.Unmarshal([]byte(caps), &n.Capabilities)
	_ = json.Unmarshal([]byte(har), &n.Harnesses)
	_ = json.Unmarshal([]byte(ws), &n.Workspaces)
	if seen != "" {
		t, e := time.Parse(time.RFC3339Nano, seen)
		if e == nil {
			n.LastSeen = &t
		}
	}
	n.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if revoked.Valid {
		n.Status = protocol.NodeRevoked
	}
	if n.Labels == nil {
		n.Labels = map[string]string{}
	}
	return n, nil
}

const nodeCols = `id,display_name,os,arch,version,hostname,status,public_key,labels_json,capabilities_json,harnesses_json,workspaces_json,COALESCE(last_seen,''),revoked_at,created_at`

func (d *DB) ListNodes() ([]Node, error) {
	rows, err := d.sql.Query(`SELECT ` + nodeCols + ` FROM nodes ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (d *DB) GetNode(id string) (Node, error) {
	row := d.sql.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE id=?`, id)
	return scanNode(row)
}

func (d *DB) TouchNode(id string, status protocol.NodeStatus) error {
	res, err := d.sql.Exec(`UPDATE nodes SET last_seen=?, status=CASE WHEN status='revoked' THEN 'revoked' ELSE ? END WHERE id=?`,
		now(), status, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (d *DB) MarkOfflineAll() error {
	_, err := d.sql.Exec(`UPDATE nodes SET status='offline' WHERE status='online'`)
	return err
}

func (d *DB) RevokeNode(id string) error {
	_, err := d.sql.Exec(`UPDATE nodes SET status='revoked', revoked_at=? WHERE id=?`, now(), id)
	return err
}

func (d *DB) UpdateNodeInventory(id string, caps []protocol.Capability, har []protocol.HarnessInfo, ws []string, osName, arch, ver, host, name string, labels map[string]string) error {
	cb, _ := json.Marshal(caps)
	hb, _ := json.Marshal(har)
	wb, _ := json.Marshal(ws)
	lb, _ := json.Marshal(labels)
	_, err := d.sql.Exec(`UPDATE nodes SET capabilities_json=?, harnesses_json=?, workspaces_json=?, os=?, arch=?, version=?, hostname=?,
		display_name=CASE WHEN ?<>'' THEN ? ELSE display_name END, labels_json=? WHERE id=? AND status!='revoked'`,
		string(cb), string(hb), string(wb), osName, arch, ver, host, name, name, string(lb), id)
	return err
}

type TaskRow struct {
	ID         string
	NodeID     string
	WorkflowID string
	StepID     string
	Kind       string
	SpecJSON   string
	Status     protocol.TaskStatus
	ExitCode   int
	Summary    string
	Output     string
	Error      string
	IdemKey    string
}

func (d *DB) InsertTask(spec protocol.TaskSpec) error {
	b, _ := json.Marshal(spec)
	_, err := d.sql.Exec(`INSERT INTO tasks(id,node_id,workflow_id,step_id,kind,spec_json,status,idempotency_key,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		spec.TaskID, "", spec.WorkflowID, spec.StepID, spec.Kind, string(b), protocol.TaskQueued, spec.Idempotency, now(), now())
	return err
}

func (d *DB) BindTaskNode(id, nodeID string) error {
	_, err := d.sql.Exec(`UPDATE tasks SET node_id=?, updated_at=? WHERE id=?`, nodeID, now(), id)
	return err
}

func (d *DB) SetTaskStatus(id string, st protocol.TaskStatus, exitCode int, summary, output, errStr string) error {
	// A terminal status is sticky. A late runner cannot replace cancelled,
	// succeeded, failed, timed_out, or uncertain with another outcome.
	res, err := d.sql.Exec(`UPDATE tasks SET status=?, exit_code=?, summary=CASE WHEN ?<>'' THEN ? ELSE summary END, output=CASE WHEN ?<>'' THEN ? ELSE output END, error=?, updated_at=?
		WHERE id=? AND status NOT IN ('succeeded','failed','cancelled','timed_out','uncertain')`,
		st, exitCode, summary, summary, output, clip(output, 200_000), errStr, now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	var cur string
	qerr := d.sql.QueryRow(`SELECT status FROM tasks WHERE id=?`, id).Scan(&cur)
	if qerr != nil {
		return qerr
	}
	if cur == string(st) {
		return nil
	}
	if terminalStatus(protocol.TaskStatus(cur)) {
		return ErrTerminal
	}
	return nil
}

func terminalStatus(st protocol.TaskStatus) bool {
	switch st {
	case protocol.TaskSucceeded, protocol.TaskFailed, protocol.TaskCancelled, protocol.TaskTimedOut, protocol.TaskUncertain:
		return true
	default:
		return false
	}
}

func (d *DB) GetTask(id string) (TaskRow, protocol.TaskSpec, error) {
	var t TaskRow
	err := d.sql.QueryRow(`SELECT id,COALESCE(node_id,''),COALESCE(workflow_id,''),COALESCE(step_id,''),COALESCE(kind,''),spec_json,status,COALESCE(exit_code,0),COALESCE(summary,''),COALESCE(output,''),COALESCE(error,''),COALESCE(idempotency_key,'') FROM tasks WHERE id=?`, id).
		Scan(&t.ID, &t.NodeID, &t.WorkflowID, &t.StepID, &t.Kind, &t.SpecJSON, &t.Status, &t.ExitCode, &t.Summary, &t.Output, &t.Error, &t.IdemKey)
	var spec protocol.TaskSpec
	if err == nil {
		_ = json.Unmarshal([]byte(t.SpecJSON), &spec)
	}
	return t, spec, err
}

// TaskByIdemOwner finds a task only inside one owner's key space.
func (d *DB) TaskByIdemOwner(owner, key string) (string, protocol.TaskStatus, bool) {
	if key == "" || owner == "" {
		return "", "", false
	}
	var id string
	var st protocol.TaskStatus
	err := d.sql.QueryRow(`SELECT id, status FROM tasks WHERE owner_id=? AND idempotency_key=?`, owner, key).Scan(&id, &st)
	if err != nil {
		return "", "", false
	}
	return id, st, true
}

func (d *DB) TaskByIdem(key string) (string, protocol.TaskStatus, bool) {
	if key == "" {
		return "", "", false
	}
	var id string
	var st protocol.TaskStatus
	err := d.sql.QueryRow(`SELECT id, status FROM tasks WHERE idempotency_key=?`, key).Scan(&id, &st)
	if err != nil {
		return "", "", false
	}
	return id, st, true
}

func (d *DB) AppendEvent(ev protocol.Event) error {
	b, _ := json.Marshal(ev)
	_, err := d.sql.Exec(`INSERT INTO task_events(task_id,seq,event_json,created_at) VALUES(?,?,?,?)`,
		ev.TaskID, ev.Sequence, string(b), now())
	return err
}

func (d *DB) Events(taskID string) ([]protocol.Event, error) {
	rows, err := d.sql.Query(`SELECT event_json FROM task_events WHERE task_id=? ORDER BY seq`, taskID)
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

func (d *DB) ListTasksByOwner(owner string, limit int) ([]TaskRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,COALESCE(node_id,''),COALESCE(workflow_id,''),COALESCE(step_id,''),COALESCE(kind,''),spec_json,status,COALESCE(exit_code,0),COALESCE(summary,''),COALESCE(output,''),COALESCE(error,''),COALESCE(idempotency_key,'') FROM tasks WHERE owner_id=? ORDER BY created_at DESC LIMIT ?`, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

func (d *DB) ListTasks(limit int) ([]TaskRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,COALESCE(node_id,''),COALESCE(workflow_id,''),COALESCE(step_id,''),COALESCE(kind,''),spec_json,status,COALESCE(exit_code,0),COALESCE(summary,''),COALESCE(output,''),COALESCE(error,''),COALESCE(idempotency_key,'') FROM tasks ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
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

// RecoverInflight marks running tasks uncertain so a restart cannot replay side effects.
func (d *DB) RecoverInflight() (int, error) {
	// A running task may already have caused a side effect. It becomes uncertain
	// and is not replayed. A queued task that never started can be scheduled again
	// only when side_effect is 0.
	res, err := d.sql.Exec(`UPDATE tasks SET status=?, error=COALESCE(error,'') || ' server restarted while running', updated_at=? WHERE status='running' OR (status='queued' AND side_effect=1)`,
		protocol.TaskUncertain, now())
	_, _ = d.sql.Exec(`UPDATE runs SET status=?, ended_at=? WHERE status='running'`, protocol.TaskUncertain, now())
	if err != nil {
		return 0, err
	}
	_, err = d.sql.Exec(`UPDATE tasks SET error='' WHERE status=? AND side_effect=0`, protocol.TaskQueued)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, _ = d.sql.Exec(`UPDATE workflow_steps SET status=? WHERE status IN ('running')`, protocol.StepUncertain)
	_, _ = d.sql.Exec(`UPDATE workflows SET status='interrupted', updated_at=? WHERE status='running'`, now())
	return int(n), nil
}

type WorkflowRow struct {
	ID        string
	Goal      string
	Planner   string
	Status    string
	PlanJSON  string
	Report    string
	CreatedAt string
	UpdatedAt string
}

func (d *DB) InsertWorkflow(id, goal, planner, planJSON string) error {
	_, err := d.sql.Exec(`INSERT INTO workflows(id,goal,planner,status,plan_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		id, goal, planner, "pending", planJSON, now(), now())
	return err
}

func (d *DB) UpdateWorkflowStatus(id, status, report string) error {
	_, err := d.sql.Exec(`UPDATE workflows SET status=?, report_json=CASE WHEN ?<>'' THEN ? ELSE report_json END, updated_at=? WHERE id=?`,
		status, report, report, now(), id)
	return err
}

func (d *DB) GetWorkflow(id string) (WorkflowRow, error) {
	var w WorkflowRow
	err := d.sql.QueryRow(`SELECT id,goal,planner,status,plan_json,COALESCE(report_json,''),created_at,updated_at FROM workflows WHERE id=?`, id).
		Scan(&w.ID, &w.Goal, &w.Planner, &w.Status, &w.PlanJSON, &w.Report, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

func (d *DB) ListWorkflows(limit int) ([]WorkflowRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.sql.Query(`SELECT id,goal,planner,status,plan_json,COALESCE(report_json,''),created_at,updated_at FROM workflows ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowRow
	for rows.Next() {
		var w WorkflowRow
		if err := rows.Scan(&w.ID, &w.Goal, &w.Planner, &w.Status, &w.PlanJSON, &w.Report, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

type StepRow struct {
	WorkflowID string
	StepID     string
	Status     protocol.StepStatus
	NodeID     string
	Reason     string
	TaskID     string
	InputJSON  string
	OutputJSON string
	Attempt    int
	Error      string
}

func (d *DB) UpsertStep(s StepRow) error {
	_, err := d.sql.Exec(`INSERT INTO workflow_steps(workflow_id,step_id,status,node_id,placement_reason,task_id,input_json,output_json,attempt,error)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(workflow_id,step_id) DO UPDATE SET
			status=excluded.status, node_id=excluded.node_id, placement_reason=excluded.placement_reason,
			task_id=excluded.task_id, input_json=excluded.input_json, output_json=excluded.output_json,
			attempt=excluded.attempt, error=excluded.error`,
		s.WorkflowID, s.StepID, s.Status, s.NodeID, s.Reason, s.TaskID, s.InputJSON, s.OutputJSON, s.Attempt, s.Error)
	return err
}

func (d *DB) Steps(workflowID string) ([]StepRow, error) {
	rows, err := d.sql.Query(`SELECT workflow_id,step_id,status,COALESCE(node_id,''),COALESCE(placement_reason,''),COALESCE(task_id,''),COALESCE(input_json,''),COALESCE(output_json,''),attempt,COALESCE(error,'') FROM workflow_steps WHERE workflow_id=?`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StepRow
	for rows.Next() {
		var s StepRow
		if err := rows.Scan(&s.WorkflowID, &s.StepID, &s.Status, &s.NodeID, &s.Reason, &s.TaskID, &s.InputJSON, &s.OutputJSON, &s.Attempt, &s.Error); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type Approval struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflow_id"`
	StepID     string `json:"step_id"`
	TaskID     string `json:"task_id"`
	NodeID     string `json:"node_id"`
	Action     string `json:"action"`
	ArgsJSON   string `json:"args_json"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expires_at"`
}

func (d *DB) CreateApproval(a Approval, ttl time.Duration) (Approval, error) {
	a.ID = NewID("apr")
	a.Status = "pending"
	exp := time.Now().Add(ttl)
	a.ExpiresAt = exp.UTC().Format(time.RFC3339Nano)
	_, err := d.sql.Exec(`INSERT INTO approvals(id,workflow_id,step_id,task_id,node_id,action,args_json,status,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.WorkflowID, a.StepID, a.TaskID, a.NodeID, a.Action, a.ArgsJSON, a.Status, a.ExpiresAt, now())
	return a, err
}

func (d *DB) GetApproval(id string) (Approval, error) {
	var a Approval
	err := d.sql.QueryRow(`SELECT id,COALESCE(workflow_id,''),COALESCE(step_id,''),COALESCE(task_id,''),COALESCE(node_id,''),action,COALESCE(args_json,''),status,expires_at FROM approvals WHERE id=?`, id).
		Scan(&a.ID, &a.WorkflowID, &a.StepID, &a.TaskID, &a.NodeID, &a.Action, &a.ArgsJSON, &a.Status, &a.ExpiresAt)
	return a, err
}

func (d *DB) DecideApproval(id, status string) error {
	a, err := d.GetApproval(id)
	if err != nil {
		return err
	}
	if a.Status != "pending" {
		return errors.New("approval already decided")
	}
	exp, _ := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if time.Now().After(exp) {
		_, _ = d.sql.Exec(`UPDATE approvals SET status='expired', decided_at=? WHERE id=?`, now(), id)
		return errors.New("approval expired")
	}
	res, err := d.sql.Exec(`UPDATE approvals SET status=?, decided_at=? WHERE id=? AND status='pending'`, status, now(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("approval already decided")
	}
	return nil
}

func (d *DB) ListApprovals(status string) ([]Approval, error) {
	q := `SELECT id,COALESCE(workflow_id,''),COALESCE(step_id,''),COALESCE(task_id,''),COALESCE(node_id,''),action,COALESCE(args_json,''),status,expires_at FROM approvals`
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = d.sql.Query(q+` WHERE status=? ORDER BY created_at DESC`, status)
	} else {
		rows, err = d.sql.Query(q + ` ORDER BY created_at DESC LIMIT 100`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Approval
	for rows.Next() {
		var a Approval
		if err := rows.Scan(&a.ID, &a.WorkflowID, &a.StepID, &a.TaskID, &a.NodeID, &a.Action, &a.ArgsJSON, &a.Status, &a.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) AddArtifact(id, taskID, wf, name, sha, kind, path string, size int64, exp *time.Time) error {
	var e any
	if exp != nil {
		e = exp.UTC().Format(time.RFC3339Nano)
	}
	_, err := d.sql.Exec(`INSERT INTO artifacts(id,task_id,workflow_id,name,sha256,size,kind,path,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, taskID, wf, name, sha, size, kind, path, e, now())
	return err
}

type Artifact struct {
	ID, TaskID, WorkflowID, Name, SHA256, Kind, Path, Expires string
	Size                                                      int64
}

func (d *DB) GetArtifact(id string) (Artifact, error) {
	var a Artifact
	var exp sql.NullString
	err := d.sql.QueryRow(`SELECT id,COALESCE(task_id,''),COALESCE(workflow_id,''),COALESCE(name,''),COALESCE(sha256,''),COALESCE(size,0),COALESCE(kind,''),COALESCE(path,''),expires_at FROM artifacts WHERE id=?`, id).
		Scan(&a.ID, &a.TaskID, &a.WorkflowID, &a.Name, &a.SHA256, &a.Size, &a.Kind, &a.Path, &exp)
	if exp.Valid {
		a.Expires = exp.String
	}
	return a, err
}

func (d *DB) Audit(who, node, task, wf, action, perm, result, detail string) {
	detail = redact(detail)
	_, _ = d.sql.Exec(`INSERT INTO audit_logs(who,node_id,task_id,workflow_id,action,permission,result,detail,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		who, node, task, wf, action, perm, result, clip(detail, 2000), now())
}

type AuditRow struct {
	Who, NodeID, TaskID, WorkflowID, Action, Permission, Result, Detail, CreatedAt string
}

func (d *DB) ListAudit(limit int) ([]AuditRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := d.sql.Query(`SELECT COALESCE(who,''),COALESCE(node_id,''),COALESCE(task_id,''),COALESCE(workflow_id,''),COALESCE(action,''),COALESCE(permission,''),COALESCE(result,''),COALESCE(detail,''),created_at FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var a AuditRow
		if err := rows.Scan(&a.Who, &a.NodeID, &a.TaskID, &a.WorkflowID, &a.Action, &a.Permission, &a.Result, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type ModelConfig struct {
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key,omitempty"`
	MaxSteps    int    `json:"max_steps"`
	TimeoutSec  int    `json:"timeout_sec"`
	TokenBudget int    `json:"token_budget"`
	Configured  bool   `json:"configured"`
}

func (d *DB) SaveModel(m ModelConfig, enc string) error {
	_, err := d.sql.Exec(`INSERT INTO model_config(id,base_url,model,api_key_enc,max_steps,timeout_sec,token_budget) VALUES(1,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET base_url=excluded.base_url, model=excluded.model,
		api_key_enc=CASE WHEN excluded.api_key_enc<>'' THEN excluded.api_key_enc ELSE model_config.api_key_enc END,
		max_steps=excluded.max_steps, timeout_sec=excluded.timeout_sec, token_budget=excluded.token_budget`,
		m.BaseURL, m.Model, enc, m.MaxSteps, m.TimeoutSec, m.TokenBudget)
	return err
}

func (d *DB) LoadModel() (ModelConfig, string, error) {
	var m ModelConfig
	var enc sql.NullString
	err := d.sql.QueryRow(`SELECT COALESCE(base_url,''),COALESCE(model,''),api_key_enc,COALESCE(max_steps,8),COALESCE(timeout_sec,120),COALESCE(token_budget,8000) FROM model_config WHERE id=1`).
		Scan(&m.BaseURL, &m.Model, &enc, &m.MaxSteps, &m.TimeoutSec, &m.TokenBudget)
	if errors.Is(err, sql.ErrNoRows) {
		m.MaxSteps = 8
		m.TimeoutSec = 120
		m.TokenBudget = 8000
		return m, "", nil
	}
	if enc.Valid {
		m.Configured = enc.String != ""
		return m, enc.String, err
	}
	return m, "", err
}

func (d *DB) SetPref(k, v string) error {
	_, err := d.sql.Exec(`INSERT INTO preferences(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

func (d *DB) Pref(k string) (string, error) {
	var v string
	err := d.sql.QueryRow(`SELECT value FROM preferences WHERE key=?`, k).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…[truncated]"
}

func redact(s string) string {
	low := strings.ToLower(s)
	for _, needle := range []string{"api_key", "apikey", "authorization", "secret", "password", "token"} {
		if strings.Contains(low, needle) {
			return "[redacted]"
		}
	}
	return s
}

// Ping is used by tests.
func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }
