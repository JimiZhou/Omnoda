package upgrade

import "testing"

func TestProposalDoesNotApplyItself(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Propose("fix seal", "replace xor", "diff --git a/auth.go", []string{"go test ./internal/auth"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "proposed" {
		t.Fatal(p.Status)
	}
	if err := s.Decide(p.ID, "accepted"); err != nil {
		t.Fatal(err)
	}
	// Acceptance records a decision. There is no apply method.
}
