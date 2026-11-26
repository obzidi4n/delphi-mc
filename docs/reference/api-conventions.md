# API conventions

**Status:** design · enforced from M3a. The wire shape itself lives in
`internal/api/openapi.yaml` (spec-first; see
[`../contributing/conventions.md`](../contributing/conventions.md)); this page records the
rules the spec must follow.

## Base and versioning

- `/api/v1` under the configured base path (default `/delphi/api/v1`).
- Within v1: additive changes only. A breaking change is `/api/v2`, and a v1 client keeps
  working until it is retired.
- Agent protocol versioning is separate ([wire protocol](wire-protocol.md)); capability flags,
  not API versions, gate features.

## Auth

| Caller | Credential |
|---|---|
| Browser | `delphi_session` cookie (httpOnly, SameSite=Lax) + CSRF token |
| CLI / SDK | `Authorization: Bearer <token>` |
| Agent | mTLS node certificate (agent endpoints only) |

A token is scoped like any principal, hashed at rest, and revocable. The AI uses the
requesting user's credential, never its own.

## Requests

- JSON only; `Content-Type: application/json`. Unknown fields are rejected, not ignored.
- Timestamps are RFC 3339 in UTC; schedules carry their own IANA timezone.
- Every list endpoint is paginated and cursor-based:

  ```
  GET /servers?limit=50&after=<cursor>
  → { "items": [...], "next_cursor": "…" | null }
  ```

  Default 50, maximum 200, stable total ordering (never offset pagination).
- Bulk operations name their targets explicitly: a group id or an id list. There is no
  implicit "all".

## Mutations

- Every mutation returns `202 Accepted` with `Location: /jobs/{id}` when it is long-running;
  handlers never block on a container operation.
- `Idempotency-Key` is accepted on all mutating POSTs and has the same semantics as the
  capability layer's key. A replay returns the original job.
- Preconditions use `If-Match` with the resource revision (`desired_revision`, file
  `revision`, approval precondition hash). A stale revision returns `409` with the current
  representation; it is never applied silently.
- Destructive endpoints require `?confirm=<resource-name>` on top of authorization, mirroring
  the UI's typed confirmation.

## Errors

RFC 9457 problem details:

```json
{ "type": "https://delphi.dev/errors/precondition-failed",
  "title": "Revision mismatch",
  "status": 409,
  "detail": "server web-1 changed since the approval was created",
  "instance": "/api/v1/servers/web-1/power",
  "code": "precondition_failed" }
```

No stack traces, no internal identifiers, no secrets. `code` is stable and machine-readable;
`detail` is for humans.

## Rate limits and quotas

- Per-token request limits return `429` with `Retry-After`.
- AI endpoints additionally enforce the org's daily token cap; exceeding it is `429` with
  `code: ai_budget_exhausted`, not a silent truncation.
- WebSocket topics are limited per subscriber ([clients](../architecture/clients.md)).

## What is not in the API

- No endpoint that returns a secret except the audited reveal.
- No endpoint that mutates without a capability call behind it.
- No endpoint whose behavior depends on the requesting client — the TUI and CLI are ordinary
  consumers by rule (ADR [0008](../decisions/0008-native-tui-as-api-client.md)).
