# Omnoda agent notes

Omnoda is a private device network. The node is the last authorizer. Do not add a path that runs a shell string, disables the harness sandbox, or stores a model key in the clear.

## Layout

- `cmd/omnoda` node CLI
- `cmd/omnoda-server` control plane
- `internal/controlplane` HTTP and node websocket
- `internal/node` outbound session and executors
- `internal/adapters` Codex, Claude Code, OpenCode
- `internal/workflow` persisted DAG
- `internal/planner` template and optional LLM planner
- `apps/web` React PWA
- `fixtures/demo-service` intentionally broken demo

## Commands

```bash
go test ./...
cd apps/web && npm run build
```

## Rules

- Do not mark a platform verified unless a test log from that OS is in the change.
- Do not treat `omnoda-fake-harness` as Codex, Claude Code, or OpenCode.
- Do not replay a task whose status is `uncertain`.
- Enrollment codes are single-use. Store only the hash.
