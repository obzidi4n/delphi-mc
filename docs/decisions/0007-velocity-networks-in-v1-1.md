# 0007 — Networks are first-class, delivered in v1.1

**Status:** accepted · 2026-11-26

## Context

Real Minecraft fleets usually have a proxy in front of backends, and the proxy is not just
another server: it owns forwarding secrets, online-mode settings, container networking and
player routing. Modelling a network as a tag on a server means retrofitting referential
integrity later — a schema-wide migration.

At the same time, most target users in v1.0 run one or two servers behind no proxy, and a
half-implemented proxy is worse than none: forwarding-secret drift is the classic outage.

## Decision

`networks` is a first-class entity with a nullable `network_id` on servers **from M0**, but
the implementation — container network, generated config on both sides, drain, the Velocity
plugin — ships at **v1.1 (M14)**.

## Consequences

- No schema migration when networks land; the data model already anticipates them.
- v1.0 does not promise proxy support, so no user installs it expecting drain to work.
- Drain requires a Velocity plugin (live server-list changes); the plugin is part of M14, not
  a follow-up.
- **Forbidden:** hand-editing forwarding secrets or online-mode settings; a "network" that is
  only a display grouping.
