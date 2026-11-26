# 0013 — Approval card always; name-typing above one server

**Status:** accepted · 2026-11-26

## Context

Approval fatigue and approval blindness are both real. If every action needs a typed
confirmation, people type the name without reading. If single-server actions need no
confirmation, a misread intent restarts the wrong server.

## Decision

Every AI mutation gets a card. Additionally, an action that touches **more than one server**
or uses a **destructive verb** (kill, delete, restore) requires typing the target name,
GitHub-style. Single-server restarts and commands need one click. Approvals are single-use,
expire in ~15 minutes, and re-validate preconditions on resume.

## Consequences

- The high-frequency, low-blast-radius case stays fast.
- The irreversible case cannot be a reflex.
- The UI must compute blast radius (`servers`, `players_online`) before rendering the card —
  a stale card is a bug, not a cosmetic issue.
- **Forbidden:** an org-level "auto-approve" switch. It is the knob people misuse, and it
  dissolves the safety argument for the whole feature.
