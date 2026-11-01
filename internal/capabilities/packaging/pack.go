// Package packaging exports and imports capability bundles.
// A bundle holds the manifest, source, lockfile, and test metadata.
// It must not contain secrets, host paths, or device keys.
package packaging

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JimiZhou/Omnoda/internal/capabilities/manifest"
	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
)

type BundleMeta struct {
	ContentHash string `json:"content_hash"`
	Origin      string `json:"origin"`
	Author      string `json:"author_or_generator"`
}

var forbiddenNames = map[string]bool{
	"record.json": true, "seal.key": true, "node.key": true, "id_ed25519": true,
}

func Export(rec registry.Record, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	meta := BundleMeta{ContentHash: rec.ContentHash, Origin: rec.Origin, Author: rec.Author}
	b, _ := json.MarshalIndent(meta, "", "  ")
	if err := writeBytes(tw, "bundle.json", b); err != nil {
		return err
	}
	return filepath.Walk(rec.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(rec.Dir, path)
		if rel == "record.json" || forbiddenNames[filepath.Base(rel)] {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if looksSecret(body) {
			return fmt.Errorf("refusing to export %s: looks like a secret", rel)
		}
		return writeBytes(tw, filepath.ToSlash(rel), body)
	})
}

func Import(reg *registry.Registry, bundlePath string) (registry.Record, error) {
	f, err := os.Open(bundlePath)
	if err != nil {
		return registry.Record{}, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return registry.Record{}, err
	}
	defer gz.Close()
	dir, err := os.MkdirTemp("", "omnoda-import-")
	if err != nil {
		return registry.Record{}, err
	}
	defer os.RemoveAll(dir)
	tr := tar.NewReader(gz)
	var claimed string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return registry.Record{}, err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(hdr.Name) {
			return registry.Record{}, errors.New("bundle path escapes")
		}
		if forbiddenNames[filepath.Base(name)] {
			continue
		}
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return registry.Record{}, err
		}
		body, err := io.ReadAll(io.LimitReader(tr, 8<<20))
		if err != nil {
			return registry.Record{}, err
		}
		if name == "bundle.json" {
			var meta BundleMeta
			_ = json.Unmarshal(body, &meta)
			claimed = meta.ContentHash
			continue
		}
		if err := os.WriteFile(target, body, 0o600); err != nil {
			return registry.Record{}, err
		}
	}
	if _, err := manifest.Parse(must(filepath.Join(dir, "capability.json"))); err != nil {
		return registry.Record{}, err
	}
	hash, err := registry.HashDir(dir)
	if err != nil {
		return registry.Record{}, err
	}
	if claimed != "" && claimed != hash {
		return registry.Record{}, fmt.Errorf("bundle hash mismatch: claimed %s got %s", claimed[:12], hash[:12])
	}
	// Imported packages are installed but not enabled.
	return reg.Install(dir, "import", "import", registry.VerifyNone)
}

func writeBytes(tw *tar.Writer, name string, body []byte) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

func looksSecret(b []byte) bool {
	s := string(b)
	sum := sha256.Sum256(b)
	_ = hex.EncodeToString(sum[:])
	for _, needle := range []string{"BEGIN OPENSSH PRIVATE", "BEGIN PRIVATE KEY", "sk-live-", "OMNODA_MODEL_API_KEY"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func must(p string) []byte {
	b, _ := os.ReadFile(p)
	return b
}
