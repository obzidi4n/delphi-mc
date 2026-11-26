# 0012 — No bundled model runtime; probe-gated tools

**Status:** accepted · 2026-11-26

## Context

The product's identity is an AI operator, and the privacy story is local-first. The tempting
default is to install Ollama and pull a model during onboarding. That install is where
first-run experience goes to die: GPU expectations, multi-gigabyte downloads, and small local
models that cannot call tools — which is the entire product surface.

"Local-first" is a policy about where data goes, not an instruction to ship a runtime.

## Decision

No bundled model runtime. Onboarding offers an OpenAI-compatible endpoint (cloud, OpenRouter,
a local server the owner already runs) or an existing Ollama instance. On connect, a
**tool-calling probe** runs one forced tool call; models that fail it are usable in chat-only
mode, visibly labelled. AI surfaces are visible but disabled when no model is configured.

## Consequences

- First run succeeds with a key, or degrades honestly without one.
- One provider adapter covers every supported backend; native Anthropic (prompt caching) waits
  for v1.1.
- The AI is opt-in by configuration but always discoverable in the UI.
- **Forbidden:** a shared key, a silently-hidden AI surface, and registering tools against a
  model that failed the probe.
