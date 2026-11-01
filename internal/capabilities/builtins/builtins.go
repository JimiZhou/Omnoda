// Package builtins installs the seed's built-in capabilities.
package builtins

import (
	"os"
	"path/filepath"

	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
)

const systemInfo = `{
  "apiVersion": "omnoda.dev/v1alpha1",
  "kind": "Capability",
  "metadata": {"id": "omnoda.system-info", "name": "System Info", "version": "0.1.0", "description": "Report the local OS and architecture"},
  "spec": {
    "runtime": "builtin",
    "platforms": ["linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"],
    "inputs": {"type": "object", "properties": {}},
    "outputs": {"type": "object", "properties": {"os": {"type": "string"}, "arch": {"type": "string"}}},
    "permissions": {"filesystem": {"read": [], "write": []}, "network": false},
    "limits": {"timeout_seconds": 5, "memory_mb": 32}
  }
}`

func Ensure(reg *registry.Registry) error {
	dir, err := os.MkdirTemp("", "omnoda-builtin-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "capability.json"), []byte(systemInfo), 0o600); err != nil {
		return err
	}
	rec, err := reg.Install(dir, "builtin", "omnoda", registry.VerifyPassed)
	if err != nil {
		return err
	}
	if rec.ActivationStatus != registry.StatusEnabled {
		_, err = reg.SetStatus(rec.CapabilityID, rec.Version, registry.StatusEnabled)
	}
	return err
}
