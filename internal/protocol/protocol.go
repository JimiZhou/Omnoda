// Package protocol is the versioned contract between the control plane,
// node daemons, and the web console.
package protocol

import "time"

const Version = "0.3.0"

// Wire protocol version negotiated on the node websocket.
const WireVersion = 1

// Execution levels. Privileged is off unless an operator explicitly arms it
// on the node. A model cannot turn it on.
type ExecLevel string

const (
	LevelReadOnly       ExecLevel = "READ_ONLY"
	LevelWorkspaceWrite ExecLevel = "WORKSPACE_WRITE"
	LevelPrivileged     ExecLevel = "PRIVILEGED"
)

type NodeStatus string

const (
	NodeOnline  NodeStatus = "online"
	NodeOffline NodeStatus = "offline"
	NodeRevoked NodeStatus = "revoked"
)

type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
	TaskTimedOut  TaskStatus = "timed_out"
	// TaskUncertain means a side effect may have happened and the ACK was lost.
	// The server must not replay it automatically.
	TaskUncertain TaskStatus = "uncertain"
)

type StepStatus string

const (
	StepPending         StepStatus = "pending"
	StepReady           StepStatus = "ready"
	StepWaitingApproval StepStatus = "waiting_approval"
	StepRunning         StepStatus = "running"
	StepSucceeded       StepStatus = "succeeded"
	StepFailed          StepStatus = "failed"
	StepSkipped         StepStatus = "skipped"
	StepCancelled       StepStatus = "cancelled"
	StepTimedOut        StepStatus = "timed_out"
	StepUncertain       StepStatus = "uncertain"
)

type RetryClass string

const (
	RetrySafe    RetryClass = "safe"
	RetryConfirm RetryClass = "confirm"
	RetryNever   RetryClass = "never"
)

type EventType string

const (
	EvRunStarted      EventType = "run.started"
	EvMessageDelta    EventType = "message.delta"
	EvToolStarted     EventType = "tool.started"
	EvToolCompleted   EventType = "tool.completed"
	EvArtifactCreated EventType = "artifact.created"
	EvRunCompleted    EventType = "run.completed"
	EvRunFailed       EventType = "run.failed"
	EvRunCancelled    EventType = "run.cancelled"
	EvLog             EventType = "log.line"
	EvHeartbeat       EventType = "heartbeat"
)

// Event is the normalized stream record. Raw harness payloads live in Raw,
// already redacted, and are not stored indefinitely by default.
type Event struct {
	EventID   string         `json:"event_id"`
	RunID     string         `json:"run_id"`
	NodeID    string         `json:"node_id,omitempty"`
	TaskID    string         `json:"task_id,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Sequence  int64          `json:"sequence"`
	EventType EventType      `json:"event_type"`
	Payload   map[string]any `json:"payload,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
	Origin    string         `json:"origin,omitempty"` // omnoda | harness
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type Capability struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Detail  string `json:"detail,omitempty"`
}

type HarnessInfo struct {
	ID      string `json:"id"`
	Present bool   `json:"present"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
}

type NodeHello struct {
	NodeID       string            `json:"node_id"`
	DisplayName  string            `json:"display_name"`
	OS           string            `json:"os"`
	Arch         string            `json:"arch"`
	Version      string            `json:"version"`
	Hostname     string            `json:"hostname"`
	Capabilities []Capability      `json:"capabilities"`
	Harnesses    []HarnessInfo     `json:"harnesses"`
	Workspaces   []string          `json:"workspaces"`
	Labels       map[string]string `json:"labels,omitempty"`
	PublicKey    string            `json:"public_key"` // base64 std
}

type Heartbeat struct {
	NodeID    string    `json:"node_id"`
	Timestamp time.Time `json:"timestamp"`
	Load      float64   `json:"load"`
	Running   int       `json:"running"`
}

// Envelope is one websocket frame. Requests carry a RequestID so responses
// and duplicate deliveries stay idempotent.
type Envelope struct {
	V         int            `json:"v"`
	Type      string         `json:"type"`
	RequestID string         `json:"request_id,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	NodeID    string         `json:"node_id,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Error     *ErrorBody     `json:"error,omitempty"`
}

const (
	MsgHello        = "node.hello"
	MsgHelloAck     = "node.hello_ack"
	MsgHeartbeat    = "node.heartbeat"
	MsgHeartbeatAck = "node.heartbeat_ack"
	MsgTaskAssign   = "task.assign"
	MsgTaskAck      = "task.ack"
	MsgTaskEvent    = "task.event"
	MsgTaskResult   = "task.result"
	MsgTaskCancel   = "task.cancel"
	MsgCancelAck    = "task.cancel_ack"
	MsgSync         = "node.sync"
	MsgError        = "error"
)

type TaskSpec struct {
	TaskID      string            `json:"task_id"`
	WorkflowID  string            `json:"workflow_id,omitempty"`
	StepID      string            `json:"step_id,omitempty"`
	Kind        string            `json:"kind"` // command | harness | native | builtin
	HarnessID   string            `json:"harness_id,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Prompt      string            `json:"prompt,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	TimeoutSec  int               `json:"timeout_sec"`
	Level       ExecLevel         `json:"level"`
	Idempotency string            `json:"idempotency_key"`
	ApprovalID  string            `json:"approval_id,omitempty"`
	EnvAllow    []string          `json:"env_allow,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
}

type TaskResult struct {
	TaskID    string        `json:"task_id"`
	Status    TaskStatus    `json:"status"`
	ExitCode  int           `json:"exit_code"`
	Summary   string        `json:"summary"`
	Output    string        `json:"output,omitempty"`
	Error     string        `json:"error,omitempty"`
	Session   string        `json:"session_id,omitempty"`
	Artifacts []ArtifactRef `json:"artifacts,omitempty"`
}

type ArtifactRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Kind   string `json:"kind"`
}

// Plan is what a planner is allowed to emit. The server re-checks every
// selector, workspace, and action against policy before anything runs.
type Plan struct {
	Goal    string     `json:"goal"`
	Planner string     `json:"planner"` // llm | template
	Steps   []PlanStep `json:"steps"`
}

type PlanStep struct {
	StepID       string         `json:"step_id"`
	Type         string         `json:"type"`
	Title        string         `json:"title"`
	Dependencies []string       `json:"dependencies"`
	Capabilities []string       `json:"required_capabilities"`
	Selector     NodeSelector   `json:"node_selector"`
	Input        map[string]any `json:"input"`
	Condition    string         `json:"condition,omitempty"`
	TimeoutSec   int            `json:"timeout_sec"`
	Retry        RetryPolicy    `json:"retry_policy"`
	Approval     string         `json:"approval_policy"` // none | required
	Level        ExecLevel      `json:"level"`
}

type NodeSelector struct {
	NodeID  string            `json:"node_id,omitempty"`
	Role    string            `json:"role,omitempty"`
	OS      string            `json:"os,omitempty"`
	Arch    string            `json:"arch,omitempty"`
	Harness string            `json:"harness,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	Reason  string            `json:"reason,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts int        `json:"max_attempts"`
	Class       RetryClass `json:"class"`
}

type Placement struct {
	StepID string `json:"step_id"`
	NodeID string `json:"node_id"`
	Reason string `json:"reason"`
}
