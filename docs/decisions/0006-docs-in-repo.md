# 0006 — Docs in-repo, doubling as the RAG corpus

**Status:** accepted · 2026-11-26

## Context

Documentation that lives outside the repository drifts from the code, and the AI needs a
corpus. A wiki or a hosted docs site would be a second thing to deploy, version and back up —
for a product whose selling point is one binary.

## Decision

All docs live in [`docs/`](../README.md), versioned with the code, and are the corpus for
`search_docs` (Postgres full-text search over an embedded snapshot). Structure is a retrieval
concern: one concept per page, descriptive headings, no giant pages. ADRs capture durable
*why*.

## Consequences

- Docs change in the same PR as the behavior, so drift is visible in review.
- The AI can answer "how do I..." questions offline, with citations.
- The corpus must be **embedded at build time**, not scraped at runtime, so search works
  without network access.
- Third-party docs (PaperMC) are embedded under their license with attribution: the terms are
  verified before M6, and every chunk records `source_url` and `corpus_version`.
- **Forbidden:** a separate wiki, and editing a doc without the code change it describes.
- Docs are not free: every page has unshipped-behavior status markers that must be updated
  when the behavior lands.
