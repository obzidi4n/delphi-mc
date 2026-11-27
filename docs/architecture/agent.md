# Agent

**Status:** current design · implementation target M1–M5 (self-update at v1.1).

One daemon per node, not one per server. It owns every container, port, log stream, metric and
snapshot on that host, dials out to the panel over a single mTLS WebSocket, and keeps serving
when the panel is unreachable. Nodes are hosts or VMs; groups are logical targeting sets and
do not imply agents. See [`nodes.md`](nodes.md).

## Desired state and reconciliation

- The panel writes **desired state** plus a monotonically increasing `revision`. It never
  sends imperatives ("start this container") as authoritative commands.
- The agent reconciles: fetch desired state → diff against actual → converge → report
  `observed_state` + `applied_revision`. Convergence is idempotent, so a lost frame or a
  panel restart is never a correctness problem.
- Crash restart policy lives in desired state and is executed here, not by the panel
  (ADR [0017](../decisions/0017-agent-owned-restart-policy.md)). Policies are `on-failure`
  (default) and `never`; the first exit is reported, the next observation restarts with
  exponential backoff.
- The last desired set is persisted at `<data_dir>/desired.json`. When the channel is down, a
  supervisor loop reconciles it every 2 s so crashes still recover without the panel.
- **On start the agent adopts what exists.** Containers are labelled
  `delphi.server=<uuid>`; a restarted agent re-attaches to running containers instead of
  recreating them, and a running server is never restarted by an agent restart. Missing or
  exited containers reconcile from desired state like any other drift.

## Container supervision

- Rootless **Podman** default; **Docker** supported and documented as root-equivalent. No
  socket proxy — it cannot restrict `exec`, so it adds a component and no security.
- Server data is a **bind mount** at `/var/lib/delphi/servers/<uuid>`, never a named volume:
  host-side access is required for reflink snapshots, CAS linking and backups.
- Networked servers join `delphi-net-<network-id>` with a hostname-safe alias;
  only the proxy publishes a host port, backends publish none. The agent creates the network
  on first use (`podman network create`) and passes `--network-alias`.
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

- The agent follows container output (`logs -f`, one process per running server) instead of
  tailing `logs/latest.log` with inotify: one cross-platform path for Paper and Velocity, and
  it includes startup output before the file exists. Inotify tailing remains the lower-CPU
  option on Linux; the reader is isolated behind `Runtime.FollowLogs`.
- No log appender in v1.0. A log4j2 unix-socket appender is a v1.1 option only if
  measurements demand it: there is no upstream AF_UNIX appender, it couples the agent to
  log4j2 versions, and it adds a second console path for no user-visible gain at target load.
- Per-server ring buffer (~5k lines) in the agent and another in the panel; sequence numbers
  are assigned per line, so a dropped batch is visible as a gap instead of silent truncation.
- Batches leave every 16 ms as newline-delimited payloads in one binary frame (server id,
  first seq, payload). One frame per line is never used.
- Backpressure: the flusher drops the oldest line and the panel drops a slow browser
  subscriber (which reconnects and gets the ring), never blocking the log reader.
- **Crash artifacts are captured, not reconstructed**: container exit code,
  `hs_err_pid*.log`, `crash-reports/*.txt` and the tail of `latest.log` survive teardown for
  `diagnose` and crash triage. The bridge dies with the JVM; the agent is the only witness.
- **Search is local.** `search_logs` runs `ripgrep` over files on disk: candidates selected by
  date, then timestamps parsed to enforce the range. ripgrep has no time filter and cannot
  read `.log.gz`, so rotated archives are decompressed on the fly. Logs are never shipped to
  Postgres.

## Bridge plugin (v0)

- The plugin jar is embedded in the agent (`internal/agent/bridge/DelphiBridge.jar`, built by
  `make bridge` from `plugin/bridge/`). It is installed into `plugins/` before every start
  when its hash changed, so a server gains or updates the bridge on boot.
- **File transport, not a socket.** Agent and plugin share the server directory, which crosses
  the bind mount on Linux and macOS alike: the plugin atomically replaces
  `.delphi-bridge/state.json` every 0.5 s (TPS, online players, root command names, the last
  200 events) and consumes `.delphi-bridge/cmd/cmd-<id>.json`, deleting each after executing
  it on the main thread. The documented unix socket remains the target if measurement shows
  the file poll is too slow; the frame shapes would not change.
- Agent polls at 500 ms, sends `bridge.state` when TPS/players/commands changed and
  `bridge.event` for each new sequence. The first read adopts the plugin's history instead of
  replaying it, so a reconnect cannot duplicate events.
- Chat is Paper's `AsyncChatEvent`; the command tree is the command map's root labels;
  moderation executes `kickPlayer`/`BanList.addBan` and reports a `moderation` event. The
  panel never parses logs for any of this.
- Bridge state is in memory in the panel (like metrics); it is not persisted.

## Files

