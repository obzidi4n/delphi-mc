# 0004 — Capability layer as the single mutation surface

**Status:** accepted · 2026-11-26

## Context

Three clients (UI, CLI, AI) mutate the same state. If each calls the agent or the database
directly, authorization, audit and idempotency get reimplemented three times and diverge — and
the AI, which must never exceed the user's rights, becomes impossible to prove safe.

## Decision

`internal/capability` is the only writer. Every method takes a `Principal`, re-checks
authorization, writes an audit record with `Via` (and `Model` for AI), and returns a `Job` for
anything long-running. HTTP handlers, CLI commands and AI tools are thin adapters.

## Consequences

- The AI's blast radius is bounded by construction: the tool layer filters tools by scope, and
  the capability layer checks again.
- One audit format, one idempotency story, one place to add preconditions.
- **Forbidden:** a handler that touches the store or the agent directly; a raw console write
  path; an AI tool with its own privilege.
- Adds one indirection to every feature, permanently. That tax is the point.
