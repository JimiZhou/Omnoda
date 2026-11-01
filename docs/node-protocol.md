# Node protocol

Wire version: 1. Transport: WebSocket at `/api/v1/node/ws`.

Frames are JSON envelopes:

```json
{"v":1,"type":"node.hello","timestamp":"...","payload":{}}
```

## Handshake

1. Node sends `node.hello` with `node_id`, base64 public key, inventory, and workspaces.
2. Server checks `node_id == node_` + SHA-256(public key)[:16].
3. Server sends `node.challenge` with a nonce.
4. Node sends `node.challenge_response` with the Ed25519 signature and, on first contact, `enrollment_code`.
5. Server consumes the code or matches the stored public key. Revoked nodes are rejected.
6. Server sends `node.hello_ack`.

## After the handshake

| Type | Direction | Meaning |
|---|---|---|
| node.heartbeat | node → server | every 20s |
| node.heartbeat_ack | server → node | |
| task.assign | server → node | one TaskSpec |
| task.event | node → server | normalized event |
| task.result | node → server | terminal status |
| task.cancel | server → node | cancel that task id |
| node.revoked | server → node | server drops the socket |

Reconnect uses exponential backoff capped at 60s plus jitter. The enrollment code is cleared after a successful hello so a reconnect cannot replay it.

Duplicate task creation from the browser uses `idempotency_key`. The node does not re-run a task id it has already finished in that process; the server status remains whatever was stored.

Event bodies are capped by the executor (256 KiB of command output, 8 KiB per harness line stored in the raw field, and raw lines containing token-like text are replaced with `[redacted harness line]`).
