# Upgrades

**Status:** design. Panel upgrade/rollback is an M12 exit requirement; signed agent
self-update is v1.1 (M15).

## Compatibility window

Panel *N* supports agents *N-1…N*; agent *N* supports panels *N-1…N*. Capabilities are
negotiated, never inferred from version numbers, so a skew of one release is boring by
design.

Upgrade order: **panel first, then agents**, draining each node before its restart.

## Panel

1. Back up Postgres (and the config file) before any minor upgrade.
2. Replace the binary; the new process runs migrations at startup.
3. `GET /readyz` returns healthy before the reverse proxy is pointed at it.
4. Roll back by restoring the binary **and** the database snapshot — migrations are
   forward-only (ADR [0019](../decisions/0019-forward-only-migrations.md)), so a binary
   rollback without the database is unsupported.

A panel restart does not stop servers: agents keep running and reconnect. Console history
replays from the agent ring buffer; metric history and anything mid-job resume from Postgres.

## Agents

**v1.0:** manual. Stop the agent unit, replace the binary, start it. Servers keep running
during the swap (the supervisor re-attaches to existing containers on boot), and the panel
sees a brief disconnect.

**v1.1:** self-update. Two slots, health-gated, automatic rollback on failure; artifacts are
signed (ed25519) and verified against a key pinned in the binary. Rollout drains a node,
updates it, verifies `hello` + a restart-free reconciliation, then un-drains.

## Bridge plugin

The plugin jar is pinned to the support floor. Updating it is a file write plus a server
restart — never a hot reload. The agent tolerates plugin *N-1*; the UI shows "plugin update
required" when a capability is missing rather than silently degrading.

## Templates and plugin versions

Neither upgrades implicitly. Re-applying a template is a diffed job; plugin updates follow the
per-plugin policy (`pin`/`notify`/`auto-*`) with a staging boot-check where the filesystem
allows it ([updates](../architecture/updates.md)).

## What must never happen

- A schema migration that cannot be applied to the previous release's data without a human.
- An agent update that requires SSH to every node in sequence with no rollback.
- A panel upgrade that stops or restarts running Minecraft servers.
