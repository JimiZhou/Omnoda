# E2E matrix v0.3

Statuses mean what was actually observed.

- UNIT VERIFIED: `go test` on this Linux amd64 host.
- INTEGRATION VERIFIED: a real process on this host, not a fixture standing in for that process.
- DEVICE VERIFIED: the named physical machine. None of those rows are device verified in this round.
- BLOCKED: the environment cannot run the check. This is not a pass.

Host for the integration rows: Linux x86_64, bubblewrap 0.11.1, Go toolchain in this workspace. Clean seed root `/tmp/omnoda-seed-v03b`.

| Check | Status | Evidence |
|---|---|---|
| Generated script cannot run without a sandbox | UNIT VERIFIED | `TestNoBackendRefusesGeneratedScript` skips here because bwrap exists. `TestFixtureEvolutionFailClosed` covers the other branch. |
| Real bubblewrap isolation | INTEGRATION VERIFIED | `go test ./internal/evolver/ ./internal/sandbox/ -count=1` on this host. Includes host-file, write, network, and loop cases. |
| GitHub-hosted runner isolation | BLOCKED | ubuntu-latest `bwrap --unshare-all` fails with `RTM_NEWADDR: Operation not permitted`. The `sandbox` job exits 1 and prints `SANDBOX=BLOCKED`. |
| Sensor-log independent verifier | INTEGRATION VERIFIED | System fixture, not `verifier_test.py`. Wrong candidate and always-pass verifier fail. |
| Capability with no trusted contract | UNIT VERIFIED | Stored `unverified`, enable rejected. |
| Linux seed, no third-party agent required | INTEGRATION VERIFIED | Clean root, admin created, `omnoda.system-info` returned `linux/amd64`. |
| Unsupported and missing goals | INTEGRATION VERIFIED | `capability_missing`, `unsupported`. No system-info success for those goals. |
| No model configured | INTEGRATION VERIFIED | `POST /api/v1/agent/run` returned `no_model`. |
| Idempotent retry and conflict | INTEGRATION VERIFIED | Same key and body: HTTP 200, same `task_id`. Different body: HTTP 409. |
| Restart keeps conversation and capabilities | INTEGRATION VERIFIED | Second process, same root, messages and `omnoda.system-info` present. |
| HTTP disconnect does not cancel | UNIT VERIFIED | Supervisor test. Browser close was not clicked. |
| Cancel beats a late success | UNIT VERIFIED | `TestCancelBeatsALateSuccess`. |
| Restart of a running task | UNIT VERIFIED | Status `uncertain`, open run `uncertain`, queued read-only task stays queued. |
| Linux ARM64 device | BLOCKED | No ARM board. Cross compile is not device verification. |
| macOS Apple Silicon device | BLOCKED | No Mac. Expected: seed starts, node registers, generated execution returns sandbox unavailable. |
| Windows x86_64 device | BLOCKED | No Windows machine. Expected: binary starts, node registers, builtin system info works, generated execution is refused. |
| Live Codex or Claude Code continuity | BLOCKED | CLIs are installed. No prompt was sent. `--ephemeral` is not session resume. The supervisor runs builtin goals only. |
| Live model authoring a capability | BLOCKED | No provider key. Fixture generator is a test double. |
| iPhone voice | BLOCKED | Out of scope for this round. |

## Device commands

Run these on the named machine. Record the exit code. Do not treat a cross-compiled binary as a device pass.

Linux ARM64 and a second Linux amd64:

```bash
go test ./...
go test ./internal/evolver/ ./internal/sandbox/ -count=1
omnoda seed --root /tmp/omnoda-clean --port 8791
```

Expected: unit tests pass. On a host with a working `bwrap` user namespace the sandbox tests pass. Without `bwrap`, generated execution fails closed and the sandbox-dependent tests skip. Seed answers a system-info goal with `linux/<arch>` and rejects an unknown goal.

macOS Apple Silicon and Windows x86_64:

```bash
go test ./...
omnoda seed --root /tmp/omnoda-clean --port 8791
```

Expected: `sandbox.Select` is not available, so a generated package is not executed. Seed still serves the builtin system-info capability. A missing capability returns `capability_missing`.
