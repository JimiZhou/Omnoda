# Architecture

Omnoda v0.1 is one Go server process and one Go node process per device.

## Processes

`omnoda-server` owns SQLite, the browser API, the workflow engine, and the node hub. It does not open connections to nodes. Nodes dial `GET /api/v1/node/ws`.

`omnoda` holds an Ed25519 key in `~/.omnoda/node.key` (mode 0600). The node id is `node_` plus 16 bytes of SHA-256 of the public key. IP and hostname are labels, not identity.

## Trust boundaries

The browser can ask. The server can route. The node decides.

A planner emits a plan. `planner.Validate` rejects `PRIVILEGED` and unknown step types. The scheduler then picks an online node. The node policy checks the argv and the workspace again. A model tool call uses the same policy.

## Workflow recovery

`tasks.status` is the source of truth. On startup, rows left in `running` become `uncertain`. Queued rows that were never bound to a node return to `queued`. The engine does not automatically re-run `uncertain` steps, because the command may have had a side effect and the result frame may have been lost.

## Limits that are intentional

No Redis, Kafka, Kubernetes, or multi-replica coordination. One SQLite file. The websocket hub is in memory; after a restart the nodes reconnect and re-sync inventory, and the database still has history.
