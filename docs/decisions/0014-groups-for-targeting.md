# 0014 — Groups for targeting, tags for display

**Status:** accepted · 2026-11-26

## Context

Operators say "restart the SMP servers". If the only grouping is a free-text tag, then a typo
silently targets the wrong set, schedules cannot validate their target, alerts cannot be
scoped, and AI bulk operations inherit the ambiguity.

## Decision

`server_groups` is a first-class table with referential integrity, used for targeting by
schedules, alerts, AI operations and bulk power actions. Tags remain for display and
filtering. A server may be in many groups; groups carry `org_id`.

## Consequences

- Bulk operations are auditable against a concrete member set at execution time.
- Scopes gain group forms (`group:<id>:power`), and a per-server grant never implies a group
  grant.
- Deleting a group cannot orphan a schedule: referential integrity forces an explicit retarget.
- **Forbidden:** targeting schedules or AI operations by tag matching. Tags are for humans and
  filters, not for deciding what gets restarted.
