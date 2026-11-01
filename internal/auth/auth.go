package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}

// Seal encrypts a short secret with a random key stored beside the database.
// This is a local seal, not a substitute for an OS keystore. The key file is 0600.
func Seal(key, plain []byte) string {
	// XOR with SHA-256 keystream. Sufficient to keep the key out of plaintext
	// SQLite and logs for a single-operator POC. Documented limitation.
	out := make([]byte, len(plain))
	stream := keystream(key, len(plain))
	for i := range plain {
		out[i] = plain[i] ^ stream[i]
	}
	return base64.StdEncoding.EncodeToString(out)
}

func Open(key []byte, sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	stream := keystream(key, len(raw))
	out := make([]byte, len(raw))
	for i := range raw {
		out[i] = raw[i] ^ stream[i]
	}
	return string(out), nil
}

func keystream(key []byte, n int) []byte {
	out := make([]byte, 0, n)
	var counter uint32
	for len(out) < n {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", key, counter)))
		out = append(out, h[:]...)
		counter++
	}
	return out[:n]
}

func RandomKey() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}
