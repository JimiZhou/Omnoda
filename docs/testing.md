# Testing

```bash
go test ./...
```

Unit tests cover enrollment single-use under concurrency, expiry, revoke, node revocation stickiness, path escape, symlink escape, planner rejection of `PRIVILEGED`, scheduler explanation, workflow skip when diagnose is healthy, and harness line normalization.

`tests/integration` starts an `httptest` server and real node goroutines:

- login, enroll, `echo`, streamed events
- `cat /etc/passwd` rejected
- revoke then task rejected
- enrollment code replay does not create a second node
- three enrolled nodes, unhealthy fixture diagnose, test command after a real file copy
- unauthenticated `/api/v1/nodes` returns 401
- fake harness binary prints a session id
- Codex / Claude / OpenCode detection logged from the live PATH

Not automated here:

- Playwright against a browser. The UI is a real client of the API; browser E2E was not run in this session.
- A live `codex exec` or `claude -p` call. That spends model quota and needs an interactive login on the device.
- macOS, Windows, and Linux arm64 installers on those operating systems.

Cross-compile check:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/omnoda-arm64 ./cmd/omnoda
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/omnoda.exe ./cmd/omnoda
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o /tmp/omnoda-darwin ./cmd/omnoda
```

A green cross-compile is not a runtime pass.
