// Package evolver runs a bounded capability-acquisition state machine.
// It does not activate a capability. Activation waits for an explicit approval.
package evolver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/capabilities/manifest"
	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
)

type State string

const (
	StRequested        State = "requested"
	StResearching      State = "researching"
	StDesigning        State = "designing"
	StImplementing     State = "implementing"
	StTesting          State = "testing"
	StVerifying        State = "verifying"
	StAwaitingApproval State = "awaiting_approval"
	StActivated        State = "activated"
	StRejected         State = "rejected"
	StFailed           State = "failed"
)

type Record struct {
	ID           string            `json:"evolution_id"`
	Goal         string            `json:"original_goal"`
	Missing      string            `json:"missing_capability"`
	Constraints  map[string]string `json:"constraints,omitempty"`
	State        State             `json:"state"`
	AttemptCount int               `json:"attempt_count"`
	CapabilityID string            `json:"capability_id,omitempty"`
	Version      string            `json:"generated_version,omitempty"`
	ContentHash  string            `json:"content_hash,omitempty"`
	TestsRun     []string          `json:"tests_run,omitempty"`
	TestResults  map[string]bool   `json:"test_results,omitempty"`
	Evidence     []Evidence        `json:"evidence,omitempty"`
	TokenUsage   int               `json:"token_usage"`
	StartedAt    time.Time         `json:"started_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Error        string            `json:"error,omitempty"`
	FinalStatus  string            `json:"final_status,omitempty"`
	PackageDir   string            `json:"package_dir,omitempty"`
}

type Evidence struct {
	Phase  string    `json:"phase"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// Generator produces a candidate package in dest.
// A deterministic generator is used when no model is configured.
// A model-backed generator must be supplied by the caller; this package
// never pretends a template is an LLM.
type Generator interface {
	Generate(ctx context.Context, goal string, dest string) (tokens int, err error)
}

type Store struct {
	Dir string
	Reg *registry.Registry
}

func Open(dir string, reg *registry.Registry) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir, Reg: reg}, nil
}

func (s *Store) Start(goal, missing string, constraints map[string]string) (Record, error) {
	id := newID()
	rec := Record{
		ID: id, Goal: goal, Missing: missing, Constraints: constraints,
		State: StRequested, StartedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		TestResults: map[string]bool{}, Evidence: []Evidence{},
	}
	return rec, s.save(rec)
}

// Run advances a record until it waits for approval or fails.
// gen may be nil only when a deterministic fixture generator is appropriate
// for tests. Production callers pass a model generator or get ErrNoModel.
func (s *Store) Run(ctx context.Context, id string, gen Generator, maxAttempts int) (Record, error) {
	rec, err := s.Get(id)
	if err != nil {
		return Record{}, err
	}
	if maxAttempts <= 0 {
		maxAttempts = 2
	}
	if gen == nil {
		rec.State = StFailed
		rec.Error = "model provider is not configured; capability generation is unavailable"
		rec.FinalStatus = "failed"
		rec.UpdatedAt = time.Now().UTC()
		return rec, s.save(rec)
	}
	for rec.AttemptCount < maxAttempts && rec.State != StAwaitingApproval && rec.State != StFailed && rec.State != StRejected && rec.State != StActivated {
		if ctx.Err() != nil {
			rec.State = StFailed
			rec.Error = ctx.Err().Error()
			break
		}
		rec.AttemptCount++
		var stepErr error
		for rec.State != StAwaitingApproval && rec.State != StFailed && rec.State != StRejected && rec.State != StActivated {
			rec, stepErr = s.step(ctx, rec, gen)
			if stepErr != nil {
				rec.Error = stepErr.Error()
				rec.State = StFailed
				rec.FinalStatus = "failed"
				break
			}
			if err := s.save(rec); err != nil {
				return rec, err
			}
		}
		if stepErr != nil {
			break
		}
	}
	_ = s.save(rec)
	return rec, nil
}

