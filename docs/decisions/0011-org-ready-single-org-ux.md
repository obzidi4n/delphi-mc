# 0011 — Org-ready schema, single-org UX

**Status:** accepted · 2026-11-26

## Context

Multi-tenancy is cheap to add to a schema at the start and expensive to retrofit: every
query, every authorization check, every unique constraint. But multi-tenant UX (org switcher,
invites, per-org billing) is expensive now and might never be used.

## Decision

`org_id` on every **owned root** (servers, nodes, groups, templates, networks, schedules,
alerts, conversations), with children deriving it through foreign keys. v1.0 ships a
single-org experience: no switcher, no org management UI, one org created at install.

## Consequences

- Adding a second org later is a UX project, not a migration.
- Composite foreign keys enforce ownership where it matters; the rule is written down in
  [`../reference/data-model.md`](../reference/data-model.md) so it does not decay.
- **Forbidden:** a query that scopes by id without an org check, and any feature that assumes
  "the world is one org" in storage rather than in the UI.
