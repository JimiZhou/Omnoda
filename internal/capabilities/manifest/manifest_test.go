package manifest

import (
	"strings"
	"testing"
)

const good = `{
  "apiVersion": "omnoda.dev/v1alpha1",
  "kind": "Capability",
  "metadata": {"id": "local.log-analyzer", "name": "Log Analyzer", "version": "0.1.0", "description": "count"},
  "spec": {
    "runtime": "python",
    "entrypoint": "src/main.py",
    "platforms": ["linux/amd64", "linux/arm64", "darwin/arm64"],
    "inputs": {"type": "object", "properties": {"input_file": {"type": "string"}}, "required": ["input_file"]},
    "outputs": {"type": "object", "properties": {"errors": {"type": "integer"}}},
    "permissions": {"filesystem": {"read": ["workspace"], "write": ["output"]}, "network": false},
    "limits": {"timeout_seconds": 30, "memory_mb": 256}
  }
}`

func TestParseGood(t *testing.T) {
	m, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Supports("linux", "amd64") || m.Supports("windows", "amd64") {
		t.Fatal("platform")
	}
}

func TestRejects(t *testing.T) {
	cases := []string{
		strings.Replace(good, `"network": false`, `"network": false, "shell": true`, 1),
		strings.Replace(good, `"workspace"`, `"/etc"`, 1),
		strings.Replace(good, `"python"`, `"elixir"`, 1),
		strings.Replace(good, `"src/main.py"`, `"../main.py"`, 1),
		strings.Replace(good, `"timeout_seconds": 30`, `"timeout_seconds": 0`, 1),
	}
	for _, c := range cases {
		if _, err := Parse([]byte(c)); err == nil {
			t.Fatalf("accepted bad manifest: %s", c[:80])
		}
	}
}
