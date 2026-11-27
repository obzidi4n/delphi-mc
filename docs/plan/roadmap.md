# Roadmap

**Status:** living document — with the decision log, one of the only pages expected to churn.
Last updated 2026-11-26 (v0.9).

No dates. Milestones are gates with observable exit criteria; a milestone is done when its
criteria can be demonstrated on a clean host.

## Phases

| Phase | Milestones | Outcome |
|---|---|---|
| Alpha | M0–M5 | Install → enroll → create from template → boot → console → bridge |
| v1.0 | M6–M12 | AI, files, jar/plugin updates, backups, automation, hardening |
| v1.1 | M13–M19 | TUI, networks, fleet ops, S3, dev envs, databases, proactive AI |

## Delivered

- **M0** — monorepo, single binary, embedded page, Postgres + migrations, config, docs tree.
- **M1** — enrollment, per-node certificates, the mTLS WebSocket, node registry and health.
- **M2** — container lifecycle on rootless Podman (create/start/stop/kill, limits, crash
  artifacts, pinned jar).
- **M3a** — spec-first OpenAPI with generated DTOs and Go client, users/API keys/sessions,
  role→scope profiles (owner/admin/developer/viewer, server and group bindings), user
  management (API + Team page), audit (write + read), server CRUD with rename and soft
  delete, groups (API + page), `server_summary`, cursor pagination on every list endpoint,
  and the first CLI commands (`nodes`, `servers`, `audit`).

- **M3b** — template schema and validation, seven embedded built-ins, PaperMC/Purpur channel/build
  resolution with sha256, instantiation that renders files and records `template_id`/
  `template_version`, and JSON/YAML import/export. Plugin downloads landed at M9. Exit: a
  Paper server created from the Survival template boots.

- **M4** — realtime console: agent tailing with per-line sequence numbers, 16 ms binary
  batches, a 5k ring in the agent and panel, browser WebSocket fan-out (20 viewers tested
  under 50 ms), a console page with ANSI colour, search, pause, follow, copy, reconnect and
  power buttons, and 1s/15s/5m CPU+memory rollups exposed at `/servers/{id}/metrics`.
  Command input is wired once the bridge is live.

- **M5** — bridge plugin v0 embedded in the agent and self-installed: file transport for
  state/commands, chat events, online players, TPS, the root command tree, direct console
  commands through `POST /servers/{id}/command`, and kick/ban moderation through
  `POST /servers/{id}/moderation`. A console sidebar shows TPS, players, chat and the command
  tree. Live chat with a real player is the remaining manual check.
- **M8** — files: node-side list/read/write over request/response frames with path and size
  containment, a file manager page (browse, edit, save, revisions, unified diff, restore),
  and `file_revisions` rollback. The AI gains `write_file` (10 tools). Live: edited
  `server.properties` through the UI, diffed r3→r4, restored r1 as r3 via API, and an AI
  approval created `ai-note.txt`.
- **M9** — artifacts and plugins: a panel-side sha256 CAS with an authenticated
  `/agent/blobs/{sha}` endpoint, Modrinth/Hangar/GitHub resolution (pins and version-prefix
  ranges), plugins installed at template instantiation under stable project names, update
  detection, guarded apply with restart/boot-check rollback, artifact GC, and the `plugins`
  (read) and `update_server` (approval) tools. Live: Plugins-Demo booted with LuckPerms,
  Chunky and ViaVersion; two pinned builds updated to current with the server still running;
  unreferenced old artifacts were collected. Staging-clone boot-checks and `auto-*` policies
  remain deferred.
- **M15** — fleet ops: automatic placement (least-loaded healthy non-draining node with a
  free port), node drain toggles that exclude placement, and signed agent self-update over the
  node channel with a one-slot rollback and `delphi fleet update`. Live: enrolled three agents,
  pushed one signed binary/command, all three re-exec'd and reported `m15-new`, each keeping a
  `.rollback`; auto-placement chose node-d, then node-e after node-d was drained. Protocol
  version 1 is enforced for newer agents. Automatic post-update health rollback, image/agent
  version pinning at build, and node-level capacity accounting remain deferred.
