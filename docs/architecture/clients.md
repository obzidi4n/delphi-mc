# Clients

**Status:** M13 ships a Bubble Tea TUI (`delphi tui`) alongside the server-rendered web
console and CLI. The React/Vite portal below remains the target; the API, WS route, and frame
shapes are the ones it will consume (ADR
[0008](../decisions/0008-native-tui-as-api-client.md)).

All three are consumers of the public API. If a client needs a private endpoint, the API is
wrong. The **spec is the source**: `internal/api/openapi.yaml` is hand-written and reviewed;
Go and TypeScript clients plus request/response DTOs are generated from it at M3a, and
handlers convert those DTOs to capability types.

## Web UI

React + Vite + TypeScript. TanStack Router and Query, Zustand for local UI state, xterm.js
(WebGL) for console, TanStack Virtual for long lists, uPlot for metrics (canvas — Recharts
chokes on live 5k-point series), CodeMirror 6 for configs, Tailwind + shadcn.

- At M4 the console page opens one WebSocket per tab at `<base>ws/servers/<id>`; it carries
  binary console batches plus JSON stats. The topic-multiplexed single-socket design
  (`server:<id>:console`, `server:<id>:stats`, `fleet:alerts`) is the target for the SPA.
- The agent streams every running server's console to the panel; the panel keeps a 5k-line
  ring per server and fans batches out to browsers. Twenty viewers of one server therefore
  share one upstream stream. A slow subscriber is dropped, never allowed to block the agent;
  it reconnects and receives the ring. Sequence gaps are rendered as an explicit line.
- Config editing is a text editor with validation, diff and revision history — no
  JSON-Schema form generation in v1.0. Schema forms are a v1.1 candidate. The editor parses
  `.json`, `.yaml`/`.yml`, and `.toml` on open and edit, shows parser errors with a line
  reference, and disables Save while the file is invalid. JSON is indented on open and save;
  YAML/TOML keep comments and ordering. `server.properties` gets a versioned key reference
  with allowed values and defaults; invalid values block saving, while removed/unknown keys
  are warnings so existing files remain editable. `.dat`/`.nbt` files open in a read-only
  NBT tree with compression metadata and a raw download; editing NBT is not exposed.
- The fleet page groups servers by their server groups, with an `Ungrouped` section for
  servers outside a group; nodes remain a separate table. It explains degraded operation: a
  banner names offline nodes and a recent panel restart, states that servers keep running and
  restarts are agent-local, and the node table shows status and last-seen. Actions stay
  unavailable while the panel is down; there is no offline write queue.
- The Nodes page generates one-time enrollment tokens, downloads `ca.crt`, and lists node
  health. The Create server page selects a template and node, applies optional memory/CPU/port
  overrides, requires the EULA, and opens the new server console.
- The Templates page lists built-in and org templates, shows the full JSON body, and edits
  org-owned templates in place with revision increments. The `files` map is also exposed as
  per-file multiline sub-editors, so long `server.properties` content is not a single escaped
  JSON line. Built-ins are read-only; **Save as copy** creates an editable org template.
  Validation errors are returned before a write.
- An audit page lists the most recent 200 mutations (seq, time, actor, via, action, target);
  diff payloads remain on the API.
- M8 ships a server-scoped file manager page: directory browser, monospace editor with
  Ctrl+S, revision list, unified diff, and restore. The SPA target keeps virtualization and a
  richer editor; the API and revision model are the same. Small writes go
  inline through `write_file`; uploads above a small threshold use `POST /artifacts` and the
  blob path, so no file body rides a WebSocket frame.
- Serves under a configurable base path (`/delphi/`) with a matching WS path, so it can sit
  behind an existing reverse proxy without owning a hostname.

### Premium console checklist

What M4 delivers in the vanilla page:

