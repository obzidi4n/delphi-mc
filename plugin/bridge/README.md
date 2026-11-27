# DelphiBridge (v0)

A Paper plugin embedded in the agent. It publishes structured state and consumes moderation
commands through files shared with the agent:

- `plugins/DelphiBridge.jar` — installed by the agent before each start when its hash changed.
- `<server>/.delphi-bridge/state.json` — atomically replaced every 0.5 s:
  `{seq, tps, players[], commands[], events[], at}`.
- `<server>/.delphi-bridge/cmd/cmd-<id>.json` — one `{id, action: kick|ban, player, reason}`;
  the plugin deletes it after executing on the main thread and emits a `moderation` event.

Build with `make bridge` (needs a JDK and network for the pinned Paper/Adventure/Gson jars);
the build script installs the jar into `internal/agent/bridge/` for `go:embed`. The built jar
is committed so `go build` does not need a JDK.
