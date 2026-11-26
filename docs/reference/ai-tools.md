# AI tool catalog

**Status:** design · v1.0 subset at M6–M8. This table is the **authorization contract**: every
tool row names its scope and whether it is read-only. A tool that is not in this table must
not be registered.

The registry is capability-gated: a tool appears only when every capability it needs exists
at the panel's and node's versions. Ten tools at v1.0 — nine by M7, `write_file` at M8 — and
fifteen is the ceiling.

## Read-only (no approval)

| Tool | Purpose | Scope | Unlocks |
|---|---|---|---|
| `list_servers(filter)` | Fleet inventory + summary | `org:<id>:read` | M6 |
| `inspect(target)` | Entity card: state, version, plugins, players, TPS, `last_error` (one derived line, not log content) | `server:<id>:read` | M6 |
| `diagnose(server)` | Composite: metrics + log excerpts + plugin list + TPS + crash artifacts | `server:<id>:logs` | M6 |
| `search_logs(server, query, range)` | Agent-side ripgrep over log files | `server:<id>:logs` | M6 |
| `search_docs(query)` | Postgres FTS over an embedded, attributed Delphi + Paper snapshot | `org:<id>:read` | M6 |

`diagnose` exists so the model rarely needs primitives; when it does, they are added on
evidence, not symmetry.

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
