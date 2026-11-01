// Package identity owns node Ed25519 keys and enrollment-code hashing.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

func Generate() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyPair{Public: pub, Private: priv}, nil
}

func (k *KeyPair) PublicB64() string {
	return base64.StdEncoding.EncodeToString(k.Public)
}

func ParsePublic(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key length %d", len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

func Sign(priv ed25519.PrivateKey, msg []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, msg))
}

func Verify(pub ed25519.PublicKey, msg []byte, sigB64 string) error {
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, msg, sig) {
		return errors.New("signature mismatch")
	}
	return nil
}

// SavePrivate writes the key as PEM with mode 0600. The directory is 0700.
func SavePrivate(dir string, priv ed25519.PrivateKey) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	block := &pem.Block{Type: "OMNODA NODE PRIVATE KEY", Bytes: priv}
	path := filepath.Join(dir, "node.key")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pem.EncodeToMemory(block), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadOrCreate returns the existing key or creates one exactly once.
func LoadOrCreate(dir string) (ed25519.PrivateKey, error) {
	priv, err := LoadPrivate(dir)
	if err == nil {
		return priv, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	kp, err := Generate()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "node.key")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadPrivate(dir)
		}
		return nil, err
	}
	block := &pem.Block{Type: "OMNODA NODE PRIVATE KEY", Bytes: kp.Private}
	if _, err := f.Write(pem.EncodeToMemory(block)); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return kp.Private, nil
}

func LoadPrivate(dir string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(filepath.Join(dir, "node.key"))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil || len(block.Bytes) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid node key")
	}
	return ed25519.PrivateKey(block.Bytes), nil
}

func PublicFromPrivate(priv ed25519.PrivateKey) ed25519.PublicKey {
	return priv.Public().(ed25519.PublicKey)
}

func PublicB64(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// NodeID is a stable identifier derived from the public key, not from IP or hostname.
func NodeID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "node_" + hex.EncodeToString(sum[:16])
}

// EnrollmentCode returns a 128-bit code and its SHA-256 hex. Only the hash is stored.
func EnrollmentCode() (plain, hash string, err error) {
	buf := make([]byte, 16)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	plain = strings.ToUpper(hex.EncodeToString(buf))
	// Group for readability: XXXX-XXXX-...
	var parts []string
	for i := 0; i < len(plain); i += 4 {
		parts = append(parts, plain[i:i+4])
	}
	plain = strings.Join(parts, "-")
	return plain, HashCode(plain), nil
}

func HashCode(plain string) string {
	n := strings.ToUpper(strings.ReplaceAll(plain, "-", ""))
	sum := sha256.Sum256([]byte(n))
	return hex.EncodeToString(sum[:])
}

func NormalizeCode(plain string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(plain), "-", ""))
}