func (s *Store) step(ctx context.Context, rec Record, gen Generator) (Record, error) {
	switch rec.State {
	case StRequested:
		rec.State = StResearching
		rec.Evidence = append(rec.Evidence, Evidence{Phase: "researching", Detail: "checked registry for " + rec.Missing, At: time.Now().UTC()})
		if s.Reg != nil {
			if existing, err := s.Reg.Get(rec.Missing, ""); err == nil && existing.ActivationStatus == registry.StatusEnabled {
				rec.State = StFailed
				rec.Error = "capability already enabled: " + existing.CapabilityID
				rec.FinalStatus = "failed"
				return rec, errors.New(rec.Error)
			}
		}
		return rec, nil
	case StResearching:
		rec.State = StDesigning
		rec.Evidence = append(rec.Evidence, Evidence{Phase: "designing", Detail: "plan: python parser, isolated tests, no network", At: time.Now().UTC()})
		return rec, nil
	case StDesigning:
		rec.State = StImplementing
		dir, err := os.MkdirTemp(s.Dir, "build-")
		if err != nil {
			return rec, err
		}
		tokens, err := gen.Generate(ctx, rec.Goal, dir)
		rec.TokenUsage += tokens
		if err != nil {
			return rec, err
		}
		if _, err := manifest.Parse(mustRead(filepath.Join(dir, "capability.json"))); err != nil {
			return rec, fmt.Errorf("generated manifest: %w", err)
		}
		rec.PackageDir = dir
		rec.Evidence = append(rec.Evidence, Evidence{Phase: "implementing", Detail: "package written to isolated dir", At: time.Now().UTC()})
		return rec, nil
	case StImplementing:
		rec.State = StTesting
		ok, detail := runBuilder(rec.PackageDir)
		rec.TestsRun = append(rec.TestsRun, "builder")
		if rec.TestResults == nil {
			rec.TestResults = map[string]bool{}
		}
		rec.TestResults["builder"] = ok
		rec.Evidence = append(rec.Evidence, Evidence{Phase: "testing", Detail: detail, At: time.Now().UTC()})
		if !ok {
			return rec, errors.New("builder tests failed: " + detail)
		}
		return rec, nil
	case StTesting:
		rec.State = StVerifying
		restore, err := sealIndependentFixture(rec.PackageDir, trustedSensorFixture())
		if err != nil {
			return rec, err
		}
		ok, detail := runVerifier(rec.PackageDir)
		restore()
		rec.TestsRun = append(rec.TestsRun, "verifier")
		rec.TestResults["verifier"] = ok
		// Builder success is self-test only. Independent verification is a separate bit.
		rec.TestResults["functional_pass"] = ok
		rec.TestResults["schema_pass"] = true
		rec.TestResults["security_pass"] = securityCheck(rec.PackageDir)
		rec.TestResults["independent"] = ok
		rec.Evidence = append(rec.Evidence, Evidence{Phase: "verifying", Detail: detail, At: time.Now().UTC()})
		if !ok || !rec.TestResults["security_pass"] {
			return rec, errors.New("verifier failed: " + detail)
		}
		return rec, nil
	case StVerifying:
		verify := registry.VerifyUnverified
		if rec.TestResults["independent"] && rec.TestResults["security_pass"] && rec.TestResults["verifier"] {
			verify = registry.VerifyPassed
		}
		installed, err := s.Reg.Install(rec.PackageDir, "evolver", "omnoda-evolver", verify)
		if err != nil {
			return rec, err
		}
		rec.CapabilityID = installed.CapabilityID
		rec.Version = installed.Version
		rec.ContentHash = installed.ContentHash
		rec.State = StAwaitingApproval
		rec.FinalStatus = "awaiting_approval"
		rec.Evidence = append(rec.Evidence, Evidence{Phase: "awaiting_approval", Detail: "installed " + installed.ContentHash[:12] + " not enabled", At: time.Now().UTC()})
		return rec, nil
	default:
		return rec, nil
	}
}

func (s *Store) Approve(id string) (Record, error) {
	rec, err := s.Get(id)
	if err != nil {
		return Record{}, err
	}
	if rec.State != StAwaitingApproval {
		return rec, fmt.Errorf("cannot approve from %s", rec.State)
	}
	if _, err := s.Reg.SetStatus(rec.CapabilityID, rec.Version, registry.StatusEnabled); err != nil {
		return rec, err
	}
	rec.State = StActivated
	rec.FinalStatus = "activated"
	rec.UpdatedAt = time.Now().UTC()
	rec.Evidence = append(rec.Evidence, Evidence{Phase: "activated", Detail: "operator approved", At: rec.UpdatedAt})
	return rec, s.save(rec)
}

func (s *Store) Reject(id string) (Record, error) {
	rec, err := s.Get(id)
	if err != nil {
		return Record{}, err
	}
	if rec.State != StAwaitingApproval && rec.State != StRequested {
		return rec, fmt.Errorf("cannot reject from %s", rec.State)
	}
	rec.State = StRejected
	rec.FinalStatus = "rejected"
	rec.UpdatedAt = time.Now().UTC()
	return rec, s.save(rec)
}

func (s *Store) Get(id string) (Record, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, id+".json"))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	err = json.Unmarshal(b, &rec)
	return rec, err
}

func (s *Store) List() ([]Record, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, err := s.Get(strings.TrimSuffix(e.Name(), ".json"))
		if err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (s *Store) save(rec Record) error {
	rec.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, rec.ID+".json"), b, 0o600)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "evo_" + hex.EncodeToString(b)
}

func mustRead(p string) []byte {
	b, _ := os.ReadFile(p)
	return b
}
