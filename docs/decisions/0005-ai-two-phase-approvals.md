# 0005 — Two-phase AI approvals

**Status:** accepted · 2026-11-26

## Context

An AI that can restart servers must not do so from a chat message alone. The two failure modes
are a model that misunderstands intent and attacker-controlled text that tries to steer it.
Prompt instructions cannot bound either.

## Decision

Mutating tools never execute inline. They return a fully-specified action — summary, blast
radius, preview/diff, idempotency key. The loop persists state, ends the turn, and resumes only
after the owner approves or denies. Approvals are single-use, short-lived, and re-validate
their preconditions on resume. State lives in Postgres, not memory.

## Consequences

- A prompt injection can waste tokens but cannot mutate anything.
- Every mutation has a reviewable artifact, which doubles as the audit trail.
- The UI must render action cards before the product's AI feels useful — this is M7, not a
  polish task.
- **Forbidden:** an "autonomy" setting that skips approval per org; an approval that executes a
  stale plan after the world changed.
