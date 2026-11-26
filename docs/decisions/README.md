# Decision records

ADRs are **append-only**: accepted records are never edited. A changed decision gets a new
number that supersedes the old one, and the old record gains one line pointing forward.

**The index lives in [`../plan/decision-log.md`](../plan/decision-log.md)** — D1–D22, one line
each, with links to the reasoning here. This directory holds context and consequences; it does
not maintain a second table, because two indexes drift.

## Numbering

- One decision per ADR, sequential, never reused.
- Filename: `NNNN-kebab-title.md`, four digits.
- Accepted records are immutable; supersede with a new number and a link in both directions.
- A policy that constrains future work (migrations, docs, support floor) still gets an ADR.

## Format

```markdown
# 0000 — Title

**Status:** accepted · YYYY-MM-DD

## Context
What forced the decision.

## Decision
The choice, in the present tense.

## Consequences
What gets easier, what gets harder, what is now forbidden.
```
