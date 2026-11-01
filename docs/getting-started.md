# Getting started

Requirements: Go 1.26, Node.js 22 for the UI build. The server binary serves `apps/web/dist`.

```bash
make server node web
./bin/omnoda-server -addr 127.0.0.1:8787 -public-url http://127.0.0.1:8787
```

1. Open the URL and create the admin user.
2. Nodes → create an enrollment code.
3. On the device: `omnoda join --server URL --code CODE --workspace /path --label role=monitor`
4. `omnoda start`
5. The node card shows `online` only while the websocket is attached.

`omnoda doctor` prints identity, key permissions, server URL, harness detection, and workspace checks.

Native agent configuration is per node, via `OMNODA_MODEL_BASE_URL`, `OMNODA_MODEL`, and `OMNODA_MODEL_API_KEY`. If those are empty, a native task fails with `native agent model provider is not configured`.