- ANSI SGR colours (16-colour) with bold, smooth follow toggling, Ctrl+F search with hit
  count and jump, pause that keeps buffering, copy all, clear, auto-reconnect with backoff,
  connection and server-state badges, live CPU/memory from the stats stream, and power
  buttons (start/restart/stop/kill) through the API.
- Gaps are explicit; the DOM keeps the newest 5000 lines.
- The bridge sidebar shows TPS, online players (with kick/ban), chat and join/quit events, and
  the root command tree, all over the same socket. Moderation calls
  `POST /servers/{id}/moderation` and surfaces the plugin's result event.
- The console command bar sends one command through the live Paper bridge. It has ↑/↓ history
  and autocomplete from the bridge's root command tree; the server and scope checks are
  enforced by `POST /api/v1/servers/{id}/command`.
- The xterm/WebGL renderer, timestamp toggle, level filter, and richer command completion
  remain SPA targets.
- Timestamp toggle, level filter, and **structured events rendered as events** (chat, join,
  death) rather than raw lines — the bridge paying off.
- Command input with ↑ history, autocomplete from the plugin's command tree, and one-line
  validation — submitted through `capability.Servers.Command`, so it is authorized and
  audited like every other mutation.
- Snippets/macros with variables (`{{server}}`, `{{player}}`).
- Multi-server tail in tabs or split panes.
- Reconnect with buffer replay from the agent ring buffer; "jump to first error" on crash.

### Plugins page

`/servers/{id}/plugins` lists the managed plugin set and offers a Modrinth/Hangar
catalog search. Installing resolves the newest Paper-compatible build for the
server's Minecraft version, downloads it into the artifact store, and stages it
on the node; removing a plugin does the same in reverse. A running server does
not restart automatically; the owner restarts when ready to load or unload the
change. Applying updates is the explicit restart/boot-check action.

The page also accepts a GitHub repository or release URL and installs the latest
release asset, and a Jenkins job URL to install the last successful build's jar
artifact. Both forms accept an optional plugin name; the source URL is retained
for later update checks. It has a manual Spigot jar upload form for resources without a public
API. Uploaded jars are pinned and marked manual, so they are not checked for
remote updates. The page checks provider-backed plugins for updates on load and
every five minutes, shows current vs available versions, and applies all updates
through the guarded boot-check path.
For unattended operation, the page's cron form creates a schedule with action
`updates.apply`; the scheduler performs detection and download on each run.

## CLI

`delphi init` is the one-time first run (config + schema). Then
`delphi <resource> <verb>`: servers, nodes, plugins, backups, ai — generated from the same
spec as the SDK, so parity is enforced, not promised.

- JSON output by default when stdout is not a TTY, so it composes with `jq`.
- `delphi auth login` uses a device-code flow; `--token` (stdin or `DELPHI_TOKEN`) is the
  headless/CI fallback.
- Tokens live in the OS keychain or a `0600` file; never in shell history.
- Present from alpha (M3a) so the public API is exercised by a real consumer from the start.

## TUI (v1.1)

`delphi tui` — Bubble Tea + lipgloss, k9s-class keys, no mouse required. Implemented at M13:
token auth (including the console WebSocket), fleet → server navigation, live console replay,
power/backups/plugins/updates, and an AI pane with streaming and inline approvals. Keys are in
[`../reference/tui-keybindings.md`](../reference/tui-keybindings.md).

- Fleet → node → server → console navigation; live console pane with the same ring-buffer
  replay as the web UI.
- Power actions, player list, plugin list, metric sparklines, log search.
- **AI pane** — the same conversation API, streamed, with inline approval prompts.
- Degrades gracefully over SSH and narrow terminals. Shares `internal/client` with the CLI;
  keybindings are documented at v1.1 in `../reference/tui-keybindings.md`.

## What the clients never do

- Never mutate through a side channel. Console input, file writes and power actions all go
  through the capability layer.
- Never render a secret. Secrets have set/rotate affordances only.
- Never hide a degraded state: a missing capability, a disabled AI surface, and an
  unavailable boot-check are all visible, never silent.
