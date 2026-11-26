# 0008 — Native TUI as a pure API client (v1.1)

**Status:** accepted · 2026-11-26

## Context

Shell-native operators want k9s-class ergonomics, not a web app in a terminal emulator. A TUI
that links the panel's internals directly would be a second implementation of the API and bug
surface with no contract. A TUI built in v1.0 would also cost a full second UX while the API
is still moving.

## Decision

The TUI is **v1.1**, built with Bubble Tea, and consumes only `internal/client` — the same
generated client as the CLI. If the TUI needs a private endpoint, the API is wrong and gets
fixed. v1.0 ships the scriptable CLI instead, generated from the OpenAPI spec at M3a.

## Consequences

- API completeness is proven earlier and more cheaply by generated clients and contract tests
  than by a second UI.
- Deferring the TUI costs no architecture: it is one more consumer.
- **Forbidden:** TUI-only endpoints, TUI-side business logic, and password prompts in a
  terminal (device-code flow with keychain/`0600` fallback).
- The vision still claims terminal-native operation — delivered in v1.1, and the CLI carries
  the claim until then.
