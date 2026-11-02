# Task lifecycle v0.3

## Create

`POST /api/v1/conversations/{id}/messages` writes the user message and a queued task. The response contains `conversation_id` and `task_id`. Repeating the same `idempotency_key` returns the original task and does not start another run.

## Execute

The supervisor uses a process context, not the HTTP request context. Disconnecting the browser or the SSE stream leaves the task running. `POST /api/v1/tasks/{id}/cancel` is the user cancel.

## Reconnect

`GET /api/v1/tasks/{id}/events?after=<seq>` returns events newer than that sequence. A duplicate sequence is ignored by `AppendEventOnce`.

## Restart

`RecoverInflight` leaves a queued task with `side_effect=0` queued. A running task, or a queued task already marked as a side effect, becomes `uncertain`. Uncertain tasks are not replayed.

A CLI that has no stable session id cannot resume the old process. The control plane may start a new run and must label it as a new run. Codex `--ephemeral` sessions are not treated as resumable.

## Approval

An approval row names the task, node, action, argument summary, and expiry. Deciding that id does not decide a different task.
