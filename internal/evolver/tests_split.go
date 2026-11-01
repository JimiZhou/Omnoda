package evolver

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Builder tests are the generator's own checks.
// Verifier tests use fixtures the generator is not allowed to rewrite:
// they live in verifier/expected.json and are hashed into the evidence.

func runBuilder(dir string) (bool, string) {
	script := filepath.Join(dir, "builder_test.py")
	if _, err := os.Stat(script); err != nil {
		return false, "builder_test.py missing"
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return false, "python3 missing"
	}
	cmd := exec.Command("python3", "builder_test.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(out)
	}
	return true, strings.TrimSpace(string(out))
}

func runVerifier(dir string) (bool, string) {
	script := filepath.Join(dir, "verifier_test.py")
	expected := filepath.Join(dir, "verifier", "expected.json")
	if _, err := os.Stat(script); err != nil {
		return false, "verifier_test.py missing"
	}
	body, err := os.ReadFile(expected)
	if err != nil {
		return false, "verifier expected.json missing"
	}
	var exp map[string]any
	if json.Unmarshal(body, &exp) != nil || exp["temperature_count"] == nil {
		return false, "verifier expected.json is not an independent fixture"
	}
	cmd := exec.Command("python3", "verifier_test.py")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, string(out)
	}
	return true, strings.TrimSpace(string(out))
}

func securityCheck(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "capability.json"))
	if err != nil {
		return false
	}
	if strings.Contains(string(b), `"network": true`) {
		return false
	}
	src, err := os.ReadFile(filepath.Join(dir, "src", "main.py"))
	if err != nil {
		return false
	}
	text := string(src)
	for _, bad := range []string{"socket", "urllib", "requests", "subprocess", "/etc/shadow", "os.system"} {
		if strings.Contains(text, bad) {
			return false
		}
	}
	return true
}