- **M14** — Velocity networks (legacy forwarding): one container network per network, proxy
  publishes the public port while backends publish nothing, hostname-safe aliases, generated
  `velocity.toml`/`server.properties`/`spigot.yml`, Java 25 proxy template, and
  `POST/GET /api/v1/networks`. Live: m14net4 proxy answered a Minecraft status ping (Velocity
  1.7.2-26.3, 0/100) on its published port; both backends ran with no host ports; proxy DNS
  resolved their aliases. A mineflayer 1.21.11 bot joined through the proxy and spawned on a
  backend (`DelphiTest joined the game`, seen from the proxy's network IP). The proxy was
  toggled to offline mode for the scripted join; generated configs remain online-mode=true.
  Modern forwarding, the Velocity plugin/drain, and network delete remain deferred.
- **M13** — TUI: `delphi tui` on Bubble Tea/lipgloss with token-authenticated console WebSocket,
  fleet → server navigation, live console replay, power/backups/plugins/updates, and an AI
  pane that streams deltas/tools/citations and prompts for approval. Keys documented in
  `docs/reference/tui-keybindings.md`. Live: started a server, watched the console, created a
  backup, asked the operator, and stopped it from the terminal. Inline file editing, log
  search, sparklines, multi-server panes, and the React portal remain deferred.
- **M12** — v1.0 exit: base-path routing covers pages, WebSockets, API, and agent endpoints (verified
  through a Go reverse proxy at `/delphi`), a degraded-status banner and node table explain
  offline nodes and recent panel restarts, an audit viewer page, and `delphi upgrade`/
  `delphi rollback` binary slots. Live: with Plugins-Demo running and the panel stopped, port
  25566 stayed open; a kill was restarted by the agent with the panel still down; after the
  panel returned the fleet page showed the server running and the outage banner. Automatic
  health-gated upgrade/rollback and agent self-update remain deferred to v1.1.
- **M11** — automation: cron schedules with persisted `next_run` (power and backup actions),
  agent-owned crash restart using the last desired set on disk (`on-failure`/`never`,
  backoff), crash-loop/server-down rules, webhook (Discord payload) and SMTP channels with
  sealed configs, maintenance windows that suppress delivery, and AI `create_schedule`.
  Live: a minute cron restarted Plugins-Demo twice; killing the container produced a
  Discord-shaped webhook alert and the agent restarted it; with the panel stopped, a kill was
  still recovered by the offline supervisor. Leased multi-panel claims, pre-actions, group
  targets, and block-updates enforcement remain deferred.
- **M10** — backups: quiesced `tar.gz` snapshots (bridge `save-off`/`save-all flush`/
  `save-on`, stop/start fallback), upload over node mTLS, AES-256-GCM at rest with
  `<data_dir>/backup.key`, keep-last-7 retention, restore with directory replacement and a
  desired-revision bump, and AI `backups`/`backup`. Live: Plugins-Demo snapshot (184 MB) while
  running, motd mutated, restore reverted it and the server returned to running. Reflink/zfs
  snapshot acceleration and age-based retention remain deferred.
- **M7** — AI actions: create_server, power, run_command, moderate_player as two-phase
  tools; durable approval plans, approval cards in the chat UI, approve/reject that executes
  as the user and resumes the model, and audit entries. Live: a group restart plan approved
  to two server restarts, `run_command "list"` executed by the plugin, and an AI ban issued
  through the bridge.
- **M6** — AI foundation: OpenAI-compatible provider, conversations and messages in
  Postgres, docs FTS over the embedded corpus (288 chunks), five read-only tools (docs
  search, servers, server detail, health, console tail), a 48k-character context broker, and
  a streaming chat page with tool chips, citations, and a visible chat-only fallback.
  Demonstrated on Ollama qwen2.5:3b (tools) and gemma3:4b (chat-only).

## Alpha: the vertical slice (M0–M5)

```
install → enroll node → create server from template → container boots
       → console streams → bridge plugin loads → chat/players/TPS appear
```

Everything after M5 is additive. None of it invalidates the slice; all of it depends on the
slice being real. The alpha is also where the top risks live (rootless UID mapping, cgroup
delegation, the container socket, bridge injection, console latency) — answered first, when
changing course is cheap.

### Exit criteria (observable, not "feels done")

1. One binary installs on a clean Ubuntu host, with the rootless prerequisites the installer
   verifies (subuid/subgid ranges, cgroup v2 delegation).
2. A node enrolls with a pasted token and holds a signed node identity.
3. A server is created from the Survival template and boots.
4. Console streams at <50 ms (line emitted → painted in a browser) with 20 concurrent viewers.
5. The bridge plugin auto-installs; chat, players and TPS appear in the UI.
6. Start/stop/restart/kill work and survive a panel restart.

### Explicitly outside alpha

AI, networks, TUI, file manager, plugin updates, backups, automation, agent self-update. The
CLI is **inside** alpha: generated from the M3a OpenAPI spec, so the public API is exercised by
a real consumer from the start. Nothing is cancelled — only ordered. The schema still carries
`org_id`, a nullable `network_id`, and capability flags from M0.

## Sequence

| # | Milestone | Phase | Exit criteria |
|---|---|---|---|
| 0 | Monorepo (Go), single binary, embedded UI, Postgres + migrations, config | Alpha | `delphi panel` serves a hello page; `delphi agent` connects; docs tree committed |
| 1 | Fleet spine: enrollment + per-node identity (CSR → signed cert), the single mTLS WebSocket channel (hello/heartbeat, revocation), node registry with health transitions, `GET /api/v1/nodes` + `delphi nodes` | Alpha | Two agents healthy over one channel; a revoked node's channel closes and reconnect is refused |
| 2 | Container lifecycle: create/start/stop/kill, ports, limits, per-server subvolume/dataset, log capture + crash artifacts, a pinned jar URL + sha256 fetch | Alpha | A Paper 1.20.5+ server boots from a hardcoded spec and is playable from the API |
| 3a | Capability layer + spec-first OpenAPI (hand-written spec → generated Go/TS clients + DTOs) + auth + RBAC + audit (write and read) + server CRUD + groups + `server_summary` + first CLI commands | Alpha | Create a server from a hardcoded spec; the CLI lists and powers it; the audit API shows both |
| 3b | Templates: schema, built-ins, **version resolution** (`mc/versions`), instantiation, import/export; template bundles and jars stored as sha256 blobs | Alpha | Create a server from the Survival template |
| 4 | Realtime console: ring buffer, batching, WS hub, power, metric rollups (1s/15s/5m), premium console checklist | Alpha | <50 ms emit→paint; 20 viewers on one server |
| 5 | Bridge plugin v0, embedded in the agent binary: chat, players, TPS, command tree, moderation verbs | Alpha | Live chat + TPS in UI; plugin self-installs; kick/ban through the API |
| 6 | AI foundation: OpenAI-compatible adapter + tool-calling probe, conversation store, embedded docs corpus → FTS, 5 read tools, context broker, chat UI | v1.0 | "Why is Survival-1 lagging?" answers with cited tools; a non-tool model degrades visibly to chat-only |
| 7 | AI actions: `create_server`, `power`, `run_command`, `moderate_player`; approval cards, resumable loop, audit | v1.0 | "Restart all SMP servers" → plan → approve → done |
| 8 | File manager + code editor + revisions and rollback (`write_file` → 10 tools) | v1.0 | Edit + diff + save `server.properties`; roll back a revision |
| 9 | Provider plugin manager, artifact GC, update policies, staging boot-check (`plugins`, `update_server`); Paper resolution landed at M3b, Velocity at M14 | v1.0 | Install Paper + 3 plugins; detect + apply an update |
| 10 | Snapshots/backups/restore on local/mounted storage, encrypted, with retention (`backup`) | v1.0 | Restore a server from a 1 h-old backup |
| 11 | Automation: schedules (claimed `next_run`), agent-local restart policy, alert rules, Discord/webhook + SMTP channels, maintenance windows | v1.0 | Nightly restart + crash-loop alert in Discord; a crash restarts with the panel down |
| 12 | v1.0 exit: degraded-mode matrix ([incident runbook](../operations/incident-runbook.md)), reverse-proxy base path, audit viewer, panel upgrade/rollback | v1.0 | Stop the panel mid-run: servers serve, restarts still happen, the UI explains it |
| 13 | TUI + client polish (`delphi tui`, AI pane, approvals) | v1.1 | Manage a server end-to-end from a shell, including the AI pane |
| 14 | Networks (Velocity): proxy + backends, container network, generated config, Velocity plugin for drain and events | v1.1 | Two backends behind a proxy, joined and playable; backends not publicly reachable |
| 15 | Fleet ops: placement, drain, signed agent self-update, version skew | v1.1 | Update 3 agents with one command, zero manual SSH |
| 16 | S3-compatible backup offload (multipart, presigned, restore-from-S3) | v1.1 | Restore a server straight from S3 |
| 17 | Dev environments (reflink clone, hot-swap, idle teardown) | v1.1 | Clone → boot → hot-swap jar → teardown on idle |
| 18 | Databases (provision, creds, backup) | v1.1 | Provision MySQL for a server; credentials available through an audited one-time reveal |
| 19 | Anomaly detection + proactive AI (digests, triage), consuming the M4 rollups | v1.1 | Alert arrives with an AI-written explanation |

## Why this order

Milestones 6/7 come before file and plugin management deliberately: the AI tool layer is the
product's identity and the capability layer is its foundation — the registry is
capability-gated and grows from ten tools toward its fifteen ceiling as evals demand.

M3 is split into 3a/3b because capability + auth + RBAC + audit + generated clients is what
every later milestone stands on, and templates are the first thing built on top of it.

v1.1 holds everything that pays off at larger scale or needs a second surface: the TUI, proxy
networks (with the Velocity plugin that drain actually requires), fleet operations, and the
S3/dev-env/database tail.

## Risks

| Risk | Mitigation |
|---|---|
| Rootless Podman UID mapping vs bind mounts | Spike at M2; explicit `--uidmap` fallback |
| Container socket = root | Rootless default; Docker documented root-equivalent; agent is the only client; no proxy |
| Prompt injection via chat/logs/plugin metadata | Fencing + approval gate + no auto-tool from untrusted text |
| AI hallucinating server state | Tool-first prompt + provenance UI + evals |
| Cloud AI egress for self-hosters | Local-only mode, redaction at egress and at rest, BYO key, off unless configured |
| Folia bridge/telemetry | Folia template/runtime supported; Paper bridge is not installed, so player/TPS/chat telemetry is unavailable |
| SpigotMC has no API | Manual upload path from day one |
| Reflink unavailable on ext4 | Capability-detected (ADR 0010); `tar` fallback and UI gating before M10 |
| Node loss with local-only backups | A mounted/network backup target is required for node-loss recovery; S3 offload at v1.1 |
| Agent version skew across a fleet | Capability handshake + N-1 contract (v1.1 rollout) |
| Velocity config drift (secret/forwarding) | Both sides generated from one source (v1.1) |
| Backends accidentally publicly reachable | Per-network container network; backends publish no ports (v1.1) |
| Template drift breaking a running server | Versioned templates; re-apply is explicit and diffed |
| Alert fatigue | Durations, sensible defaults, maintenance windows |
| TUI becomes a second implementation | TUI is a consumer of `internal/client` only (v1.1) |
| Two clients, two bugs | Shared SDK; contract tests against the OpenAPI spec |
| Local model without tool calling | Probe at connect; chat-only fallback is visible, not silent |
| Panel down when a server crashes | Restart policy is agent-local (ADR 0017) |
| Second replica double-fires work | Jobs and `next_run` are claim-based from M1 |
| Single-transport ceiling | Written trigger: second replica or >50 nodes reopens the broker question |
| Pre-1.20.5 demand | Explicit floor (ADR 0016); no parallel code paths |

## Changelog

- **M2 (2026-11-26)** — Container lifecycle landed. Servers are created through the capability
  layer (pinned jar URL + sha256, EULA gate, port allocation), pushed to their node as desired
  state, and reconciled: btrfs subvolume when available (plain dir fallback), `eula.txt` and
  `server.properties`, sha256-verified `paper.jar`, a hardened container with limits, periodic
  observed-state reporting, and crash artifacts only for unexpected exits. Verified live on
  Podman: Paper 1.21.4 booted to `Done (28.140s)` with port 25565 open; start, stop, restart and
  kill all converged in seconds; an external SIGKILL surfaced as `exited` within one heartbeat.
  The run found a real limit: a 1 GiB container OOM-killed the JVM during world gen, so the heap
  is now `memory − 512 MiB` and 2 GiB boots cleanly.
- **M1 (2026-11-26)** — Fleet spine landed: org CA + panel TLS certificate, single-use
  enrollment tokens (SHA-256 at rest), CSR-signed node certificates, one mTLS WebSocket per
  node (`hello`, `heartbeat`, `hello.ack`), a node registry with
  healthy/unhealthy/disconnected/revoked transitions, revocation enforced on heartbeat and
  reconnect, and bearer-gated `delphi nodes list|token|revoke`. Verified by unit and mTLS
  integration tests plus a live run: two agents healthy, revoking one closed its channel with a
  policy violation, reconnect got 403, the other node stayed healthy.
- **M0.1 (2026-11-26)** — Apache-2.0 adopted (`LICENSE`, ADR 0020, D22), closing the last open
  project decision. M0's database gap is closed: against a live Postgres 19, `init` created a
  0600 config, applied the baseline, and a second run applied nothing; `/readyz` returned 200
  and the agent probe exited 0.
- **M0 (2026-11-26)** — Scaffold landed. One binary with `panel`, `agent`, `init`, `version`;
  TOML config with explicit defaults; embedded hello page at a configurable base path;
  `/healthz` and `/readyz` (Postgres-aware, lazy pool); forward-only migration runner with a
  baseline (`orgs`, `users`); agent readiness probe. Verified with unit tests, `go vet`, a
  clean build, and a live smoke (200/503/404 and the agent probe).
- **docs v1.0** — Final documentation patch before implementation. Fixed: log-bearing tools
  require `:logs`; Paper resolution moved to M3b (M2 uses a pinned jar); mount/offload honesty
  for node-loss backups; artifact upload (`POST /artifacts`) and template/file transfer via
  blobs; boot-check ports assigned by the panel; crash-artifact storage and retention;
  group-scoped role bindings; `org:ai` gate; restore scope; M0/M2 dependency fixes; NTP
  prerequisite; TLS/`Secure` cookies; master-key loss consequence; `delphi init`; Postgres-down
  matrix; unknown capability flags ignored; `proto/` replaced by `internal/wire`. Decision
  index collapsed to the decision log; `SECURITY.md` added; architecture and reference docs
  frozen until code contradicts them.
- **v0.9** — Review fixes. Tool budget reconciled to the fifteen ceiling (three non-tools
  removed); secrets allow an audited one-time reveal; scope verbs defined in the capability
  layer; `create_server` scopes at org level with an `ungrouped` default. Jobs documented as
  at-least-once with a fencing token and a non-replayable `run_command`. OpenAPI is spec-first.
  Added: agent container adoption, panel-owned port allocation, `observed_state`/
  `applied_revision`, plugin rollback history, deletion tombstones, non-reflink `auto-*`
  behaviour, corpus attribution, `server_summary` ownership, console fan-out, panel
  `/metrics`. New ADR 0018 (proactive AI), ADR 0019 (forward-only migrations),
  `reference/api-conventions.md`, `contributing/testing.md`.
- **v0.8** — Simplicity/functionality pass. v1.0 defined for the owner-operator; TUI, networks
  and fleet ops to v1.1; Paper 1.20.5+/Java 21 floor. NATS removed: one mTLS WebSocket per
  agent. Cuts: log4j2 appender, socket proxy, native Anthropic adapter, schema forms,
  template variable schema. AI: nine tools at v1.0, `moderate_player` promoted, probe
  required, redaction at rest. Function: agent-local restart policy, crash artifacts, console
  input through the capability layer, claimed `next_run`, keychain fallback, base-path hosting.
- **v0.7** — Contradictions fixed: jobs are Postgres `SKIP LOCKED` only (no JetStream);
  `docs_chunks` is FTS (no pgvector); S3 offload moved out of v1.0; "logs never leave the node"
  narrowed to log *files*; `Principal.Model` added; tool registry capability-gated. Added the
  desired-state/job claim protocol, the panel↔node blob path, enrollment PKI, signed agent
  updates. M3 split into 3a/3b; rollups moved to M4.
- **v0.6** — Alpha scope recorded: M0–M5 is a milestone-gated vertical slice with explicit exit
  criteria and a cut list. No dates.
- **v0.5** — All ten open decisions resolved; a decision log was introduced (D1–D17). Data model
  gained `server_groups`, per-node `fs_capability`, `org_id` on core tables.
- **v0.4** — Functionality review: native TUI, Velocity networks, editable templates, and
  automation (schedules, restart policy, alerts).
- **v0.3** — Docs in-repo: `docs/` with ADRs, roadmap, reference, runbooks; doubles as the RAG
  corpus. M0 includes promotion.
- **v0.2** — AI subsystem, AI-forward milestones, proactive AI, capability layer, approval
  protocol, injection guardrails.
- **v0.1** — Initial architecture: Go monorepo, NATS, containers, fleet-first, Paper-only.

## Open items

See [`decision-log.md`](decision-log.md#still-open) — kept there so there is one list.
