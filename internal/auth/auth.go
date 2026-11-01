package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}

const (
	// sealPrefix marks AES-256-GCM records. Each record carries its own nonce.
	sealPrefix = "v2:"
	nonceSize  = 12
)

var (
	ErrSealAuth = errors.New("secret authentication failed")
	ErrSealKey  = errors.New("seal key must be 32 bytes")
)

// Seal encrypts a short secret with AES-256-GCM.
// The key file is local (0600). This is a local seal, not an OS keystore.
// The returned string is "v2:" + base64(nonce || ciphertext || tag).
func Seal(key, plain []byte) (string, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, plain, []byte(sealPrefix))
	return sealPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a v2 seal. It rejects tampered ciphertext.
// Legacy XOR seals are not opened here; call MigrateLegacy.
func Open(key []byte, sealed string) (string, error) {
	if !strings.HasPrefix(sealed, sealPrefix) {
		return "", errors.New("unsupported seal format")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, sealPrefix))
	if err != nil {
		return "", err
	}
	if len(raw) < nonceSize+aes.BlockSize {
		return "", ErrSealAuth
	}
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	nonce, ct := raw[:nonceSize], raw[nonceSize:]
	plain, err := aead.Open(nil, nonce, ct, []byte(sealPrefix))
	if err != nil {
		return "", ErrSealAuth
	}
	return string(plain), nil
}

// IsLegacy reports a v0.1 XOR seal (raw base64, no version prefix).
func IsLegacy(sealed string) bool {
	if sealed == "" || strings.HasPrefix(sealed, sealPrefix) {
		return false
	}
	_, err := base64.StdEncoding.DecodeString(sealed)
	return err == nil
}

// MigrateLegacy decrypts a v0.1 XOR seal and re-encrypts it with AES-256-GCM.
// On failure the original ciphertext is returned unchanged so callers never
// overwrite a key they could not read.
func MigrateLegacy(key []byte, sealed string) (next string, plain string, migrated bool, err error) {
	if strings.HasPrefix(sealed, sealPrefix) {
		plain, err = Open(key, sealed)
		return sealed, plain, false, err
	}
	if !IsLegacy(sealed) {
		return sealed, "", false, errors.New("unrecognized seal")
	}
	plain, err = openLegacy(key, sealed)
	if err != nil {
		return sealed, "", false, err
	}
	// XOR has no authentication tag. Refuse to replace the ciphertext unless
	// the plaintext is a printable secret. Garbage from the wrong key stays sealed.
	if !plausibleSecret(plain) {
		return sealed, "", false, errors.New("legacy seal did not yield a printable secret; original ciphertext kept")
	}
	next, err = Seal(key, []byte(plain))
	if err != nil {
		return sealed, plain, false, err
	}
	return next, plain, true, nil
}

func openLegacy(key []byte, sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	stream := legacyKeystream(key, len(raw))
	out := make([]byte, len(raw))
	for i := range raw {
		out[i] = raw[i] ^ stream[i]
	}
	return string(out), nil
}

func legacyKeystream(key []byte, n int) []byte {
	out := make([]byte, 0, n)
	var counter uint32
	for len(out) < n {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", key, counter)))
		out = append(out, h[:]...)
		counter++
	}
	return out[:n]
}

func plausibleSecret(s string) bool {
	if s == "" || len(s) > 4096 {
		return false
	}
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrSealKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func RandomKey() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}
