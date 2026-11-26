# 0019 — Forward-only migrations

**Status:** accepted · 2026-11-26

## Context

Panel upgrades are where self-hosted products break: a migration runs, the new binary fails,
and there is no way back. Down-migrations are rarely exercised, and a schema that supports
two versions doubles every query's test surface.

## Decision

Migrations are **forward-only**. Rollback is: stop the panel, restore the Postgres snapshot
taken before the upgrade, start the previous binary. Atlas migrations are expand-first
(additive before destructive) so a running panel survives the window between migration and
binary swap. The panel refuses to start when the schema is newer than the binary.

## Consequences

- No `down` migration files to write, test or get wrong.
- Upgrade procedure always includes a database snapshot; the estimator's "one command" claim
  does not extend to a no-backup upgrade.
- Data written after the upgrade is lost on rollback; the UI states the snapshot time.
- CI applies the full migration set to a copy of the previous release's schema on every PR —
  the only way forward-only stays honest.
- **Forbidden:** a destructive migration in the same release as the feature that needs it;
  use expand/contract across two releases.
