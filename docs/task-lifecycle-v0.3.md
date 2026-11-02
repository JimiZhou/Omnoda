# Task lifecycle v0.3

## Create

`POST /api/v1/conversations/{id}/messages` classifies the goal, then writes the user message and the queued task in one transaction.

| Classification | Stored result |
|---|---|
| System info for this computer | Runs `omnoda.system-info` |
| Named capability that is not installed | `failed`, error `capability_missing` |
| Planning, explanation, or code writing | `failed`, error `no_model` or `model_required` |
| Delete, install, reboot, or format | Pending approval bound to the task. The capability is not run. |
| Anything else | `failed`, error `unsupported` |

System info is not the default success result.

## Idempotency

The key is unique for one `owner_id`. The unique index is the concurrency backstop.

- Same owner, same key, same prompt, kind, command, workspace, and harness: the original task is returned and a second run is not started.
- Same owner, same key, different body: HTTP 409 `idempotency_conflict`.
- Different owner, same key: a separate task. The other owner's id is not returned.
- A commit that the client never sees is safe to retry. The retry returns the stored task.
- A failed transaction does not leave the message without the task, or the task without the message.

A legacy task with an empty `owner_id` is not readable or cancellable by a logged-in user. In-process recovery may still see it.

## Events

`NextEventSeq` allocates the next sequence in a write transaction on `event_seq`. `task_events(task_id, seq)` is unique. `AppendEventOnce` treats a duplicate as already stored. `GET /api/v1/tasks/{id}/events?after=<seq>` returns later events for an owner of that task.

## Disconnect, cancel, restart

HTTP disconnect, browser close, and SSE close do not cancel an accepted task. The supervisor uses a process context.

User cancel is `POST /api/v1/tasks/{id}/cancel`. The status write is conditional. A later success or failure from the runner does not replace `cancelled`, `succeeded`, `failed`, `timed_out`, or `uncertain`. Repeating cancel on an already cancelled task stays cancelled.

A runner panic records `failed`. If the run row cannot be created, the task is `failed` and no success is recorded.

Process restart cannot continue an in-memory run.

- `queued` and `side_effect=0`: stays `queued` and may be scheduled again. The builtin seed path is read-only.
- `running`, or `queued` with `side_effect=1`: becomes `uncertain`. An open run row becomes `uncertain`. The task is not replayed.

`uncertain` is not a resumable agent session. Codex `--ephemeral` starts a new process. It is not described as session restore.

## Approval

An approval row stores the task id, action, and argument summary. Deciding an approval checks that the caller owns that task. A decision does not apply to a different task.
