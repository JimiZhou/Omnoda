package evolver

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// sealIndependentFixture keeps the verifier fixture outside the generated tree
// until the verifier runs, then restores a copy the sandbox can read.
// The generator writes verifier/expected.json. That file is not trusted.
// The sealed fixture is what the verifier actually executes against.
func sealIndependentFixture(packageDir string, trusted []byte) (restore func(), err error) {
	dir := filepath.Join(packageDir, "verifier")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "expected.json")
	generated, _ := os.ReadFile(path)
	if err := os.WriteFile(path, trusted, 0o600); err != nil {
		return nil, err
	}
	return func() {
		if generated == nil {
			_ = os.Remove(path)
			return
		}
		_ = os.WriteFile(path, generated, 0o600)
	}, nil
}

// trustedSensorFixture is the system-owned sample for the sensor-log demo.
// Model output cannot change these numbers.
func trustedSensorFixture() []byte {
	// These numbers belong to the system. A generated expected.json is replaced
	// with this document before the verifier runs.
	b, _ := json.Marshal(map[string]any{
		"samples":           3,
		"anomalies":         1,
		"temperature_avg":   43.0,
		"temperature_count": 3,
	})
	return append(b, '\n')
}
