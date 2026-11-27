# Automation

**Status:** M11 implemented (subset). Three mechanisms, deliberately not a rules engine.

## Schedules

Cron expression + timezone; target is a server or group (network at v1.1); actions are
restart, backup, command, broadcast, stop, start.

- Implemented actions: `power.start`, `power.stop`, `power.restart`, `backup.create`.
  Countdowns, drains, group targets, and command/broadcast actions are deferred.
- `next_run` is persisted and advanced after dispatch (`last_run` recorded); the v1 panel is
  single-replica, so there is no SKIP LOCKED claim yet. A second replica would need one.
- Scheduling is panel-driven and therefore **best-effort across panel downtime**. Anything
  that must survive a panel outage belongs in the restart policy, not a schedule.

## Restart policy (agent-owned)

Lives on the node as part of `desired_state`; executed by `agent/supervisor` (ADR
[0017](../decisions/0017-agent-owned-restart-policy.md)).

- Implemented policy: `on-failure` (default) or `never`, stored on the server and sent in the
  spec. The first observation of an unexpected exit reports `exited` (visible to alerts and
  crash artifacts); a following observation restarts after a short delay. Backoff doubles per
  attempt, capped at 60 s.
- The agent persists the last desired set at `<data_dir>/desired.json` and supervises it while
  the panel channel is down, so a crash at 03:00 still recovers. It does not create missing
  containers or fetch artifacts offline; that remains panel-driven convergence.
- Max attempts / terminal `failed` state and readiness-gated restart are deferred.
- The panel observes and alerts; it is not in the restart path.

## Alerts

Threshold rules with durations, not instant spikes:

| Kind | Example |
|---|---|
| Metric | TPS < 15 for 5 min; memory > 90% for 10 min |
| State | server down, crash loop |
| Operational | backup failed, update available |
| Player count | above/below a bound |

Implemented kinds: crash-loop and server-down thresholds over a rolling in-memory window.
Channels: Discord-shaped webhook (HTTP JSON `{"content": ...}`) and SMTP. Channel configs are
sealed with the panel's `secrets.key` (AES-256-GCM); the list API never returns them. Metric
and player-count rules, durations, escalation, and in-app delivery are deferred.

Suppressed during maintenance windows and scheduled restarts. Alert fatigue is the failure
mode: durations, sensible defaults, and one place (the window) to silence a known event.

## Maintenance windows

Implemented org-wide windows with `starts_at`/`ends_at`. `suppress_alerts` skips delivery
while still recording the firing alert; `block_updates` is stored but not yet enforced by the
update service. Server/group scoping and the server-card badge are deferred.

## Precedence

1. Maintenance window suppresses alert delivery (auto-update blocking is not wired yet).
2. Restart policy always runs — a maintenance window does not make a crash acceptable.
3. Schedules run even if the target is mid-job; overlapping runs are not coalesced yet.

## Deferred

`when X then Y` rules engines, escalation policies, on-call rotation. The three mechanisms
above cover the real need; a rules engine is a second programming language with worse
observability.
