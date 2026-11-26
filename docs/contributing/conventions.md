# Conventions

**Status:** current · enforced from M0.

## Repo layout

```
cmd/delphi/              # panel | agent | resource commands (tui at v1.1)
internal/
  capability/            # THE typed API surface (HTTP, CLI, AI)
  client/                # generated API client (CLI; TUI/SDK later)
  api/                   # hand-written OpenAPI spec (source of truth), DTOs, thin handlers
  agent/
    supervisor/          # container lifecycle, port pool, limits
    console/             # tail, ring buffer, batching, search
    metrics/             # cgroup v2 readers
    files/               # atomic writes, path sandboxing
    artifacts/           # CAS, reflink/hardlink
    snapshot/            # reflink, zfs clone, tar fallback
    bridge/              # unix-socket server for the Java plugin
    network/             # container networks, ports (v1.1)
    selfupdate/          # signed two-slot updater (v1.1)
  panel/
    auth/                # sessions, 2FA, tokens
    rbac/                # scopes, role bindings
    audit/               # append-only records
    realtime/            # WS hub, topics, batching
    fleet/               # node registry, heartbeats, job workers
    schedule/            # cron claims, alerts, windows
  ai/                    # one package first: provider, tools, context, loop, guard, store
  mc/
    versions/            # Paper now, Velocity at v1.1
    plugins/             # Modrinth, Hangar, GitHub
    java/                # 21/25 images
    template/            # built-ins, instantiation
  store/                 # pgx + sqlc; embedded forward-only SQL migrations
  wire/                  # frames, envelope, console batching (Go; spec in docs)
web/                     # React + Vite SPA (embedded)
plugin/                  # Java bridge plugin (Gradle, Paper API)
images/                  # per-JDK runtime images (Dockerfile per Java version)
docs/                    # this tree; also the AI RAG corpus
```

Packages are split when a cycle forces it, not in advance. `ai/` and `mc/versions/` have
already collapsed for this reason. Directories marked v1.1 are placeholders: nothing is
created there before its milestone.

## Code rules

- **All mutations go through `capability`.** A handler, CLI command or AI tool that writes
  state directly is a bug, not a shortcut.
- **The OpenAPI document is spec-first.** `internal/api/openapi.yaml` is hand-written and
  reviewed; Go/TS clients and DTOs are generated from it. Handlers are thin, convert DTOs to
  capability types, and never become the second source of truth. The capability layer is the
  behavior; the spec is the wire shape.
- **No web framework.** `net/http` with Go 1.22 routing; one middleware chain, no reflection
  routers.
- **Return `Job` for anything long.** No handler blocks on a container operation.
- **Never log a secret.** Redaction is applied at the boundary (`ai/guard` for prompts,
  structured-log fields for everything else).
- **Errors carry the operation, not the wire:** `fmt.Errorf("stop server %s: %w", id, err)`.
- **Timestamps are UTC in the database**, rendered in the schedule's timezone.
- **Tests** target behavior that can break a user: the capability contract, the reconciler,
  the console batcher, the approval loop. Do not test wiring or defaults. Layers and the e2e
  host are defined in [`testing.md`](testing.md).

## Doc rules

- One concept per page; under ~300 lines; split rather than grow.
- Every page states its status (`current`, `target: Mx`, `v1.1`) when it describes something
  unshipped.
- Docs change in the same PR as the behavior.
- Duplication is a bug: link instead of restating.
- ADRs are append-only; supersede with a new number.

## Commits and PRs

- One logical change per commit; the message says what changed and why in the imperative.
- A PR that changes behavior updates the doc that describes it, or says why not.
- Anything that breaks the compatibility window (`panel N ↔ agent N-1…N`) needs an ADR.

## M0 bootstrap checklist

1. `go.mod`, layout above, `cmd/delphi` with `panel` and `agent` subcommands.
2. Postgres connection, baseline migration applied at startup, sqlc config.
3. Config file (`/etc/delphi/panel.toml`) parsing with explicit defaults.
4. `embed.FS` wired for `web/`, producing a real hello page.
5. This docs tree in the repo, with the roadmap and decision log as the only churning pages.