- The panel asks the node for file operations over the same agent socket
  (`file.request` → `file.result`, request id correlated). The agent only resolves paths under
  `<data_dir>/servers/<server_id>`: relative paths, no `..`, no absolute paths, and both the
  file and its parent are checked after symlink resolution. Text reads/writes are capped at
  1 MiB; binary or non-UTF-8 files are refused, as are special files.
- Writes are atomic (temp file + rename). The panel stores every write as a `file_revisions`
  row (content, sha256, author, revision) and prunes to the last 50 per file.
- Revisions and rollback are panel-side: the UI/API can diff two stored revisions and restore
  one by writing it back as a new revision. The node never sees revision numbers.

## Plugins

- The panel resolves plugin versions, downloads each artifact into its sha256 CAS, and sends
  `{name, sha256}` in the server spec. The node fetches `GET /agent/blobs/{sha256}` with its
  node certificate, verifies the hash, and atomically installs `plugins/<name>`.
- Names are stable per project (`luckperms.jar`), so an update replaces a jar instead of
  stacking versions. Existing plugins whose hash already matches are not downloaded again.
- Paths are flat filenames ending in `.jar`; slashes, traversal and dotfiles are refused.
- A plain jar fetch from upstream is still used for the server jar itself; plugins always come
  through the panel so an egress-restricted node can install them.
- Plugin staging does not bump the desired revision. A running container gets the new/removed
  jars on disk without restarting; the next manual restart loads the change. The agent keeps a
  `.delphi-managed.json` manifest (and falls back to the previous desired plugin list) so a
  removed managed jar is deleted without touching unrelated jars the owner placed manually.

## Metrics

- M4 samples with one `stats --no-stream` call per second for all containers and parses both
  podman's JSON array and docker's per-line JSON. The cgroup v2 fast path (`cpu.stat`,
  `memory.current`, `io.stat`) is the Linux optimization still to land; it must not change
  the wire shape or the rollups.
- Retention: 1 h @ 1 s, 24 h @ 15 s, 30 d @ 5 m as in-memory rollups in the panel. Rollup
  persistence is not wired yet; a panel restart loses history rather than back-filling.
- While the panel is down, the agent keeps streaming; the panel's sequence gaps are marked in
  the console, and metrics history is **not** back-filled.

## Snapshots and backups

- M10 creates a `tar.gz` of the server directory (world, configs, plugins, jar) excluding
  `logs/`, `cache/`, `crash/`, `backups/`, `.delphi-state.json`, `plugins/.paper-remapped`,
  and every `session.lock`.
- Quiesce: a running server gets `save-off` → `save-all flush` through the bridge, a 1.5 s
  drain, snapshot, then `save-on`. If the bridge is not live the agent stops the container,
  snapshots, and starts it again. Autosave is resumed before the upload, not after.
- The archive uploads over the node's mTLS identity to `POST /agent/backups/{id}`; the panel
  encrypts it (AES-256-GCM, chunked) into `<data_dir>/backups/<id>.enc` and records plaintext
  sha256/size. No plaintext backup touches disk.
- Restore downloads the decrypted archive from the panel, stops the container, renames the
  directory aside, extracts into a fresh directory (rejecting traversal/symlinks), swaps it in,
  and leaves the container stopped. The panel bumps the desired revision; reconciliation
  re-installs the jar/bridge/plugins and starts a running server. A failed restore restarts the
  previous data.
- Filesystem capability is probed at handshake (`reflink`, `zfs`, `copy`). M10 always uses the
  tar path; reflink/zfs snapshot acceleration and age-based retention are deferred.
- Retention keeps the newest 7 backups per server; older files and rows are pruned after each
  successful create.

## Dev environments

- The current implementation clones the server directory through a local tar archive on the
  agent, preserving plugins, configs, and the world while excluding logs/caches/backups. The
  reflink/ZFS acceleration and hot-swap jar watching from the original M17 design remain
  future work.
- The clone gets a scratch port, `white-list=true`, `enforce-whitelist=true`, and a
  `whitelist.json` containing only the owner. It never joins a network or the proxy and never
  receives the production server's port.
- A dev clone is marked with `dev_of`, expires after 24 hours, and is removed by the panel
  cleanup loop through the normal server delete path.

## Bridge plugin

- Agent and plugin negotiate a **protocol version plus capability flags** on connect. The
  agent tolerates plugin *N-1*; the UI shows "plugin update required" for a missing
  capability instead of degrading silently.
- The jar is pinned to the support floor. Updating it is a file write plus a restart — never
  a hot reload.
- Each server has its own bridge token; the unix socket is created by the agent and mounted
  into that server's container, never exposed on the host.
- The bridge ships inside the agent binary as a builtin artifact until the CAS exists (M9).
  It is installed for Paper/Purpur only; Velocity and Folia run without it (Folia therefore has
  no bridge-provided player/TPS/chat telemetry).
  afterwards it is a normal artifact.

## Self-update (v1.1)

Two-slot binary swap, health check, automatic rollback on failure. Release artifacts are
signed (ed25519) and verified against a public key pinned in the binary — the signature
handles a hijacked channel, the slots handle a bad build. See
[fleet](fleet.md) for rollout order and version skew.
