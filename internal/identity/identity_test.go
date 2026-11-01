package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSignRoundTripAndPerms(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := SavePrivate(dir, kp.Private); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "node.key"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	priv, err := LoadPrivate(dir)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("nonce")
	sig := Sign(priv, msg)
	if err := Verify(PublicFromPrivate(priv), msg, sig); err != nil {
		t.Fatal(err)
	}
	if NodeID(kp.Public) == "" {
		t.Fatal("empty id")
	}
}

func TestCodeHashStable(t *testing.T) {
	if HashCode("ABCD-EFGH") != HashCode("abcdefg h") && HashCode("ABCD-EFGH") != HashCode("abcdefgh") {
		t.Fatal(HashCode("ABCD-EFGH"))
	}
	if HashCode("ABCD-EFGH") != HashCode("abcdefgh") {
		t.Fatal("normalization")
	}
}
