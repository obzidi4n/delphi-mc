# 0015 — v1.0 scope: the technical owner-operator

**Status:** accepted · 2026-11-26

## Context

The plan accumulated features for two different customers: a self-hosting owner with a handful
of servers, and a hosting business with a fleet. Their v1 requirements diverge — placement,
drain, quotas and billing versus AI, updates, backups and "it just works". Shipping everything
means shipping neither.

## Decision

v1.0 targets the **technical owner-operator: one to ten self-hosted servers, single org**.
Everything that only pays off at larger scale — placement, drain, signed agent self-update,
Velocity networks — moves to v1.1. The architecture stays fleet-ready (agents, desired state,
`org_id`, `network_id`), so the deferral is release scheduling, not rework.

## Consequences

- v1.0 is six milestones of product instead of nine, and each milestone has a user-visible
  outcome.
- The vision's "fleet-native" and "terminal-native" claims are dated to v1.1 rather than
  silently overpromised.
- Multi-node works in v1.0 (choose the node), it just is not scheduled automatically.
- **Forbidden:** adding a feature to v1.0 because "it is nearly free" without removing
  something else; scope is the only thing that makes the milestone gate real.
