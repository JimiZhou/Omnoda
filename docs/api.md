# HTTP API

Version prefix: `/api/v1`. Machine-readable list: `protocol/openapi.yaml`.

Browser writes send `X-CSRF-Token` and an `Origin` that matches the host. The session cookie is `omnoda_session`, HttpOnly, SameSite=Lax.

| Method | Path | Purpose |
|---|---|---|
| POST | /api/v1/setup | Once, create the first admin |
| POST | /api/v1/auth/login | Session |
| POST | /api/v1/auth/logout | |
| GET | /api/v1/auth/me | |
| GET | /api/v1/nodes | |
| GET | /api/v1/nodes/{id} | |
| DELETE | /api/v1/nodes/{id} | Revoke and drop the socket |
| GET | /api/v1/nodes/{id}/capabilities | |
| POST | /api/v1/nodes/enrollment-codes | Returns the plaintext code once |
| POST | /api/v1/tasks | Dispatch |
| GET | /api/v1/tasks/{id} | |
| GET | /api/v1/tasks/{id}/events | |
| POST | /api/v1/tasks/{id}/cancel | |
| POST | /api/v1/workflows | Body: goal, planner=`template`\|`llm`\|`auto` |
| GET | /api/v1/workflows/{id} | Plan, steps, report |
| POST | /api/v1/workflows/{id}/cancel | |
| POST | /api/v1/approvals/{id}/approve | |
| POST | /api/v1/approvals/{id}/reject | |
| GET | /api/v1/artifacts/{id} | Metadata |
| GET | /api/v1/audit | |
| GET | /api/v1/events | Server-sent events for live task frames |
| PUT | /api/v1/settings/model | Sealed API key |

Errors are `{"code","message"}`.
