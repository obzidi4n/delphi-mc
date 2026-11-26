# Configuration

**Status:** current at M0. TOML files; a missing file means the defaults below. There is no
environment-variable magic and no hidden configuration.

| Process | Default path |
|---|---|
| panel | `/etc/delphi/panel.toml` |
| agent | `/etc/delphi/agent.toml` |

Examples live at [`panel.example.toml`](../../panel.example.toml) and
[`agent.example.toml`](../../agent.example.toml).

## Panel

| Key | Default | Notes |
|---|---|---|
| `listen` | `127.0.0.1:8080` | Address the panel binds. Put a TLS reverse proxy in front. |
| `base_path` | `/` | UI prefix; normalized to a leading and trailing slash (`delphi` → `/delphi/`). |
| `database_url` | empty | Postgres DSN. Empty starts the panel but `/readyz` reports unready. |
| `log_level` | `info` | `debug`, `info`, `warn`, `error`. |

The pool connects lazily, so the panel starts (and serves `/healthz` and the page) while
Postgres is down, and becomes ready when it comes back.

## Agent

| Key | Default | Notes |
|---|---|---|
| `panel_url` | `http://127.0.0.1:8080` | Must be an absolute `http(s)` URL. |
| `interval` | `30s` | Probe period; any Go duration string. |

At M0 the agent only probes the panel's `/readyz`. Enrollment, its own identity, and the mTLS
WebSocket channel arrive at M1 ([node enrollment](../operations/node-enrollment.md)).

## First run

```sh
delphi init --config ./panel.toml --database-url 'postgres://delphi:delphi@127.0.0.1:5432/delphi?sslmode=disable'
```

`init` writes the config when it does not exist (refusing to overwrite one), then applies every
pending migration in version order. Migrations are embedded SQL, forward-only, and recorded in
`schema_migrations`; running `init` again is a no-op ([ADR 0019](../decisions/0019-forward-only-migrations.md)).

## Health endpoints

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | The process is alive. Always 200 when serving. |
| `GET /readyz` | 200 when Postgres answers a ping within 2 s, otherwise 503. |

Health endpoints stay at the root even when `base_path` is a subpath; the page and the API
follow `base_path`.
