# Install

**Status: target M5.** No installer exists yet. This page defines what "installed" means, so
the alpha exit criteria are testable.

## Shape

One binary plus Postgres. The panel serves the UI and API; the agent runs on each node and
dials out. There is no message broker and no Redis.

```
/etc/delphi/panel.toml        # panel config (M0 defines the file)
/var/lib/delphi/              # data volume (btrfs recommended; ext4 works)
/var/lib/delphi/servers/<id>/ # one subvolume/dataset per server
/var/lib/delphi/artifacts/    # node CAS
/var/lib/delphi/backups/      # local/mounted backup target
```

## Host prerequisites (agent node)

Checked by the installer, not assumed:

| Requirement | Why |
|---|---|
| Linux, cgroup v2, systemd | Container limits and metrics |
| Rootless Podman ≥ 4.x | Container runtime (Docker is supported and root-equivalent) |
| `/etc/subuid` + `/etc/subgid` range for `delphi` | Rootless UID mapping |
| `newuidmap` / `newgidmap` setuid helpers | Rootless UID mapping |
| systemd `Delegate=yes` + lingering for the `delphi` user | cgroup v2 counters readable |
| ~2 GB disk overhead per node + heap + ~400 MB per server | Placement floor |
| Ports 25565–25600 (configurable) reachable | Game traffic |
| Outbound HTTPS to api.papermc.io, Modrinth, Hangar, GitHub | `pull` blob mode; `proxy` mode avoids it entirely |
| Working NTP (`systemd-timesyncd` or chrony) | Schedules, TLS validity, cross-node log correlation |

The installer **fails closed** with a specific message when a prerequisite is missing; a
half-working rootless setup is worse than a refusal.

## Server prerequisites (panel)

- Postgres 16+ with a dedicated database and role.
- TLS termination in front of the panel (reverse proxy is fine and expected). The SPA and API
  serve under a configurable base path (`/delphi/`), and the WS path follows it.
- Backups target: a local/mounted path in v1.0; S3-compatible at v1.1. A **node-local** path is
  not node-loss durable — if the host dies, its backups die with it. Use a mounted network
  target unless losing a node is acceptable.

## Intended flow

1. Install the package or run the binary as a systemd unit; `delphi init` writes config and
   creates the schema.
2. Open the panel, create the owner account, choose an AI endpoint (or skip — surfaces stay
   visible but disabled).
3. Generate a node enrollment token in the UI.
4. Install the agent on the node with the token; it enrolls, obtains a certificate, and
   appears healthy.
5. Create a server from a template and boot it.

## Verification

Observable checks, matching the alpha exit criteria
([`../plan/roadmap.md`](../plan/roadmap.md)):

- Panel serves the UI over TLS at the configured base path.
- The node appears healthy; restarting the panel reconnects without re-enrollment.
- A server boots, console streams, and the bridge reports chat/players/TPS.
- Power actions survive a panel restart; servers keep running while the panel is down.

## Health and metrics

- `/healthz` and `/readyz` for the reverse proxy and the installer's own verification step.
- `/metrics` (Prometheus, M12): job queue depth, agent lag, WebSocket connections and
  console fan-out, and line latency — the numbers behind "<50 ms, 20 viewers". Without them
  the alpha's console criterion is unmeasurable in production.
- Panel and agent logs are structured (`slog`); nothing ships anywhere by default.

## Uninstall

Stop containers, export what you need (per-server config bundle + snapshot), remove the
systemd units and the data volume. Deliberately manual: nothing here deletes worlds on your
behalf.
