# Agent

**Status:** current design · implementation target M1–M5 (self-update at v1.1).

One daemon per node. It owns containers, ports, logs, metrics and snapshots, dials out to the
panel over a single mTLS WebSocket, and keeps serving when the panel is unreachable.

## Desired state and reconciliation

- The panel writes **desired state** plus a monotonically increasing `revision`. It never
  sends imperatives ("start this container") as authoritative commands.
- The agent reconciles: fetch desired state → diff against actual → converge → report
  `observed_state` + `applied_revision`. Convergence is idempotent, so a lost frame or a
  panel restart is never a correctness problem.
- Crash restart policy lives in desired state and is executed here, not by the panel
  (ADR [0017](../decisions/0017-agent-owned-restart-policy.md)).
- **On start the agent adopts what exists.** Containers are labelled
  `delphi.server=<uuid>`; a restarted agent re-attaches to running containers instead of
  recreating them, and a running server is never restarted by an agent restart. Missing or
  exited containers reconcile from desired state like any other drift.

## Container supervision

- Rootless **Podman** default; **Docker** supported and documented as root-equivalent. No
  socket proxy — it cannot restrict `exec`, so it adds a component and no security.
- Server data is a **bind mount** at `/var/lib/delphi/servers/<uuid>`, never a named volume:
  host-side access is required for reflink snapshots, CAS linking and backups.
- Each server directory is its **own subvolume/dataset** at create time — btrfs snapshots and
  `zfs clone` require it (`cp --reflink` does not). Retrofitting is a migration.
- UID mapping: `--userns=keep-id` (or explicit `--uidmap`) so host files stay owned by
  `delphi`. This is tested on day one of M2; it quietly poisons everything.
- Installer-checked prerequisites, not assumptions: `/etc/subuid` + `/etc/subgid` ranges,
  `newuidmap`/`newgidmap`, and systemd `Delegate=yes` + lingering so cgroup v2 counters are
  readable.
- Hardening: non-root, read-only rootfs, `tmpfs /tmp`, `--cap-drop=ALL`,
  `no-new-privileges`, default seccomp, no host network, explicit port publish.
- Images: `delphi/java:21-temurin`, `:25`. Support floor is Paper 1.20.5+ / Java 21
  (ADR [0016](../decisions/0016-paper-1-20-java-21-floor.md)).
- Ports come from a per-node pool **owned by the panel**: the node reports its range at
  `hello`, the panel assigns ports into desired state, and the agent reports observed bindings.
  The agent never writes Postgres; a binding it did not expect is a drift report, not an
  assignment.

### Server lifecycle (M2)

The panel sends the node's full desired server set; the agent converges it
(`internal/agent/supervisor.go`):

- Creates `<data_dir>/servers/<id>` as its own btrfs subvolume when the filesystem and `btrfs`
  tooling allow, otherwise a plain directory; the node reports `reflink`, `zfs` or `copy` in
  `hello`.
- Prepares the directory: `eula.txt` only when the operator accepted the EULA, a default
  `server.properties` only when absent, and the pinned `paper.jar` verified by sha256 and
  installed with an atomic rename.
- Runs one container per server with its own published port, memory and CPU limits, read-only
  rootfs, `tmpfs /tmp`, `--cap-drop=ALL`, `no-new-privileges`, and the server directory at
  `/data`. The JVM heap is `memory_mb − 512` (minimum 256 MiB): a 1 GiB limit OOM-kills Paper
  during world gen because metaspace and native memory sit above the heap.
- On Linux it adds `--userns=keep-id` and the invoking UID so host files stay owned by the
  agent's user; on macOS the machine's virtiofs share already maps ownership.
- Records the applied revision in `.delphi-state.json` and in the `delphi.revision` container
  label, so an agent restart neither restarts running servers nor re-downloads jars.
