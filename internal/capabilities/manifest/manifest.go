// Package manifest validates versioned capability declarations.
// A manifest is a claim. Activation still requires local policy and approval.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const APIVersion = "omnoda.dev/v1alpha1"

var (
	idRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,80}$`)
	verRe  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	platRe = regexp.MustCompile(`^(linux|darwin|windows)/(amd64|arm64)$`)
)

var allowedRuntimes = map[string]bool{
	"builtin": true,
	"python":  true,
	"shell":   true, // argv only, no shell string
	"cli":     true,
}

type Manifest struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       Spec     `json:"spec"`
}

type Metadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type Spec struct {
	Runtime     string          `json:"runtime"`
	Entrypoint  string          `json:"entrypoint,omitempty"`
	Argv        []string        `json:"argv,omitempty"`
	Platforms   []string        `json:"platforms"`
	Inputs      json.RawMessage `json:"inputs"`
	Outputs     json.RawMessage `json:"outputs,omitempty"`
	Permissions Permissions     `json:"permissions"`
	Limits      Limits          `json:"limits"`
	Tests       TestRefs        `json:"tests,omitempty"`
}

type Permissions struct {
	Filesystem Filesystem `json:"filesystem"`
	Network    bool       `json:"network"`
}

type Filesystem struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`
}

type Limits struct {
	TimeoutSeconds int `json:"timeout_seconds"`
	MemoryMB       int `json:"memory_mb"`
	OutputKB       int `json:"output_kb,omitempty"`
}

type TestRefs struct {
	Builder  string `json:"builder,omitempty"`
	Verifier string `json:"verifier,omitempty"`
}

var (
	ErrManifest = errors.New("invalid capability manifest")
	// Resource classes a capability may name. They are not paths and not grants.
	classes = map[string]bool{"workspace": true, "output": true, "examples": true}
)

func Parse(b []byte) (Manifest, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifest, err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) Validate() error {
	if m.APIVersion != APIVersion {
		return fmt.Errorf("%w: apiVersion", ErrManifest)
	}
	if m.Kind != "Capability" {
		return fmt.Errorf("%w: kind", ErrManifest)
	}
	if !idRe.MatchString(m.Metadata.ID) {
		return fmt.Errorf("%w: id", ErrManifest)
	}
	if strings.TrimSpace(m.Metadata.Name) == "" || !verRe.MatchString(m.Metadata.Version) {
		return fmt.Errorf("%w: name or version", ErrManifest)
	}
	if !allowedRuntimes[m.Spec.Runtime] {
		return fmt.Errorf("%w: runtime %q", ErrManifest, m.Spec.Runtime)
	}
	if len(m.Spec.Platforms) == 0 {
		return fmt.Errorf("%w: platforms", ErrManifest)
	}
	for _, p := range m.Spec.Platforms {
		if !platRe.MatchString(p) {
			return fmt.Errorf("%w: platform %q", ErrManifest, p)
		}
	}
	if m.Spec.Runtime != "builtin" && m.Spec.Entrypoint == "" && len(m.Spec.Argv) == 0 {
		return fmt.Errorf("%w: entrypoint", ErrManifest)
	}
	if err := checkRel(m.Spec.Entrypoint); err != nil {
		return err
	}
	for _, a := range m.Spec.Argv {
		if strings.ContainsAny(a, "\n;&|`$") {
			return fmt.Errorf("%w: argv", ErrManifest)
		}
	}
	if err := checkSchema(m.Spec.Inputs); err != nil {
		return fmt.Errorf("%w: inputs: %v", ErrManifest, err)
	}
	if len(m.Spec.Outputs) > 0 {
		if err := checkSchema(m.Spec.Outputs); err != nil {
			return fmt.Errorf("%w: outputs: %v", ErrManifest, err)
		}
	}
	if err := checkClasses(m.Spec.Permissions.Filesystem.Read); err != nil {
		return err
	}
	if err := checkClasses(m.Spec.Permissions.Filesystem.Write); err != nil {
		return err
	}
	if m.Spec.Limits.TimeoutSeconds <= 0 || m.Spec.Limits.TimeoutSeconds > 120 {
		return fmt.Errorf("%w: timeout", ErrManifest)
	}
	if m.Spec.Limits.MemoryMB <= 0 || m.Spec.Limits.MemoryMB > 512 {
		return fmt.Errorf("%w: memory", ErrManifest)
	}
	return nil
}

func (m Manifest) Supports(goos, goarch string) bool {
	want := goos + "/" + goarch
	for _, p := range m.Spec.Platforms {
		if p == want {
			return true
		}
	}
	return false
}

func checkRel(p string) error {
	if p == "" {
		return nil
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "..") || strings.Contains(p, "\\") {
		return fmt.Errorf("%w: path %q", ErrManifest, p)
	}
	return nil
}

func checkClasses(ss []string) error {
	for _, s := range ss {
		if !classes[s] {
			return fmt.Errorf("%w: permission class %q", ErrManifest, s)
		}
	}
	return nil
}

func checkSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("missing")
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return err
	}
	if obj["type"] != "object" {
		return errors.New("type must be object")
	}
	return nil
}
