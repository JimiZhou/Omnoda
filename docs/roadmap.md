# Roadmap

v0.1 is the private single-server POC in this repository. The items below are not substitutes for gaps listed in `implementation-status.md`.

## v0.2 candidates

- OS keystore for the model key instead of the local seal file.
- Git-ref transfer between the fix node and the test node, with the fixture repository as the only default payload.
- A recorded live run of `codex exec` and `claude -p` on a machine that already has those CLIs logged in.
- Playwright coverage of login, enrollment display, task log, and approval.
- Real install runs on macOS and Windows hardware.
- Linger / LaunchDaemon documentation samples that an admin opts into.
- Per-workspace write locks so two write tasks cannot share a directory.
