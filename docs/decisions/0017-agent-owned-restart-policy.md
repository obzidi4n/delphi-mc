# 0017 — Crash restart policy lives on the node

**Status:** accepted · 2026-11-26

## Context

Automation (schedules, alerts) is panel-driven, which is fine for "restart at 04:00" — a missed
run is annoying. Crash recovery is different: the panel can be down, upgrading or unreachable
at 03:00, and a crashed server must still come back. The invariant "agent owns actual state"
already implies this; the earlier draft nonetheless put restart policy in panel automation.

## Decision

The crash restart policy is part of `desired_state` and is executed by `agent/supervisor`:
exponential backoff (5s → 15s → 45s …), attempt budget, crash-loop detection, then `failed` +
alert. The panel observes, records and alerts; it is not in the restart path.

## Consequences

- A crash recovers during a panel outage, which is when crashes are most likely to go unseen.
- Readiness detection must work without the panel: bridge `Done (x.xxs)!` with a port-open
  fallback.
- Schedules remain panel-driven and explicitly best-effort; the docs say so instead of
  implying otherwise.
- **Forbidden:** implementing restart logic in the panel scheduler, and any policy that
  requires a panel round-trip to decide whether to restart.
