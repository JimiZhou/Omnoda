# Security review v0.3 — generated code

## Finding

`internal/evolver/tests_split.go` previously ran `builder_test.py` and `verifier_test.py` with `exec.Command("python3")` on the host. The generated package also supplied `verifier/expected.json`, so a model could satisfy its own verifier.

## Change

Both scripts go through `sandbox.Select`. The policy is no network, cleared environment, work directory mount, timeout, and output cap. Missing backend returns an error and the evolution fails. The status is not `passed`.

Before the verifier runs, the evolver overwrites `verifier/expected.json` with the system fixture and restores the generated file afterward. Verification status `passed` requires the independent run and the static security check. Anything short of that is `unverified`. `POST /capabilities/{id}/activate` rejects `unverified`, `failed`, and `none` unless the origin is `builtin`.

## Evidence

`go test ./internal/evolver/ ./internal/sandbox/` on Linux amd64 with bwrap. `TestHostileGeneratedScriptsStayInsideSandbox` checks a host marker, an outside write, a network call, and a tight loop. `TestBwrapDeniesHostFile` and `TestBwrapDeniesWriteAndNetwork` cover the backend. `TestNoBackendRefusesGeneratedScript` skips when bwrap exists and fails closed otherwise.

## Still open

macOS and Windows have no backend here. That is a refusal, not a measurement on those operating systems. The seal for model keys remains AES-256-GCM with a local key file, as in v0.2.
