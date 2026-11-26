# Clients

**Status:** web UI and CLI target v1.0; TUI at v1.1 (ADR
[0008](../decisions/0008-native-tui-as-api-client.md)).

All three are consumers of the public API. If a client needs a private endpoint, the API is
wrong. The **spec is the source**: `internal/api/openapi.yaml` is hand-written and reviewed;
Go and TypeScript clients plus request/response DTOs are generated from it at M3a, and
handlers convert those DTOs to capability types.

## Web UI

React + Vite + TypeScript. TanStack Router and Query, Zustand for local UI state, xterm.js
(WebGL) for console, TanStack Virtual for long lists, uPlot for metrics (canvas — Recharts
chokes on live 5k-point series), CodeMirror 6 for configs, Tailwind + shadcn.

- **One WebSocket per browser tab**, topic-multiplexed (`server:123:console`,
  `server:123:stats`, `fleet:alerts`) with server-side rate limits.
- **The panel subscribes upstream once per server, not once per viewer.** Twenty tabs on one
  console share one `console.subscribe` to the agent; the panel fans frames out to topic
  subscribers and applies backpressure per subscriber (a slow tab drops oldest, never the
  agent). A subscriber joining mid-stream passes `since_seq`; the panel serves replay from the
  agent ring buffer and marks an explicit gap when the sequence is gone.
- Config editing is a text editor with validation, diff and revision history — no
  JSON-Schema form generation in v1.0. Schema forms are a v1.1 candidate.
- Virtualized file manager; the diff view is shared with AI approval cards. Small writes go
  inline through `write_file`; uploads above a small threshold use `POST /artifacts` and the
  blob path, so no file body rides a WebSocket frame.
- Serves under a configurable base path (`/delphi/`) with a matching WS path, so it can sit
  behind an existing reverse proxy without owning a hostname.

### Premium console checklist

The headline claim, made concrete:

- True-colour xterm.js with the WebGL renderer, smooth scroll, Ctrl+F search, select/copy
  that does not fight the browser.
- Timestamp toggle, level filter, and **structured events rendered as events** (chat, join,
  death) rather than raw lines — the bridge paying off.
- Command input with ↑ history, autocomplete from the plugin's command tree, paste safety for
  multi-line commands — submitted through `capability.Servers.Command`, so it is authorized
  and audited like every other mutation.
- Snippets/macros with variables (`{{server}}`, `{{player}}`).
- Multi-server tail in tabs or split panes.
- Reconnect with buffer replay from the agent ring buffer; "jump to first error" on crash.

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

`delphi tui` — Bubble Tea + lipgloss, k9s-class keys, no mouse required.

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
