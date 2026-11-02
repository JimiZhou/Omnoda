// Package supervisor runs tasks off the HTTP request context.
// Closing a browser, dropping SSE, or ending a voice session does not cancel work.
// Only an explicit user cancel, or process shutdown, stops a run.
package supervisor

import (
	"context"
	"sync"
	"time"

	"github.com/JimiZhou/Omnoda/internal/protocol"
	"github.com/JimiZhou/Omnoda/internal/storage"
)

type Runner func(ctx context.Context, task storage.TaskRow, spec protocol.TaskSpec) protocol.TaskResult

type Supervisor struct {
	DB     *storage.DB
	Run    Runner
	mu     sync.Mutex
	active map[string]context.CancelFunc
	parent context.Context
}

func New(parent context.Context, db *storage.DB, run Runner) *Supervisor {
	if parent == nil {
		parent = context.Background()
	}
	return &Supervisor{DB: db, Run: run, active: map[string]context.CancelFunc{}, parent: parent}
}

// Start detaches execution from the caller. The returned cancel is stored for explicit user cancel.
func (s *Supervisor) Start(row storage.TaskRow, spec protocol.TaskSpec) {
	s.mu.Lock()
	if _, ok := s.active[row.ID]; ok {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(s.parent)
	s.active[row.ID] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.active, row.ID)
			s.mu.Unlock()
		}()
		_ = s.DB.SetTaskStatus(row.ID, protocol.TaskRunning, 0, "", "", "")
		run, err := s.DB.StartRun(row.ID, row.NodeID, spec.HarnessID)
		if err != nil {
			_ = s.DB.SetTaskStatus(row.ID, protocol.TaskFailed, 1, "", "", err.Error())
			return
		}
		res := s.Run(ctx, row, spec)
		if res.TaskID == "" {
			res.TaskID = row.ID
		}
		_ = s.DB.FinishRun(run.ID, string(res.Status), res.Summary, "", false)
		_ = s.DB.SetTaskStatus(row.ID, res.Status, res.ExitCode, res.Summary, "", res.Error)
		seq, _ := s.DB.NextEventSeq(row.ID)
		_ = s.DB.AppendEventOnce(protocol.Event{
			EventType: protocol.EvRunCompleted, Sequence: seq, Timestamp: time.Now().UTC(),
			TaskID: row.ID, Origin: "omnoda", Payload: map[string]any{"status": res.Status, "summary": res.Summary},
		})
	}()
}

// Cancel is the only path that stops a live run.
func (s *Supervisor) Cancel(taskID string) bool {
	s.mu.Lock()
	cancel, ok := s.active[taskID]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

func (s *Supervisor) Running(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.active[taskID]
	return ok
}
