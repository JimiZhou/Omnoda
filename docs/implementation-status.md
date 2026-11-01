# Implementation status

Updated for v0.2 seed work. Items marked tested were exercised by `go test` on Linux amd64 unless noted.

| Area | State | Evidence | Open |
|---|---|---|---|
| AES-256-GCM seal and legacy migration | done | auth tests | |
| Node and hub websocket single writer | done | code review; race not separately timed | |
| Seed bootstrap, loopback bind, idempotent registry | tested | seed test | browser walkthrough of the new pages |
| Capability manifest, registry, enable, rollback | tested | registry tests | |
| Sandbox refuses untrusted code without bwrap | tested | sandbox test | bwrap denial of a host file runs only when bwrap is installed |
| Evolver state machine, split tests, approval | tested | evolver tests, fixture generator | real model generation not run; no API key in this environment |
| Capability export/import | implemented | packaging package | second-node import not run |
| Web capabilities and evolution pages | implemented | typecheck required | Playwright not run |
| v0.1 node, task, harness paths | kept | existing tests | |

## Not done, and not claimed

- A live model call that authors a capability. Fixture generation is labeled as a test double.
- macOS, Windows, and arm64 device runs.
- Playwright and phone browsers.

| Area | State | Evidence | Open |
|---|---|---|---|
| Monorepo, Go module, Makefile | done | builds | |
| SQLite schema | done | storage tests | |
| Admin setup and session | done | integration login | |
| Enrollment one-time code | done | concurrent consume test, replay test | |
| Node Ed25519 and outbound WS | done | integration nodes connect | |
| Heartbeat and reconnect loop | implemented | backoff code; a kill/reconnect case was not separately timed | |
| Revoke | done | integration | |
| Read-only command and event log | done | echo task | |
| Policy path and argv checks | done | unit + cat /etc/passwd | |
| Codex adapter | implemented | detect on this machine; no live exec | live prompt not run |
| Claude Code adapter | implemented | detect on this machine; no live exec | live prompt not run |
| OpenCode adapter | implemented | CLI absent | not run |
| Fake harness | test-only | binary test | must not be reported as a real agent |
| Native agent | implemented | unconfigured call errors | no model key in this environment, so no live loop |
| Template planner and DAG | done | unit skip-when-healthy; API workflow returns terminal status | arm64 selector does not match this amd64 node, so the canned demo workflow fails placement here |
| Three local nodes | done | diagnose UNHEALTHY and tester passed=1 | nodes are Linux processes, not ARM, macOS, and Windows |
| Approval API | implemented | UI calls it; no browser click test | |
| Web UI | implemented | typecheck/build required before release | Playwright not run |
| PWA manifest | implemented | vite config | phone install not verified |
| Install scripts and signing | implemented | openssl verified a detached Ed25519 signature | scripts not executed on macOS or Windows |
| Docker | file present | image not built in this session | |
| CI | workflow file | not yet run on GitHub | |

## Not done, and not claimed

- Real ARM board, Mac, and Windows workstation in one workflow.
- Live third-party harness prompt.
- Playwright.
- Phone browsers.
