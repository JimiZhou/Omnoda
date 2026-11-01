# Harness adapters

Interface: `adapters.Adapter` with `ID`, `Detect`, and `Start`.

`Start` emits normalized events (`run.started`, `message.delta`, `tool.started`, `tool.completed`, `run.completed`, `run.failed`, `run.cancelled`) and keeps a redacted copy of the source line on the event. The child is started in its own process group and killed on cancel or timeout.

## Codex

Detection: `codex --version`.

Run: `codex exec --json --color never --skip-git-repo-check --sandbox workspace-write --ephemeral --cd <workspace> <prompt>`.

`--dangerously-bypass-approvals-and-sandbox` is not used.

## Claude Code

Detection: `claude --version`.

Run: `claude -p <prompt> --output-format stream-json --verbose --permission-mode default --add-dir <workspace>` with the process working directory set to the workspace.

`--dangerously-skip-permissions` is not used. Permission mode is `default`, so Claude Code still applies its own approval rules.

## OpenCode

Detection: `opencode --version`.

Run: `opencode run --format json <prompt>`.

No auto-approve flag is passed. This CLI was not installed in the development environment, so the adapter is untested against a live binary.

## Fake

`cmd/omnoda-fake-harness` prints JSON lines (`started`, `delta`, `completed`). It exists so orchestration tests have an explicit double. DetectAll skips it. The UI does not list it as Codex or Claude.

## Execution record

Each run stores `node_id`, `harness_id`, `task_id`, workspace, prompt, timeout, level, and status on the task row. Harness credentials are not read and not sent to the control plane.

A harness task at `READ_ONLY` is rejected. Workspace write still has to point at an authorized workspace.
