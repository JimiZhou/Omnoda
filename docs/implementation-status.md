# Implementation status

Updated for continuity hardening on `feat/omnoda-continuity-v0.3`. "Tested" means `go test` on Linux amd64 unless a row says integration.

| Area | State | Evidence | Open |
|---|---|---|---|
| System verifier for sensor-log | INTEGRATION VERIFIED | evolver tests with bwrap | Other capability ids stay unverified |
| Sandbox fail-closed | UNIT VERIFIED | sandbox tests | GitHub-hosted user namespace may be BLOCKED |
| Atomic message and task create | UNIT VERIFIED | storage tests, race test | |
| Owner checks on task, events, runs, cancel, artifacts | IMPLEMENTED and UNIT VERIFIED | `OwnsTask`, continuity tests | New routes must call the same helper |
| Seed intent routing | INTEGRATION VERIFIED | clean-root seed on Linux amd64 | Model planning still uses the existing agent path |
| Supervisor cancel and restart | UNIT VERIFIED | supervisor tests | Browser disconnect not clicked |
| Web goal, task, refresh, error text | IMPLEMENTED | typecheck and production build | No Playwright session |
| Codex and Claude detection | IMPLEMENTED | CLIs present on this host | Live prompt BLOCKED. `--ephemeral` is not resume |
| macOS, Windows, ARM64 devices | BLOCKED | no hardware in this session | Commands are in `docs/e2e-v0.3.md` |
| Live model capability authoring | BLOCKED | no provider key | Fixture generator is a test double |

## Not done, and not claimed

- Device verification on ARM64, macOS, or Windows.
- A live Codex or Claude Code task that survives client disconnect.
- A live model call.
- Phone browsers and voice.
- Playwright.

v0.1 enrollment, node websocket, and workflow behavior stay covered by the existing tests that passed in `go test ./...`.
