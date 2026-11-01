package storage

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEnrollmentOneTime(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	plain, _, err := db.CreateEnrollment("arm", "linux", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ok := 0
	var mu sync.Mutex
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := db.ConsumeEnrollment(plain); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("consumed %d times", ok)
	}
	if _, err := db.ConsumeEnrollment(plain); err == nil {
		t.Fatal("second consume should fail")
	}
}

func TestEnrollmentExpiredAndRevoked(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	plain, e, err := db.CreateEnrollment("n", "", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := db.ConsumeEnrollment(plain); err == nil {
		t.Fatal("expired code consumed")
	}
	plain2, e2, _ := db.CreateEnrollment("n", "", time.Minute)
	if err := db.RevokeEnrollment(e2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConsumeEnrollment(plain2); err == nil {
		t.Fatal("revoked code consumed")
	}
	_ = e
}

func TestRevokeBlocksReuse(t *testing.T) {
	db, _ := Open(filepath.Join(t.TempDir(), "t.db"))
	defer db.Close()
	n := Node{ID: "node_abc", DisplayName: "a", Status: "online", PublicKey: "k"}
	if err := db.UpsertNode(n); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeNode(n.ID); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetNode(n.ID)
	if err != nil || got.Status != "revoked" {
		t.Fatalf("status %+v %v", got.Status, err)
	}
	// A hello-style upsert must not clear revocation.
	n.Status = "online"
	if err := db.UpsertNode(n); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetNode(n.ID)
	if got.Status != "revoked" {
		t.Fatal(got.Status)
	}
}
