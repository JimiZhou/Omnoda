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

## Generated code

Model output, imported packages, and `builder_test.py` / `verifier_test.py` run only through `internal/sandbox`. On Linux the backend is bubblewrap with `--unshare-all`, `--clearenv`, no network, and a timeout. A builder test is a self-test. The independent expected fixture is owned by the evolver and written over any generated `verifier/expected.json` before verification. A candidate that cannot be isolated is stored as `unverified` and the activate API rejects it.

macOS and Windows have no backend in this release. `sandbox.Select` returns none, and packaged capability execution returns `untrusted capability refused: sandbox unavailable`. The task can be sent to an enrolled Linux node that has bubblewrap. It is not executed with a host `python3`.

If `bwrap` is absent on Linux, the same refusal applies. Tests skip the live isolation proof and still assert the fail-closed result. A skip is not a pass.

## Known POC limits

- The local model-key seal is XOR with a SHA-256 keystream from `seal.key`, not an OS keystore.
- The node websocket upgrader does not check browser Origin, because authentication is the signature challenge. Do not put anything else on that route.
- CSRF is a header token plus Origin check. A non-browser client that stole the cookie could still call the API. Protect the cookie with HTTPS.
- Concurrency cap is 2 tasks per node in the hub and in the daemon.
