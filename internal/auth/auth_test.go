package auth

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestSealRoundTripAndTamper(t *testing.T) {
	key, err := RandomKey()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := Seal(key, []byte("sk-live-example"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, "v2:") {
		t.Fatalf("prefix: %s", sealed)
	}
	got, err := Open(key, sealed)
	if err != nil || got != "sk-live-example" {
		t.Fatalf("open: %q %v", got, err)
	}
	// Different nonce each time.
	again, _ := Seal(key, []byte("sk-live-example"))
	if again == sealed {
		t.Fatal("nonce reuse")
	}
	// Flip a ciphertext byte.
	raw := []byte(sealed)
	raw[len(raw)-2] ^= 0x1
	if _, err := Open(key, string(raw)); err == nil {
		t.Fatal("tamper accepted")
	}
	other, _ := RandomKey()
	if _, err := Open(other, sealed); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestLegacyMigrationDoesNotDropOnFailure(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	legacy := legacySealForTest(key, "old-api-key")
	next, plain, migrated, err := MigrateLegacy(key, legacy)
	if err != nil || !migrated || plain != "old-api-key" {
		t.Fatalf("migrate: %v %v %q", err, migrated, plain)
	}
	got, err := Open(key, next)
	if err != nil || got != "old-api-key" {
		t.Fatalf("reopen: %q %v", got, err)
	}
	// Already migrated is a no-op.
	next2, _, migrated2, err := MigrateLegacy(key, next)
	if err != nil || migrated2 || next2 != next {
		t.Fatalf("second pass: %v %v", err, migrated2)
	}
	// Wrong key must not produce a replacement ciphertext.
	bad := bytes.Repeat([]byte{9}, 32)
	kept, plain, migrated, err := MigrateLegacy(bad, legacy)
	if err == nil || migrated || kept != legacy || plain != "" {
		t.Fatalf("failed migrate overwrote: kept=%s migrated=%v err=%v", kept, migrated, err)
	}
}

func legacySealForTest(key []byte, plain string) string {
	// Reproduce the v0.1 XOR construction so the migration test does not import it as API.
	stream := legacyKeystream(key, len(plain))
	out := make([]byte, len(plain))
	for i := range plain {
		out[i] = plain[i] ^ stream[i]
	}
	return base64.StdEncoding.EncodeToString(out)
}
