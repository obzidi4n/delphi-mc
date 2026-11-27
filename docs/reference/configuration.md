# Configuration

**Status:** current at M1. TOML files; a missing file means the defaults below. There is no
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
| `listen` | `127.0.0.1:8080` | Address the panel binds. Its host becomes the certificate SAN, so use the name agents dial. |
| `base_path` | `/` | Prefix for pages, WebSockets, the API, and agent endpoints; normalized to a leading and trailing slash (`delphi` → `/delphi/`). |
| `database_url` | empty | Postgres DSN. Empty starts the panel but `/readyz` reports unready. |
| `log_level` | `info` | `debug`, `info`, `warn`, `error`. |
| `data_dir` | `/var/lib/delphi` | Holds `pki/` (CA, server certificate) and node data. |
| `admin_token` | empty | Break-glass bearer token for `/api/v1`. Optional: API keys minted for a user are the normal credential. |
| `tls` | `true` | Serve TLS with the org CA's server certificate. The agent channel requires TLS. |
| `ai.base_url` | empty | OpenAI-compatible API root (for example `http://127.0.0.1:11434/v1`). DelphiMC appends `/chat/completions`; do not include that path. For OpenRouter use `https://openrouter.ai/api/v1`. Empty disables the AI surface. |
| `ai.model` | empty | Model name; required when `ai.base_url` is set. |
| `ai.api_key` | empty | Bearer key for the provider, if it needs one. |

The pool connects lazily, so the panel starts (and serves `/healthz` and the page) while
Postgres is down, and becomes ready when it comes back.

## Agent

Run **one agent per host or VM**. The agent manages all server containers and data directories
on that node; it is not a per-server process. Groups organize servers and can span nodes. See
[`../architecture/nodes.md`](../architecture/nodes.md).

| Key | Default | Notes |
|---|---|---|
| `panel_url` | `https://127.0.0.1:8080` | Must be `https`: the channel is mutually authenticated. |
| `interval` | `30s` | Heartbeat period; any Go duration string. |
| `state_dir` | `/etc/delphi` | Holds `ca.crt`, `agent.crt`, `agent.key` written by enrollment. |
| `data_dir` | `/var/lib/delphi` | Server directories, jars, crash artifacts; resolved to an absolute path. |
| `container_runtime` | `auto` | `auto` picks podman, then docker; or force one. |
| `update_public_key` | empty | Base64 ed25519 key; agent accepts self-updates only with a valid signature. |

Enrollment is one-time and happens before `agent run`:

```sh
DELPHI_ENROLL_TOKEN=… delphi agent enroll --panel https://127.0.0.1:8080 \
  --ca /path/to/ca.crt --name node-a --state-dir /etc/delphi
```

The agent then dials `wss://<panel>/agent/ws` with its certificate, sends `hello`, and sends a
heartbeat every `interval`. The panel marks it healthy on connect, unhealthy after 90 s without
a heartbeat, and closes the channel within one heartbeat of revocation.

On connect, and after every server mutation, the panel sends the node's full desired server set.
The agent converges it: writes `eula.txt` (only when the operator accepted), a default
`server.properties`, downloads the pinned `paper.jar` (sha256 verified), and starts/stops/restarts
the container. Observed state is reported back per server. An agent restart adopts running
containers instead of restarting them.

## First run

```sh
delphi init --config ./panel.toml \
  --database-url 'postgres://delphi:delphi@127.0.0.1:5432/delphi?sslmode=disable' \
  --data-dir ./.delphi --listen 127.0.0.1:8443
```

`init` writes the config when it does not exist (refusing to overwrite one), creates the org CA
and the panel's TLS certificate, and applies every pending migration in version order. Migrations
are embedded SQL, forward-only, recorded in `schema_migrations`; running `init` again is a no-op
([ADR 0019](../decisions/0019-forward-only-migrations.md)).

On a database without an owner, `init` also seeds `owner@delphi.local`, prints its generated
password once, and mints a bootstrap API key with `*`. Both are printed once; sign in at `/` to
create named users and keys. The CLI reads `DELPHI_API_TOKEN` (or the legacy
`DELPHI_ADMIN_TOKEN`) when `--token` is absent.

## Health endpoints

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | The process is alive. Always 200 when serving. |
| `GET /readyz` | 200 when Postgres answers a ping within 2 s, otherwise 503. |

Health endpoints stay at the root even when `base_path` is a subpath; pages, WebSockets,
`/api/v1`, and `/agent/*` follow `base_path`. A reverse proxy may pass the prefix through
unchanged (`/delphi/...`) or strip it while rewriting links is impossible — pass it through.
Agents must be configured with a `panel_url` that includes the prefix; enrollment composes
`<panel_url>/agent/enroll`.

## AI

```toml
[ai]
base_url = "http://127.0.0.1:11434/v1"   # Ollama, vLLM, LM Studio, OpenAI, OpenRouter…
model = "qwen2.5:3b"
api_key = ""                              # empty for local providers
```

With no `base_url`/`model` the AI page reports "not configured" and the API returns 503.
A model that rejects tool definitions is retried without tools and its answers are labelled
chat-only on the page.
