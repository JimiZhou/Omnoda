# E2E matrix v0.3

Recorded against Linux amd64 (`uname` host in the gate report). Real ARM, macOS, Windows, and iPhone runs are not in this workspace.

| ID | Result | Evidence |
|---|---|---|
| T01 | PASS on this Linux host with bwrap | `go test ./internal/evolver/ ./internal/sandbox/` |
| T02 | UNVERIFIED on macOS/Windows hardware. Code path refuses when `Select` is none | `sandbox.Select`, executor `ErrSandbox` |
| T03 | BLOCKED. No clean desktop image in this session | seed bootstrap unit test only |
| T04 | PARTIAL. Missing model returns `no_model`. A wrong live key was not called | `agentRun` |
| T05 | BLOCKED. No provider credential | |
| T06 | PARTIAL. Supervisor test keeps the task after the caller returns. No phone | `internal/supervisor` |
| T07 | PARTIAL. Idempotency and event sequence covered in unit tests | |
| T08 | PASS in unit test for restart classification | `TestRestartMarksSideEffectUncertain` |
| T09 | unchanged approval row binding from v0.1 | |
| T10 | BLOCKED. One Linux machine | |
| T11 | BLOCKED. No live Codex/Claude prompt in this session | |
| T12 | PARTIAL. Fixture evolution is labeled fixture. Live generation not run | |
| T13 | BLOCKED. No iPhone | |
| T14 | covered by existing enrollment tests on this host | |
| T15 | see gate report for the command output | |

Do not read a unit test as a three-device or phone pass.
