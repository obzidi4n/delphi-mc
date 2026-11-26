# 0018 — Bounded proactive AI

**Status:** accepted · 2026-11-26

## Context

An AI operator that only answers when asked is a demo. Crashes and alerts are where it earns
its keep: the owner is asleep, the server died, and the useful output is "it was a
WorldEdit/Oraxen conflict, here is the signature and the doc". But unrequested model calls
cost money, can hallucinate, and a self-hosted install has no central corpus to learn from.

## Decision

Two proactive behaviours are **on by default and bounded**: crash triage and alert
explanations. Daily/weekly digests and update risk notes are **opt-in**. Every proactive run
is explanation only — it may call read tools and cite docs, and it can never mutate, create
an approval, or take an action.

Bounds: hard daily token cap per org with a visible meter, quiet hours, per-tool result
truncation, and evidence restricted to what the agent captured (exit code, `hs_err_pid*`,
`crash-reports/*`, log tail) plus the docs corpus.

## Consequences

- Value arrives without approval fatigue, because nothing proactive can act.
- The token cap and meter are not optional features; they ship with the behaviour (M19).
- `known_issues` is per install. Cross-install signature sharing is an opt-in telemetry
  decision, not an assumed moat — a single install's table only grows from its own crashes.
- **Forbidden:** a proactive path that mutates, an unbounded background model loop, and any
  proactive output presented without provenance.
