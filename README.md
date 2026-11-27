# DelphiMC

A control plane for Minecraft (Java Edition) servers: server, jar and plugin management with
automated updates, templates, backups, scheduled automation, a first-party Paper bridge
plugin, and an AI operator — behind a web UI and a scriptable CLI, in one binary plus
Postgres.

**Status: M11 automation.** One binary with `panel`, `agent`, `nodes`, `init`, and `version`.
Servers come from templates, boot in hardened containers, stream console/metrics, and run a
self-installed bridge plugin. Modrinth/Hangar/GitHub plugins are resolved into a sha256
artifact store, installed on create, updated with a guarded restart and rollback, and
garbage-collected. Quiesced snapshots are encrypted at rest with a per-install key, retained
per server, and restorable with reconcile. Cron schedules trigger power/backup actions; the
agent restarts crashed servers from its last desired state even when the panel is down; and
crash alerts reach webhook/SMTP channels. The panel has a file manager with
revisions/diff/restore, an AI operator with read tools and approval-gated actions, and a
scriptable API/CLI.

## Try it

```sh
make build
./bin/delphi init --config ./panel.toml \
  --database-url 'postgres://delphi:delphi@127.0.0.1:5432/delphi?sslmode=disable' \
  --data-dir ./.delphi
./bin/delphi panel --config ./panel.toml
```

`init` prints the admin token once and writes the panel CA to `.delphi/pki/ca.crt`. Then:

```sh
./bin/delphi nodes token  --panel https://127.0.0.1:8080 --ca ./.delphi/pki/ca.crt   # admin token via DELPHI_ADMIN_TOKEN
./bin/delphi agent enroll --panel https://127.0.0.1:8080 --ca ./.delphi/pki/ca.crt \
  --name node-a --state-dir ./.delphi/node-a                                        # token via DELPHI_ENROLL_TOKEN
./bin/delphi agent run    --config agent.example.toml
./bin/delphi nodes list   --panel https://127.0.0.1:8080 --ca ./.delphi/pki/ca.crt
```

The page is at `https://127.0.0.1:8080/` (or your `base_path`), `/healthz` is liveness, and
`/readyz` reports Postgres. See [`docs/contributing/dev-setup.md`](docs/contributing/dev-setup.md)
for the full local flow.

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

Apache License 2.0 — see [`LICENSE`](LICENSE). Third-party assets (Paper, plugins, their docs)
keep their own licenses. The name and logo are not licensed (Apache-2.0 §6); there is no
`NOTICE` file yet.

## Contributing

Read [`docs/contributing/conventions.md`](docs/contributing/conventions.md) first, then
[`docs/contributing/testing.md`](docs/contributing/testing.md) for the harness the alpha
depends on. Docs change in the same PR as the behavior they describe.
