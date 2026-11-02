# Security review v0.3

## Trust boundary

Model-generated code, scripts, tests, and fixtures are untrusted input.

| Piece | Who owns it | Used for the verdict |
|---|---|---|
| Candidate entrypoint and `builder_test.py` | Model or fixture generator | No |
| `verifier_test.py` and generated `expected.json` | Model or fixture generator | No |
| Sensor-log input and expected counts | System, in `internal/evolver/verify.go` | Yes |
| Comparison of `result.json` | System verifier | Yes |
| Bubblewrap policy | System | Execution confinement |

The system verifier copies only the candidate entrypoint into a fresh work directory. The golden log is a read-only mount. Expected counts never enter the sandbox. A missing trusted contract does not become `VerifyPassed`.

Builder success is stored as a diagnostic bit. Activation requires `VerifyPassed`, and `SetStatus` re-hashes the package first. If the new version cannot be marked enabled, the previously enabled version stays enabled.

## Fixes in this round

- Generated tests no longer decide verification by exiting 0 or printing `success`.
- No sandbox backend: the candidate is not executed and verification fails closed.
- CI `go` job does not need bubblewrap. CI `sandbox` job fails with `SANDBOX=BLOCKED` when the user namespace probe fails.
- Task reads, events, runs, cancel, and artifacts check the owner. An empty legacy `owner_id` is not granted to a logged-in user.
- Idempotency is unique per owner. A different body with the same key is a conflict. Another user does not see the row.
- A terminal task status is not replaced by a later runner write.
- Seed does not report system info for a goal it cannot classify.

## Evidence the boundary was not lowered

- `internal/sandbox` still returns `ErrUnavailable` when bubblewrap is absent. There is no host `python3` fallback.
- macOS and Windows still have no generated-code backend.
- Enrollment codes stay hashed. Model keys stay in the existing seal. This round did not add a plaintext key path.
- The seed log from the clean-root run did not contain the administrator password.

## Residual risk

- The trusted contract covers `local.sensor-log` only. Other generated packages remain disabled.
- GitHub-hosted runners may be unable to run the isolation tests. That is recorded as blocked, and the isolation proof stays on a Linux host with a working user namespace.
- Resource checks are per task owner. The product is still a single-admin seed. A future multi-user mode needs the same checks on every new route.
- Codex `--ephemeral` and Claude print mode are not durable sessions. A live agent run was not performed.
- Static `securityCheck` is a hint. The sandbox and the system verifier are the controls that decide execution and activation.
