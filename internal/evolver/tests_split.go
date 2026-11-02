package evolver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/JimiZhou/Omnoda/internal/sandbox"
)

// Builder tests are the generator's own checks. They never set verification.
// The independent verifier owns expected.json. A package that ships its own
// copy is staged aside and is not the fixture the verifier reads.

func runBuilder(dir string) (bool, string) {
	return runSandboxed(dir, "builder_test.py")
}

func runVerifier(dir string) (bool, string) {
	script := filepath.Join(dir, "verifier_test.py")
	if _, err := os.Stat(script); err != nil {
		return false, "verifier_test.py missing"
	}
	expected := filepath.Join(dir, "verifier", "expected.json")
	body, err := os.ReadFile(expected)
	if err != nil {
		return false, "independent verifier fixture missing"
	}
	var exp map[string]any
	if json.Unmarshal(body, &exp) != nil || exp["temperature_count"] == nil {
		return false, "verifier expected.json is not an independent fixture"
	}
	return runSandboxed(dir, "verifier_test.py")
}

func runSandboxed(dir, script string) (bool, string) {
	path := filepath.Join(dir, script)
	if _, err := os.Stat(path); err != nil {
		return false, script + " missing"
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return false, "python3 missing"
	}
	backend := sandbox.Select()
	if !backend.Available() {
		return false, "sandbox unavailable: " + sandbox.ErrUnavailable.Error()
	}
	req := sandbox.Untrusted(dir)
	req.Argv = []string{"python3", script}
	res := backend.Run(context.Background(), req)
	detail := strings.TrimSpace(res.Output)
	if res.Err != nil {
		if detail == "" {
			detail = res.Err.Error()
		} else {
			detail = res.Err.Error() + ": " + detail
		}
		return false, detail
	}
	if res.ExitCode != 0 {
		if detail == "" {
			detail = script + " failed"
		}
		return false, detail
	}
	return true, detail
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
