# 0016 — Support floor: Paper 1.20.5+ / Java 21

**Status:** accepted · 2026-11-26

## Context

The bridge plugin is the product's differentiator: structured chat, players, TPS, moderation
and crash context instead of log scraping. Supporting every Paper version from 1.8 means a
plugin compatibility matrix, Java 8/17 containers, and two decades of API drift. Every
supported variant multiplies test surface for a shrinking population of servers.

## Decision

v1.0 supports **Paper 1.20.5+ on Java 21** (and Java 25 where Paper supports it). Images:
`delphi/java:21-temurin`, `:25`. Older servers are out of scope, stated plainly, with no
code branches for them.

## Consequences

- One plugin codebase, two JDK images, and a modern Paper API.
- Plugin authors' newer releases assume modern Paper anyway, so auto-update is safer.
- Operators on 1.16-era servers cannot use the product. This is a deliberate, stated limit,
  not a bug; capability flags leave room to widen later without architectural change.
- **Forbidden:** runtime version branches for pre-1.20.5, and silently lowering the floor
  when a user complains.
