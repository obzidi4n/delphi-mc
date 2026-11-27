# Incident runbook

**Status:** current at v1.0 (M12). The point of this page is the "what still works" column:
the system is built so most incidents do not page anyone.

## What still works

| Failure | Servers | Console | Restart on crash | Alerts | Recovery |
|---|---|---|---|---|---|
| Panel down | running | no (UI/API offline; agents buffer console) | **yes** (agent-local, via `<data_dir>/desired.json`) | no | Start the panel; agents reconnect; the UI banner explains the restart |
| Postgres down | running | **unavailable** — no auth or API; open WS sessions continue until reconnect | yes | no | Restore Postgres; queue resumes |
| Node unreachable | running | no | yes (if the node is alive) | after N misses | Fix the node; desired state re-converges |
| Node host dead | down | no | no | yes | Restore on another node **only if the backup target is mounted/networked**; a node-local target dies with the host (S3 at v1.1) |
| AI provider down | running | yes | yes | yes | All non-AI features work; AI surfaces show "offline" |
| Disk full (node) | possibly | degraded | may fail | yes | Free space; see below |
| Certificate expired | running | no | yes | yes | Re-enroll with a renewal token |

## Panel down

Agents are autonomous: containers keep running, the crash restart policy still executes from
the persisted desired set, and console lines buffer in the agent ring buffer. On restart,
desired state re-converges and running containers are not recreated or restarted. The fleet
page shows a degraded banner while nodes are offline and for ten minutes after a panel start,
saying explicitly that servers keep running and restarts are agent-local. UI actions are
unavailable while the panel is down; there is no offline write queue.

Do **not** restart agents to "fix" a panel outage; that is the one action that can interrupt
servers.

## Node unreachable / unhealthy

Distinguish three cases before acting:

1. **Agent disconnected, host alive** — servers are running. Check network/TLS and the
   certificate; the panel shows the last heartbeat.
2. **Host down** — servers are down. Snapshots on that host are gone. Restore from a backup on
   a mounted/network target; a node-local backup target died with the host (S3 offload is
   v1.1, not a v1.0 rescue).
3. **Agent running, containers unhealthy** — read the last log lines and the crash artifacts
   (`hs_err_pid*.log`, `crash-reports/`) from the server detail view.

## Crash loop

The restart policy backs off and eventually marks the server `failed`. Read the crash
artifacts, fix the cause, then start manually — do not clear the failure without a reason.
A crash loop that survives a rollback of the last plugin/jar change is a bug report, not an
operator error.

## Disk full

1. Identify: backups, the CAS, or world growth.
2. The CAS is registry-backed and GC-able; a stale artifact set is safe to reclaim. Backups
   are encrypted archives; deleting one is immediate and irreversible.
3. Never delete a server directory to free space — export or snapshot first.

## Failed restore

A restore into an occupied directory is refused by design. Stop the server, snapshot the
current state (even a broken one is evidence), then restore. A restore that fails its
verification leaves the server stopped and the previous snapshot intact.

## Certificate or secret exposure

Revoke the node, rotate the affected secrets (bridge tokens, database credentials), re-enroll,
and restore from a backup predating the exposure if integrity is in doubt.

## Escalation

There is no on-call rotation in v1.0 (deliberately). The alerts page the owner; this page is
the first responder.
