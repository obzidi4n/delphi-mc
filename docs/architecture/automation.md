# Automation

**Status:** current design · implementation target M11. Three mechanisms, deliberately not a
rules engine.

## Schedules

Cron expression + timezone; target is a server or group (network at v1.1); actions are
restart, backup, command, broadcast, stop, start.

- Pre-actions are built in: countdown broadcasts, `save-all flush`, player drain.
- `next_run` is claimed with `UPDATE … RETURNING`, never an in-process timer, so a second
  replica cannot double-fire.
- Scheduling is panel-driven and therefore **best-effort across panel downtime**. The UI says
  so. Anything that must survive a panel outage belongs in the restart policy, not a schedule.

## Restart policy (agent-owned)

Lives on the node as part of `desired_state`; executed by `agent/supervisor` (ADR
[0017](../decisions/0017-agent-owned-restart-policy.md)).

- On crash: exponential backoff (5s → 15s → 45s …), max attempts, then mark `failed` and
  alert.
- Crash-loop detection: N exits within 90 s of start.
- Readiness is `Done (x.xxs)!` via the bridge, with a port-open fallback for when the bridge
  is unavailable (e.g. before M5, or a plugin failure).
- The panel observes and alerts; it is not in the restart path. A crash at 03:00 with the
  panel down still recovers.

## Alerts

Threshold rules with durations, not instant spikes:

| Kind | Example |
|---|---|
| Metric | TPS < 15 for 5 min; memory > 90% for 10 min |
| State | server down, crash loop |
| Operational | backup failed, update available |
| Player count | above/below a bound |

Channels: in-app, Discord webhook, generic webhook, and email/SMTP (M11). Accounts are
admin-created without email, so SMTP exists for alerts only. Secrets live in `secret_ref`,
never in the rule.

Suppressed during maintenance windows and scheduled restarts. Alert fatigue is the failure
mode: durations, sensible defaults, and one place (the window) to silence a known event.

## Maintenance windows

Per server or group; suppress alerts and block auto-updates. A window is visible on the
server card so nobody has to remember it existed.

## Precedence

1. Maintenance window suppresses alerts and auto-updates.
2. Restart policy always runs — a maintenance window does not make a crash acceptable.
3. Schedules run unless the target is mid-job (coalesce, don't queue a stampede).

## Deferred

`when X then Y` rules engines, escalation policies, on-call rotation. The three mechanisms
above cover the real need; a rules engine is a second programming language with worse
observability.