- On `exited`, captures `logs` and any `hs_err_pid*.log` under `crash/rev-<n>/`, once per
  revision, so a crash loop cannot fill the disk.
- Power is declarative: start/stop/restart/kill mutate desired state; kill is a flag consumed
  by the next reconcile, and revision bumps trigger a restart of an already-running container.

## Console pipeline

- Tail `logs/latest.log` via inotify, handling rotation, partial lines and truncation —
  identical for Paper and Velocity.
- No log appender in v1.0. A log4j2 unix-socket appender is a v1.1 option only if
  measurements demand it: there is no upstream AF_UNIX appender, it couples the agent to
  log4j2 versions, and it adds a second console path for no user-visible gain at target load.
- Per-server ring buffer (~5k lines) of **raw bytes** for instant attach; parse levels and
  timestamps only when rendering or searching.
- Batch outbound frames every ~16 ms as newline-delimited batches — never one frame per line.
- Backpressure: drop oldest, never block the server's log reader.
- **Crash artifacts are captured, not reconstructed**: container exit code,
  `hs_err_pid*.log`, `crash-reports/*.txt` and the tail of `latest.log` survive teardown for
  `diagnose` and crash triage. The bridge dies with the JVM; the agent is the only witness.
- **Search is local.** `search_logs` runs `ripgrep` over files on disk: candidates selected by
  date, then timestamps parsed to enforce the range. ripgrep has no time filter and cannot
  read `.log.gz`, so rotated archives are decompressed on the fly. Logs are never shipped to
  Postgres.

## Metrics

- cgroup v2 `cpu.stat`, `memory.current`, `io.stat` at 1 s. Near-free and exact.
- Never shell out to `ps` in a loop.
- Retention: 1 h @ 1 s, 24 h @ 15 s, 30 d @ 5 m, stored as rollups (rollup persistence starts
  at M4).
- While the panel is down, samples queue locally and a visible gap is reported on reconnect.
  Metric history is **not** back-filled.

## Snapshots and backups

- Filesystem capability is probed at handshake and reported per node:
  `reflink` (btrfs / XFS with `reflink=1`), `zfs`, or `copy` (ext4).
- `cp --reflink` / `zfs clone` when available; `tar` fallback otherwise. The installer offers a
  dedicated btrfs volume so ext4 hosts can still get fast snapshots.
- Order: `save-off` → `save-all flush` → snapshot → `save-on`.
- Backups are encrypted at rest with a key separate from Postgres; retention is a per-target
  policy (count + age) enforced by a GC job.
- Reflink-dependent features (dev environments, staging boot-checks) are **gated in the UI**,
  not silently slow.

## Dev environments (v1.1)

- Clone a server directory using the detected capability: reflink or ZFS clone is instant and
  near-zero bytes; on a `copy` node it still works, but the UI warns that cloning takes minutes
  and duplicates disk.
- Boot the clone on a scratch port with a dev jar, watch the build output directory, copy the
  jar in and restart on change. An idle timeout tears the environment down.
- A dev environment never joins a network or the proxy, and never receives the production
  server's port. It is a sandbox, not a backend.

## Bridge plugin

- Agent and plugin negotiate a **protocol version plus capability flags** on connect. The
  agent tolerates plugin *N-1*; the UI shows "plugin update required" for a missing
  capability instead of degrading silently.
- The jar is pinned to the support floor. Updating it is a file write plus a restart — never
  a hot reload.
- Each server has its own bridge token; the unix socket is created by the agent and mounted
  into that server's container, never exposed on the host.
- The bridge ships inside the agent binary as a builtin artifact until the CAS exists (M9);
  afterwards it is a normal artifact.

## Self-update (v1.1)

Two-slot binary swap, health check, automatic rollback on failure. Release artifacts are
signed (ed25519) and verified against a public key pinned in the binary — the signature
handles a hijacked channel, the slots handle a bad build. See
[fleet](fleet.md) for rollout order and version skew.
