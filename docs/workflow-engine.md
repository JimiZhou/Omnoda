# Workflow engine

A plan is a list of steps with dependencies, a selector, a condition, a retry class, and an approval policy.

Statuses: `pending`, `ready`, `waiting_approval`, `running`, `succeeded`, `failed`, `skipped`, `cancelled`, `timed_out`, `uncertain`.

Ready steps with satisfied dependencies run in parallel. A failed dependency skips the dependents. The condition `diagnose.unhealthy` reads the diagnose output. The word `UNHEALTHY` or a non-zero diagnose exit continues the fix branch. `HEALTHY` skips it.

Retry classes:

- `safe`: may run again up to `max_attempts` (capped at 2 by validation).
- `confirm`: one budgeted retry is recorded in the audit log.
- `never`: a failed side-effecting step stays failed. Harness fixes use this.

Approvals are rows bound to workflow, step, node, action, and arguments, with a 30 minute expiry. Approving one step does not authorize later steps.

The scheduler explains the choice, for example: selected Mac-mini because it is online, has claude-code, role=developer, and has an authorized workspace.

The template planner is deterministic and labeled `template`. The LLM planner calls `/chat/completions`, then `Validate`. If the provider errors, the API returns that error. It does not silently fall back and claim the model planned the work. `planner=auto` uses the template when no provider is configured.

Server restart: running workflows are marked `interrupted`. Running steps become `uncertain` and are not replayed.
