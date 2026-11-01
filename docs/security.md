# Security

Omnoda is a remote execution system for machines you own. Pairing requires an enrollment code from a logged-in operator. There is no scan-the-LAN join path.

## Identity

- First boot creates an admin. Password hash is bcrypt. No built-in password.
- Nodes use Ed25519. The server stores the public key.
- Enrollment codes are 128-bit, shown grouped, stored as SHA-256, TTL 10 minutes, single consumer under a transaction.
- Revoke sets `status=revoked`, which later upserts cannot clear, and closes the socket.

## Execution levels

`READ_ONLY` allows a short diagnostic argv list (`uname`, `echo`, `git status`, `git diff`, and absolute programs that live inside an authorized workspace).

`WORKSPACE_WRITE` allows those plus programs located in an authorized workspace.

`PRIVILEGED` is rejected by the API and by planner validation. The node enables it only when `allow_privileged` is set in the local config by the device owner.

The executor does not start a shell. Arguments are argv. `..` paths and symlinks that resolve outside the workspace fail.

## Indirect authority

Model JSON cannot widen policy. Native tools call `policy.CheckCommand` and `runtime.SafeJoin`. Audit details that contain `token`, `password`, `secret`, or `api_key` are stored as `[redacted]`.

## Known POC limits

- The local model-key seal is XOR with a SHA-256 keystream from `seal.key`, not an OS keystore.
- The node websocket upgrader does not check browser Origin, because authentication is the signature challenge. Do not put anything else on that route.
- CSRF is a header token plus Origin check. A non-browser client that stole the cookie could still call the API. Protect the cookie with HTTPS.
- Concurrency cap is 2 tasks per node in the hub and in the daemon.
