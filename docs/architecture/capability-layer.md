# Capability layer

**Status:** current design · implementation target M3a. ADR
[0004](../decisions/0004-capability-layer-as-single-mutation-surface.md).

`internal/capability` is the **only** way anything mutates the world. HTTP handlers, the CLI,
and AI tools are thin adapters over it. Authorization, validation, audit and idempotency live
in one place.

## Shape

```go
type Principal struct {
    UserID  string
    OrgID   string
    Scopes  []Scope        // e.g. server:web-1:power
    Via     string         // "ui" | "cli" | "api" | "ai:<conversation-id>"
    Model   string         // set when Via is "ai:…" — model id, never the provider key
}

type Servers interface {
    List(ctx context.Context, p Principal, f Filter) ([]Server, error)
    Get(ctx context.Context, p Principal, id string) (Server, error)
    Power(ctx context.Context, p Principal, id string, a PowerAction) (Job, error)
    Command(ctx context.Context, p Principal, id, cmd string) (Job, error)
    ...
}
```

`List` and `Get` take the same `Principal` as mutations: reads are authorized too, and
scoping leaks through omission are a bug class worth closing once.

## Rules

- **Every method takes a `Principal` and re-checks authorization.** Defense in depth: the AI
  tool layer filters tools by scope, and the capability layer checks again.
- **Every mutating method writes an audit record** including `Via` and, for AI actions,
  `Model` — so an action is attributable to a user, a conversation, and a model.
- **Console input is a capability call.** `Servers.Command` — never a raw write on the
  console socket — so UI, CLI and AI get the same RBAC, audit and approval path.
- **Long-running operations return a `Job`, never block.**
- **Destructive operations are declarative**: they describe a plan before executing.
- **Deleting a server is two-step**: export first (config bundle + snapshot), typed
  confirmation second, then a job stops the container and removes the directory — retained
  seven days unless purged. Nothing is deleted in one click.

## Jobs and idempotency

Work that must happen once (backup now, stage-and-boot-check, run a command) is a row in
`jobs` with `idempotency_key`, `attempts`, `lease_until`, `priority`.

- Agents claim over the agent WebSocket (`claim` frame); the panel answers from
  `SELECT … FOR UPDATE SKIP LOCKED` and the lease is renewed on the same connection.
- `idempotency_key = hash(kind, target, intent)`: a repeated request inside the window
  returns the existing job rather than a second one.
- **Delivery is at-least-once.** A lease that expires re-queues the job, so it can execute a
  second time. `attempts` doubles as a fencing token: the agent rejects a claim whose attempt
  number is not higher than the last one it saw, and a job already recorded `done` for that
  key is acknowledged, not repeated.
- Handlers must therefore be idempotent. `power` (desired state), `write_file` (same content,
  same hash) and `backup` (dedupe on target + minute) are; `run_command` is **not** and is
  marked non-replayable, so an expired lease surfaces as an error rather than a second
  command.

## Scope grammar

`<resource>:<id>:<verb>`. This table is the vocabulary; the AI catalog
([`../reference/ai-tools.md`](../reference/ai-tools.md)) may only use verbs defined here.

| Resource | Verbs |
|---|---|
| `server` | `read`, `power`, `console`, `files`, `logs`, `moderate`, `backup`, `update` |
| `group` | `read`, `members`, `power`, `backup`, `update` |
| `network` | `read`, `power`, `update` (v1.1) |
| `node` | `read`, `admin` |
| `org` | `read`, `admin`, `ai`, `server:create` |
| `backup` | `read`, `delete` (stored backup sets) |
| `artifact` | `read`, `write` |

`:logs` implies `:read` on the same resource; `:read` never implies `:logs`. Anything that
returns raw log lines or crash artifacts requires `:logs`. Restoring a backup currently
requires `server:<id>:update`; a dedicated `:restore` verb is deferred.

Examples: `server:web-1:power`, `group:smp:update`, `org:default:ai`,
`org:default:server:create`.

Group and org scopes exist because bulk operations target groups (ADR
[0014](../decisions/0014-groups-for-targeting.md)); a per-server grant must never silently
imply a group grant. Adding a verb requires updating this table and the AI catalog in the same
change.

## What this buys

- The AI cannot exceed the requesting user's rights, by construction.
- One audit format, one idempotency story, one place to add a precondition.
- The CLI is generated from the same contract ([wire protocol](../reference/wire-protocol.md)),
  so a capability that is missing from the public API fails a build, not a user.
