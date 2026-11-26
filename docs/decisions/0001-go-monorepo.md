# 0001 — Go monorepo, one binary

**Status:** accepted · 2026-11-26

## Context

The hard parts of this product are systems work: process supervision, cgroup accounting,
reflink snapshots, log tailing, socket handling. The panel, agent, CLI and TUI also need to
ship together without a matrix of runtime dependencies. A Minecraft panel is installed by
people who want one command and one service, not a Node runtime and a package manager.

## Decision

Everything is Go: panel, agent, CLI (and TUI at v1.1) in one module, built as one binary with
subcommands. The SPA is embedded with `embed.FS`. Postgres is the only external service.

## Consequences

- Install is one binary plus Postgres; no broker, no runtime, no asset pipeline at deploy.
- Shared types and the capability layer are one import away, so the API cannot drift between
  clients by accident.
- The agent binary carries the web assets (~2 MB). Acceptable; the docs do not claim a
  bare-metal agent.
- Frontend work still happens in TypeScript (React/Vite), so the monorepo has two toolchains
  and needs one build script that hides that from contributors.
