# DelphiMC

A control plane for Minecraft (Java Edition) servers: server, jar and plugin management with
automated updates, templates, backups, scheduled automation, a first-party Paper bridge
plugin, and an AI operator — behind a web UI and a scriptable CLI, in one binary plus
Postgres.

**Status: M0 scaffold.** One binary with `panel`, `agent`, `init`, and `version`;
configuration; an embedded page; health and readiness; forward-only migrations. No server
management yet — that starts at M1. The architecture lives in [`docs/`](docs/README.md); the
sequence lives in [`docs/plan/roadmap.md`](docs/plan/roadmap.md).

## Try the scaffold

```sh
make build
./bin/delphi panel --config panel.example.toml
```

The page is at `http://127.0.0.1:8080/` (or your `base_path`), `/healthz` is liveness, and
`/readyz` reports Postgres — without a database the panel still serves and reports unready.
`./bin/delphi agent --panel http://127.0.0.1:8080 --once` runs a single readiness probe.
Postgres is only required for `delphi init` and readiness.

## What v1.0 is

- For the technical owner-operator: 1–10 self-hosted servers, single org.
- Servers as rootless Podman containers (Docker supported, documented as root-equivalent).
- Safe jar/plugin updates with a staging boot-check on reflink-capable filesystems.
- Encrypted backups with retention, schedules, crash restart policy, and alerts.
- An AI operator that answers only from tool results and mutates only through approval cards.
- A web UI, plus a CLI generated from the public OpenAPI spec.

v1.1 adds Velocity proxy networks, a native TUI, and fleet operations (placement, drain,
signed agent self-update), followed by S3 backup offload, dev environments, and databases.

## Not in scope

World/NBT editing, hot plugin reload, live migration, Forge/Fabric/modpacks, Folia,
BungeeCord, an event-driven rules engine, embeddings or vector search, a browser IDE.

## Where to read

| Question | Doc |
|---|---|
| How does it fit together? | [`docs/architecture/overview.md`](docs/architecture/overview.md) |
| What ships when? | [`docs/plan/roadmap.md`](docs/plan/roadmap.md) |
| Why was X decided? | [`docs/plan/decision-log.md`](docs/plan/decision-log.md) |
| How do I run it? | [`docs/operations/install.md`](docs/operations/install.md) (target, M5) |

## Security

See [`SECURITY.md`](SECURITY.md) for reporting and scope.

## License

Not yet chosen. Until a `LICENSE` file is committed, the default is all rights reserved — see
open item 10 in [`docs/plan/decision-log.md`](docs/plan/decision-log.md#still-open).

## Contributing

Read [`docs/contributing/conventions.md`](docs/contributing/conventions.md) first, then
[`docs/contributing/testing.md`](docs/contributing/testing.md) for the harness the alpha
depends on. Docs change in the same PR as the behavior they describe.
