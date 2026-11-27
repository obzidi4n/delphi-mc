# Upgrades

**Status:** panel upgrade/rollback manual (M12); signed agent self-update implemented at M15
with a one-slot rollback, no automatic post-update health rollback yet.

## Compatibility window

Panel *N* supports agents *N-1…N*; agent *N* supports panels *N-1…N*. Capabilities are
negotiated, never inferred from version numbers, so a skew of one release is boring by
design.

Upgrade order: **panel first, then agents**, draining each node before its restart.

## Panel

1. Back up Postgres (and the config file) before any minor upgrade.
2. Verify and swap the binary: `delphi upgrade --binary ./delphi-new`. The candidate must
   answer `version`; the current binary is copied to `<exe>.rollback` and the new one is
   installed atomically. The running process is unaffected.
3. Restart the panel unit; migrations run through `delphi init --config` (or the deploy
   procedure), and `GET /readyz` must be healthy before the proxy sends traffic.
4. Roll back the binary with `delphi rollback`; restore the database snapshot too — migrations
   are forward-only (ADR [0019](../decisions/0019-forward-only-migrations.md)), so a binary
   rollback without the database is unsupported.

There is no automatic health check or automatic rollback in v1.0: the operator restarts after
verification. A failed new binary leaves the previous file at `<exe>.rollback`.

A panel restart does not stop servers: agents keep running and reconnect. Console history
replays from the agent ring buffer; metric history and anything mid-job resume from Postgres.
The fleet page shows a recent-restart/degraded banner and the node table shows last-seen times,
so the outage is explained rather than silently forgotten.

## Agents

`delphi fleet update --binary ./delphi-agent --key seed.b64 --panel … --token …` hashes and
signs the binary (ed25519 over the sha256 digest), uploads it, and sends an `agent.update`
frame to every healthy node. Each agent downloads it over its node certificate, verifies the
signature against `update_public_key`, runs `<candidate> version`, copies its current binary
to `<exe>.rollback`, and `exec`s the new binary in place. Servers keep running; the agent
reports the new version on reconnect within seconds.

- The signing seed must be handled out of band; agents trust the configured public key. Pin
  that key at build time before production (open gap).
- Rollback is manual: restore `<exe>.rollback` and restart. There is no automatic health-check
  rollback or drain-before-update orchestration yet.
- Drain a node first when its servers may restart: `POST /api/v1/nodes/<id>/drain`; undrain
  with `?draining=false`. Drain affects placement only, not running servers.

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
