# AI tool catalog

**Status:** M6 subset implemented; the rest is the v1.0 target. At M6 every tool is
read-only and no action tool is registered. This table is the **authorization contract**: every
tool row names its scope and whether it is read-only. A tool that is not in this table must
not be registered.

The registry is capability-gated: a tool appears only when every capability it needs exists
at the panel's and node's versions. Ten tools at v1.0 — nine by M7, `write_file` at M8 — and
fifteen is the ceiling.

## Read-only (M6, implemented)

| Tool | Purpose | Scope | Inputs |
|---|---|---|---|
| `search_docs(query, limit)` | Postgres FTS over the embedded docs corpus; returns ranked sections and paths | authenticated | `query`, `limit` (≤10) |
| `list_servers()` | Server inventory: name, id, state, port, node, base | `org:<id>:read` | — |
| `get_server(server)` | One server by name or id: state, template, port, node, last error | `org:<id>:read` + server read | `server` |
| `get_server_health(server)` | Live TPS, player count, CPU and memory (bridge + metrics) | `org:<id>:read` + server read | `server` |
| `get_console_tail(server, lines)` | Recent console lines for diagnosis | `org:<id>:read` + server read | `server`, `lines` (≤200) |

Citations come back with each result (`doc` with the path, `server` with the id) and are shown
next to the answer. The capability layer re-checks scopes on every call; the registry hides
everything but `search_docs` from a principal without org read.

## Write tools (M7, approval-gated)

| Tool | Purpose | Scope | Inputs |
|---|---|---|---|
| `create_server(template, name, node, …)` | Propose creating a server from a template | `server:create` + template read | template, name, node, optional memory/port/desired_state |
| `power(action, server \| group)` | Propose start/stop/restart/kill; a group expands to member servers | `server:<id>:power` per target | action, server or group |
| `run_command(server, command)` | Propose a console command through the bridge | `server:<id>:moderate` | server, command (≤512) |
| `moderate_player(action, server, player, reason)` | Propose kick/ban through the bridge | `server:<id>:moderate` | action, server, player, reason |

Calling a write tool creates a pending approval, not a mutation. The user approves or rejects
the card; approval executes as that user with their current scopes, then the model resumes
with the results. Every plan and execution is stored (`ai_approvals`) and audited.

Planned later (not registered yet): `diagnose`, `search_logs`, `inspect`, `write_file`.

## Mutating (always an approval card)

| Tool | Purpose | Scope | Unlocks |
|---|---|---|---|
| `create_server(template, name, overrides[, group])` | Template instantiation; lands in `ungrouped` unless a group is given (membership also needs `group:<id>:members`) | `org:<id>:server:create` | M7 |
| `power(server, action)` | start / stop / restart / kill | `server:<id>:power` | M7 |
| `run_command(server, cmd)` | Console through `capability.Servers.Command` | `server:<id>:console` | M7 |
| `moderate_player(server, player, action)` | kick / ban / op / deop / whitelist | `server:<id>:moderate` | M7 |
| `write_file(server, path, content)` | Returns a diff for approval | `server:<id>:files` | M8 |

`moderate_player` ships before `create_schedule` deliberately: banning a griefer is the most
common operator request, and the capability is a bridge/console call the alpha already paid
for.

## Later, in order, when evals fail without them

| Tool | Unlocks with |
|---|---|
| `plugins(server)` | M9 (plugin manager) |
| `get_metrics(server, range)` | series demand beyond `diagnose` |
| `update_server(server, set)` | M9 (update policies + boot-check) |
| `backup(server, action)` | M10 (backups) |
| `create_schedule(spec)` | M11 (automation) |

When `backup` unlocks: creating takes `server:<id>:backup`; restoring takes
`server:<id>:restore` **and** typed confirmation, because it is destructive.

Not tools, deliberately: audit viewing is the M12 audit viewer, maintenance windows are
managed in the UI, and database provisioning waits for v1.1 — each would need a new tool
budget. Ten at v1.0 plus five here is the fifteen ceiling.

## Rules

- Read-only tools execute without approval but are still audited (rate-limited, truncated).
- Mutating tools never execute inline: they return an `approval_required` action
  ([ai](../architecture/ai.md#approvals-two-phase-mutations)).
- Handlers call `capability` with the requesting `Principal`; a tool never has its own
  privilege.
- Every tool result is redacted before it is stored or sent to a cloud provider.
- Destructive verbs and multi-server targets require typed confirmation in addition to the
  card.
