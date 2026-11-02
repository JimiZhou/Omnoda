// Package registry stores capability packages and their metadata on disk.
// The active version of a capability is never replaced silently.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/JimiZhou/Omnoda/internal/capabilities/manifest"
)

type Status string

const (
	StatusInstalled Status = "installed"
	StatusEnabled   Status = "enabled"
	StatusDisabled  Status = "disabled"
)

type Verify string

const (
	VerifyNone       Verify = "none"
	VerifyPassed     Verify = "passed"
	VerifyFailed     Verify = "failed"
	VerifyUnverified Verify = "unverified"
)

type Record struct {
	CapabilityID        string               `json:"capability_id"`
	Version             string               `json:"version"`
	Manifest            manifest.Manifest    `json:"manifest"`
	ContentHash         string               `json:"content_hash"`
	Origin              string               `json:"origin"`
	Author              string               `json:"author_or_generator"`
	Runtime             string               `json:"runtime"`
	Dependencies        []string             `json:"dependencies,omitempty"`
	RequiredPermissions manifest.Permissions `json:"required_permissions"`
	VerificationStatus  Verify               `json:"verification_status"`
	ActivationStatus    Status               `json:"activation_status"`
	CreatedAt           time.Time            `json:"created_at"`
	Dir                 string               `json:"dir"`
}

type Registry struct {
	Root string
}

func Open(root string) (*Registry, error) {
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0o700); err != nil {
		return nil, err
	}
	return &Registry{Root: root}, nil
}

// Install copies a package directory into the content-addressed store.
// If the same hash is already present, the existing record is returned.
// An enabled version of the same id is left enabled; the new version stays installed.
func (r *Registry) Install(src, origin, author string, verify Verify) (Record, error) {
	mfPath := filepath.Join(src, "capability.json")
	b, err := os.ReadFile(mfPath)
	if err != nil {
		return Record{}, err
	}
	m, err := manifest.Parse(b)
	if err != nil {
		return Record{}, err
	}
	hash, err := HashDir(src)
	if err != nil {
		return Record{}, err
	}
	dest := filepath.Join(r.Root, "packages", hash)
	if _, err := os.Stat(filepath.Join(dest, "record.json")); err == nil {
		return r.read(dest)
	}
	if err := copyTree(src, dest); err != nil {
		return Record{}, err
	}
	rec := Record{
		CapabilityID: m.Metadata.ID, Version: m.Metadata.Version, Manifest: m,
		ContentHash: hash, Origin: origin, Author: author, Runtime: m.Spec.Runtime,
		RequiredPermissions: m.Spec.Permissions, VerificationStatus: verify,
		ActivationStatus: StatusInstalled, CreatedAt: time.Now().UTC(), Dir: dest,
	}
	if err := writeRecord(dest, rec); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (r *Registry) List() ([]Record, error) {
	root := filepath.Join(r.Root, "packages")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rec, err := r.read(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CapabilityID == out[j].CapabilityID {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].CapabilityID < out[j].CapabilityID
	})
	return out, nil
}

func (r *Registry) Get(id, version string) (Record, error) {
	all, err := r.List()
	if err != nil {
		return Record{}, err
	}
	var match []Record
	for _, rec := range all {
		if rec.CapabilityID == id && (version == "" || rec.Version == version) {
			match = append(match, rec)
		}
	}
	if len(match) == 0 {
		return Record{}, os.ErrNotExist
	}
	// Prefer the enabled version, otherwise the newest.
	for _, rec := range match {
		if rec.ActivationStatus == StatusEnabled {
			return rec, nil
		}
	}
	return match[0], nil
}

func (r *Registry) Versions(id string) ([]Record, error) {
	all, err := r.List()
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, rec := range all {
		if rec.CapabilityID == id {
			out = append(out, rec)
		}
	}
	if len(out) == 0 {
		return nil, os.ErrNotExist
	}
	return out, nil
}

func (r *Registry) SetStatus(id, version string, st Status) (Record, error) {
	rec, err := r.byVersion(id, version)
	if err != nil {
		return Record{}, err
	}
	if st == StatusEnabled {
		if rec.VerificationStatus != VerifyPassed && rec.Origin != "builtin" {
			return Record{}, errors.New("capability is not verified")
		}
		// Confirm the candidate still matches the hash recorded at install.
		// A package edited after verification must not become the active version.
		hash, err := HashDirExcept(rec.Dir, "record.json")
		if err != nil {
			return Record{}, err
		}
		if hash != rec.ContentHash {
			return Record{}, fmt.Errorf("content hash mismatch for %s", rec.CapabilityID)
		}
	}
	prev := rec.ActivationStatus
	rec.ActivationStatus = st
	if err := writeRecord(rec.Dir, rec); err != nil {
		return Record{}, err
	}
	if st == StatusEnabled {
		all, err := r.Versions(id)
		if err != nil {
			rec.ActivationStatus = prev
			_ = writeRecord(rec.Dir, rec)
			return Record{}, err
		}
		var disabled []Record
		for _, other := range all {
			if other.ContentHash == rec.ContentHash || other.ActivationStatus != StatusEnabled {
				continue
			}
			other.ActivationStatus = StatusDisabled
			if err := writeRecord(other.Dir, other); err != nil {
				for _, back := range disabled {
					back.ActivationStatus = StatusEnabled
					_ = writeRecord(back.Dir, back)
				}
				rec.ActivationStatus = prev
				_ = writeRecord(rec.Dir, rec)
				return Record{}, err
			}
			disabled = append(disabled, other)
		}
	}
	return rec, nil
}

func (r *Registry) Rollback(id, version string) (Record, error) {
	return r.SetStatus(id, version, StatusEnabled)
}

func (r *Registry) byVersion(id, version string) (Record, error) {
	all, err := r.Versions(id)
	if err != nil {
		return Record{}, err
	}
	if version == "" {
		return all[0], nil
	}
	for _, rec := range all {
		if rec.Version == version {
			return rec, nil
		}
	}
	return Record{}, os.ErrNotExist
}

func (r *Registry) read(dir string) (Record, error) {
	b, err := os.ReadFile(filepath.Join(dir, "record.json"))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return Record{}, err
	}
	rec.Dir = dir
	// The package bytes must still match the stored hash.
	hash, err := HashDirExcept(dir, "record.json")
	if err != nil {
		return Record{}, err
	}
	if hash != rec.ContentHash {
		return Record{}, fmt.Errorf("content hash mismatch for %s", rec.CapabilityID)
	}
	if _, err := manifest.Parse(mustRead(filepath.Join(dir, "capability.json"))); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func writeRecord(dir string, rec Record) error {
	rec.Dir = ""
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "record.json"), b, 0o600)
}

func mustRead(p string) []byte {
	b, _ := os.ReadFile(p)
	return b
}

// HashDir hashes every file except record.json, in path order.
func HashDir(root string) (string, error) { return HashDirExcept(root, "record.json") }

func HashDirExcept(root, skip string) (string, error) {
	h := sha256.New()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == skip {
			return nil
		}
		rel = filepath.ToSlash(rel)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.WriteString(h, rel+"\n"); err != nil {
			return err
		}
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "record.json" {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func Enabled(list []Record) []Record {
	var out []Record
	for _, r := range list {
		if r.ActivationStatus == StatusEnabled {
			out = append(out, r)
		}
	}
	return out
}

func Summary(list []Record) string {
	var b strings.Builder
	for _, r := range list {
		fmt.Fprintf(&b, "%s %s %s %s\n", r.CapabilityID, r.Version, r.ActivationStatus, r.Manifest.Metadata.Description)
	}
	return b.String()
}
