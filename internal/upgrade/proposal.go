// Package upgrade records a self-upgrade proposal.
// A proposal is a patch plus test evidence. It does not replace the running
// binary, change the trust root, or merge itself.
package upgrade

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Proposal struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary"`
	Patch     string    `json:"patch"`
	Tests     []string  `json:"tests"`
	Status    string    `json:"status"` // proposed | rejected | accepted
	CreatedAt time.Time `json:"created_at"`
}

type Store struct{ Dir string }

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) Propose(title, summary, patch string, tests []string) (Proposal, error) {
	if title == "" || patch == "" {
		return Proposal{}, errors.New("title and patch are required")
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	p := Proposal{
		ID: "upg_" + hex.EncodeToString(b), Title: title, Summary: summary,
		Patch: patch, Tests: tests, Status: "proposed", CreatedAt: time.Now().UTC(),
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return Proposal{}, err
	}
	return p, os.WriteFile(filepath.Join(s.Dir, p.ID+".json"), raw, 0o600)
}

func (s *Store) Decide(id, status string) error {
	if status != "accepted" && status != "rejected" {
		return errors.New("status must be accepted or rejected")
	}
	path := filepath.Join(s.Dir, id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	p.Status = status
	raw, err = json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
