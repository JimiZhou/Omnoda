package evolver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"github.com/JimiZhou/Omnoda/internal/sandbox"
)

// sensorContract is the only trusted verification contract in v0.3.
// The model does not author this input, the expected values, or the comparison.
type sensorContract struct {
	CapabilityID string
	Log          string
	Expect       map[string]any
}

func sensorLogContract() sensorContract {
	return sensorContract{
		CapabilityID: "local.sensor-log",
		Log: "# comment must be ignored\n" +
			"OMN1|2026-11-02T01:00:00Z|10|OK\n" +
			"OMN1|2026-11-02T01:00:10Z|20|OK\n" +
			"not a record\n" +
			"OMN1|2026-11-02T01:00:20Z|99|ANOM\n",
		Expect: map[string]any{
			"samples":         float64(3),
			"anomalies":       float64(1),
			"temperature_avg": 43.0,
		},
	}
}

// verifyCandidate runs the candidate entrypoint on the system fixture.
// A generated verifier_test.py is not executed and cannot decide the result.
// Packages with no trusted contract stay unverified.
func verifyCandidate(packageDir string) (passed bool, detail string, trusted bool) {
	contract := sensorLogContract()
	raw, err := os.ReadFile(filepath.Join(packageDir, "capability.json"))
	if err != nil {
		return false, "capability.json missing", false
	}
	var mf struct {
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
		Spec struct {
			Runtime    string `json:"runtime"`
			Entrypoint string `json:"entrypoint"`
		} `json:"spec"`
	}
	if json.Unmarshal(raw, &mf) != nil || mf.Metadata.ID == "" {
		return false, "capability.json is not readable", false
	}
	if mf.Metadata.ID != contract.CapabilityID {
		return false, "no trusted contract for " + mf.Metadata.ID, false
	}
	if mf.Spec.Runtime != "python" || mf.Spec.Entrypoint == "" {
		return false, "sensor contract requires the python entrypoint", true
	}
	backend := sandbox.Select()
	if !backend.Available() {
		return false, "sandbox unavailable: " + sandbox.ErrUnavailable.Error(), true
	}
	stage, err := os.MkdirTemp("", "omnoda-verify-")
	if err != nil {
		return false, err.Error(), true
	}
	defer os.RemoveAll(stage)
	work := filepath.Join(stage, "work")
	inputs := filepath.Join(stage, "inputs")
	output := filepath.Join(stage, "output")
	for _, dir := range []string{work, inputs, output} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return false, err.Error(), true
		}
	}
	if err := copyFile(filepath.Join(packageDir, filepath.FromSlash(mf.Spec.Entrypoint)), filepath.Join(work, "main.py")); err != nil {
		return false, "entrypoint missing", true
	}
	if err := os.WriteFile(filepath.Join(inputs, "sample.log"), []byte(contract.Log), 0o600); err != nil {
		return false, err.Error(), true
	}
	res := backend.Run(context.Background(), sandbox.Request{
		Argv:     []string{"python3", "main.py", "--input", "/inputs/sample.log", "--output", "/output/result.json"},
		WorkDir:  work,
		ReadOnly: []string{filepath.Join(inputs, "sample.log")},
		Writable: []string{output},
		Timeout:  sandbox.Untrusted(work).Timeout,
		MemoryMB: 128,
		Network:  false,
	})
	if res.Err != nil || res.ExitCode != 0 {
		detail := res.Output
		if res.Err != nil {
			detail = res.Err.Error() + " " + detail
		}
		if detail == "" {
			detail = "candidate exit " + strconv.Itoa(res.ExitCode)
		}
		return false, detail, true
	}
	body, err := os.ReadFile(filepath.Join(output, "result.json"))
	if err != nil {
		return false, "candidate wrote no result.json", true
	}
	var got map[string]any
	if json.Unmarshal(body, &got) != nil {
		return false, "candidate result is not json", true
	}
	for key, want := range contract.Expect {
		if !sameNumber(got[key], want) {
			return false, "candidate " + key + " = " + stringify(got[key]) + ", trusted fixture wants " + stringify(want), true
		}
	}
	return true, "trusted sensor fixture matched", true
}

func sameNumber(got, want any) bool {
	gf, ok1 := asFloat(got)
	wf, ok2 := asFloat(want)
	if !ok1 || !ok2 {
		return false
	}
	diff := gf - wf
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.001
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func stringify(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}
