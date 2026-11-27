# TUI keybindings

`delphi tui --panel https://panel.example --token "$DELPHI_TOKEN" [--ca ca.crt]`

The terminal client is a thin consumer of the public API and the console WebSocket. It
authenticates with an API token; a private CA can be supplied with `--ca`. The admin token can
read the fleet and mutate servers, but the AI pane needs a **user-scoped** API key (conversations
belong to a user); the panel returns `user_token_required` otherwise.

## Fleet

| Key | Action |
|---|---|
| `↑` / `k`, `↓` / `j` | Move the selection |
| `enter` | Open the selected server |
| `r` | Refresh the fleet |
| `q` | Quit |

## Server

| Key | Action |
|---|---|
| `esc` | Back to the fleet (closes the console stream) |
| `s` / `x` / `r` / `k` | Start / stop / restart / kill |
| `b` | Create an encrypted snapshot |
| `p` | Show managed plugins and available updates |
| `u` | Apply available plugin updates (restart + panel boot-check) |
| `a` | Focus the AI input |
| `y` / `n` | Approve / reject a pending AI plan |
| `q` | Quit |

## AI input

| Key | Action |
|---|---|
| `enter` | Send the prompt |
| `esc` | Leave the input without sending |
| `backspace` | Delete a character |

Streamed deltas, tool calls, citations, and approval summaries appear in the operator pane.
Approval resumes the same SSE stream as the web UI. The TUI never mutates through a side
channel: power/backups/updates use the capability API and the token's scopes.

## Not implemented yet

Inline file editing (`$EDITOR` handoff), console command input, log search, metric
sparklines, and multi-server panes. The console shows the live stream and gap markers. The
layout degrades to a single column on narrow terminals.
