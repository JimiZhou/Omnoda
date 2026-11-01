package evolver

import (
	"context"
	"os"
	"path/filepath"
)

// FixtureGenerator writes a known sensor-log capability for deterministic tests.
// It is not a model. Callers must not report its output as LLM-generated.
type FixtureGenerator struct{}

func (FixtureGenerator) Generate(_ context.Context, _ string, dest string) (int, error) {
	files := map[string]string{
		"capability.json":        fixtureManifest,
		"src/main.py":            fixtureSource,
		"builder_test.py":        fixtureBuilder,
		"verifier_test.py":       fixtureVerifier,
		"verifier/expected.json": fixtureExpected,
		"examples/format.md":     fixtureFormat,
		"README.md":              "Parse Omnoda sensor logs. Inputs: input_file. No network.",
	}
	for name, body := range files {
		path := filepath.Join(dest, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return 0, err
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

const fixtureManifest = `{
  "apiVersion": "omnoda.dev/v1alpha1",
  "kind": "Capability",
  "metadata": {"id": "local.sensor-log", "name": "Sensor Log", "version": "0.1.0", "description": "Parse OMN1 sensor logs into temperature, anomaly, and sample-time stats"},
  "spec": {
    "runtime": "python",
    "entrypoint": "src/main.py",
    "platforms": ["linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"],
    "inputs": {"type": "object", "properties": {"input_file": {"type": "string"}}, "required": ["input_file"]},
    "outputs": {"type": "object", "properties": {"samples": {"type": "integer"}, "temperature_avg": {"type": "number"}, "anomalies": {"type": "integer"}}},
    "permissions": {"filesystem": {"read": ["workspace"], "write": ["output"]}, "network": false},
    "limits": {"timeout_seconds": 30, "memory_mb": 128}
  }
}`

const fixtureSource = `import argparse, json, pathlib, statistics
p = argparse.ArgumentParser()
p.add_argument("--input", required=True)
p.add_argument("--output", required=True)
a = p.parse_args()
temps, anomalies, times = [], 0, []
for line in pathlib.Path(a.input).read_text().splitlines():
    line = line.strip()
    if not line or line.startswith("#") or not line.startswith("OMN1|"):
        continue
    parts = line.split("|")
    if len(parts) < 4:
        continue
    times.append(parts[1])
    temps.append(float(parts[2]))
    if parts[3] == "ANOM":
        anomalies += 1
out = {
  "samples": len(temps),
  "temperature_avg": round(statistics.fmean(temps), 3) if temps else 0,
  "anomalies": anomalies,
  "first_sample": times[0] if times else "",
  "last_sample": times[-1] if times else "",
}
pathlib.Path(a.output).write_text(json.dumps(out))
print(json.dumps(out))
`

const fixtureBuilder = `import json, pathlib, subprocess, sys, tempfile, os
sample = """# sensor
OMN1|2026-11-02T00:00:00Z|21.5|OK
OMN1|2026-11-02T00:00:01Z|30.0|ANOM
"""
root = pathlib.Path(__file__).resolve().parent
with tempfile.TemporaryDirectory() as d:
    src = pathlib.Path(d, "in.log")
    src.write_text(sample)
    out = pathlib.Path(d, "out.json")
    subprocess.check_call([sys.executable, "src/main.py", "--input", str(src), "--output", str(out)], cwd=root)
    got = json.loads(out.read_text())
    assert got["samples"] == 2
    assert got["anomalies"] == 1
print("builder ok")
`

const fixtureVerifier = `import json, pathlib, subprocess, sys, tempfile
# Independent fixture. The expected file is not produced by the builder script.
root = pathlib.Path(__file__).resolve().parent
expected = json.loads((root / "verifier" / "expected.json").read_text())
sample = (root / "verifier" / "sample.log").read_text() if (root / "verifier" / "sample.log").exists() else """OMN1|2026-11-02T01:00:00Z|10|OK
OMN1|2026-11-02T01:00:10Z|20|OK
OMN1|2026-11-02T01:00:20Z|99|ANOM
"""
with tempfile.TemporaryDirectory() as d:
    src = pathlib.Path(d, "in.log")
    src.write_text(sample)
    out = pathlib.Path(d, "out.json")
    subprocess.check_call([sys.executable, "src/main.py", "--input", str(src), "--output", str(out)], cwd=root)
    got = json.loads(out.read_text())
    assert got["samples"] == expected["samples"]
    assert got["anomalies"] == expected["anomalies"]
    assert got["temperature_avg"] == expected["temperature_avg"]
print("verifier ok")
`

const fixtureExpected = `{
  "samples": 3,
  "anomalies": 1,
  "temperature_avg": 43.0,
  "temperature_count": 3
}`

const fixtureFormat = `# OMN1 sensor log
Each record is OMN1|<RFC3339>|<celsius>|<OK|ANOM>
Lines starting with # are comments.
`
