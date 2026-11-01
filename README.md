# Omnoda

Every Device. One Mind.

Omnoda is a private control plane for the computers you already own. A node dials out over WebSocket, reports what it can run, and executes only the commands and agent harnesses its local policy allows. You describe a goal once. Omnoda selects a device, runs the step, and keeps the result.

This repository is POC v0.1. It is a single-operator system, not a hosted service.

## Architecture

- **Control plane** (`omnoda-server`): Go process, SQLite, session login, enrollment, task dispatch, workflow DAG.
- **Node** (`omnoda`): Go process on the device. Ed25519 identity. Outbound connection only. No inbound port.
- **Web console**: React PWA served by the control plane.
- **Harnesses**: Codex (`codex exec --json`), Claude Code (`claude -p --output-format stream-json`), OpenCode (`opencode run --format json`). Omnoda does not reimplement them.
- **Native agent**: OpenAI-compatible tool loop, used only when you configure a model. It returns an error when no model is configured.

```text
Browser --HTTPS--> Control plane <--WSS-- Node (Linux / macOS / Windows)
                         |
                    SQLite + DAG
```

## Quick start

```bash
go build -o bin/omnoda-server ./cmd/omnoda-server
go build -o bin/omnoda ./cmd/omnoda
cd apps/web && npm install && npm run build && cd ../..
./bin/omnoda-server -addr 127.0.0.1:8787 -public-url http://127.0.0.1:8787
```

Open http://127.0.0.1:8787. The first visit asks you to create an administrator. There is no default password.

Docker:

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

The compose file binds to `127.0.0.1`. Put Caddy or another TLS proxy in front before exposing it. See `deploy/Caddyfile.example`.

## Add a node

In the console, open **Nodes** and create an enrollment code. It expires in 10 minutes and works once.

```bash
curl -fsSL http://127.0.0.1:8787/install.sh | sh -s -- \
  --server http://127.0.0.1:8787 \
  --code ENROLLMENT_CODE
```

Windows PowerShell:

```powershell
irm http://HOST/install.ps1 | iex; Install-Omnoda -Server http://HOST -Code ENROLLMENT_CODE
```

The installer refuses to run a downloaded binary until the Ed25519 release signature checks out. The public key is compiled into `scripts/install.sh` and `scripts/install.ps1`. A SHA256 file next to the download is not treated as a trust anchor.

For local development without the signed release flow:

```bash
omnoda join --server http://127.0.0.1:8787 --code CODE --name my-box \
  --workspace "$PWD" --label role=monitor
omnoda start
```

`join` connects, registers, and exits. `start` keeps the session and reconnects with backoff.

## First remote command

On the node page, run a read-only command such as `uname -a`. The node rejects commands outside its allow list and rejects paths outside the workspace you passed to `join`.

## First workflow

In Chat, leave the planner on **Template planner** and send:

> 检查 ARM 服务器上的 Demo 服务。如果异常，让 Mac 上的 Claude Code 修复代码，然后到 Windows 工作站运行测试。

The template planner builds a diagnose → fix → test DAG. The UI shows `template`, not an LLM. Fix requires an approval bound to that step. Diagnose skips the fix branch when the health check prints `HEALTHY`.

The fixture is `fixtures/demo-service`. `app.py` is intentionally broken. Do not point this at a production service.

LLM planning is available after you save an OpenAI-compatible provider in Settings. The model output is schema-checked and cannot request `PRIVILEGED`.

## What was verified here

| Platform | Build | Runtime E2E |
|---|---|---|
| Linux amd64 | Built and tested on the development machine | Server, two and three local nodes, command streaming, policy rejection, enrollment replay, revoke |
| Linux arm64 | `GOARCH=arm64` cross-compile | Not run on ARM hardware |
| macOS amd64 / arm64 | Cross-compile only | Not run |
| Windows amd64 / arm64 | Cross-compile only | Not run. The PowerShell installer was not executed |
| Android / iOS node | Not a v0.1 target | PWA layout is responsive; not tested in Safari or Chrome on a phone |

Codex and Claude Code were detected on the development machine (`codex`, `claude` on `PATH`). A live paid prompt was not executed. OpenCode was not installed. `omnoda-fake-harness` is a named test double and is not reported as a real harness.

## Security

- Nodes authenticate with an Ed25519 challenge. Enrollment codes are stored as SHA-256 and consumed once.
- Browser sessions are HttpOnly. Secure is set when the public URL is HTTPS. State-changing requests need a CSRF token and a matching Origin.
- The node is the last authorizer. `PRIVILEGED` stays off unless the device owner sets it in node config. The API cannot turn it on.
- Model API keys are sealed with a local key file (`data/seal.key`, mode 0600). They are not written to the audit log.
- Restart marks in-flight tasks `uncertain` and does not replay them.

Read `docs/security.md` before you expose a control plane.

## Limits

See `docs/implementation-status.md` and `docs/roadmap.md`.

## License

MIT. See `LICENSE`.
