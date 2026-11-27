# DelphiMC

DelphiMC is a control panel for Minecraft: Java Edition servers. It helps you create servers,
run them in containers, watch the console, install plugins, take backups, and restart crashed
servers automatically.

You can run everything on one computer, or put the panel on one machine and the actual game
servers on other machines ("nodes"). DelphiMC also includes an optional AI assistant that can
answer questions about your servers and propose actions for your approval.

DelphiMC is under active development. The steps below are for the current build.

## What it does

- **Create servers from templates.** Start with Paper (or Velocity for proxy networks) and a
  sensible set of files, then adjust memory, ports, and settings.
- **Live console and controls.** Start, stop, restart, kill, read the console, edit files, and
  see CPU/memory for each server.
- **Install and update plugins.** Pull plugins from Modrinth, Hangar, or GitHub, and update
  them with a restart and automatic rollback if the server fails to boot.
- **Backups and restores.** Make encrypted snapshots, keep several copies, and restore a
  server without using another tool.
- **Scheduled actions and alerts.** Schedule restarts or backups, and send webhooks or email
  when a server crashes repeatedly or goes down.
- **Keeps running when the panel is down.** Each machine has a small helper (the "agent") that
  keeps servers running and restarts crashed ones even if the panel is offline.
- **A terminal UI and CLI.** Run `delphi tui` for a keyboard-driven terminal interface, or
  script the API/CLI.
- **Multiple machines and proxy networks.** Add more nodes, choose where servers go, and put
  several Paper servers behind a Velocity proxy on a private container network.
- **Optional AI helper.** Ask questions about your fleet, or ask it to propose an action. It
  must ask for approval before changing anything.

DelphiMC is not a Minecraft client, world editor, or modpack manager. It does not support
Forge, Fabric, modpacks, or editing worlds directly. It is also not a hosting company: you
run it on your own computer or servers.

## What you need

For a basic single-computer setup:

- **Go 1.27 or newer** to build DelphiMC.
- **PostgreSQL 16 or newer.** DelphiMC stores its configuration and history here.
- **Podman 4+ or Docker.** This is what actually runs the Minecraft servers. Podman is
  recommended. On macOS, install Podman and start its virtual machine once.
- **About 2 GB of free disk space** for the panel and a small server, plus a few hundred MB
  per additional server. Give the machine enough RAM for the servers you run (a server with
  `--memory-mb 2048` needs at least a few GB free).
- An open game port (usually **25565**) for players to connect.

If the panel and the game servers are on different machines, install the DelphiMC binary, a
container runtime, and the agent on each game machine. The panel only needs PostgreSQL.

## QuickStart: one computer

The commands below are for Linux or macOS. Run them from the folder containing the DelphiMC
source code.

### 1. Build DelphiMC

```sh
make build
```

This creates `./bin/delphi`. The same binary is the panel, the agent, the CLI, and the TUI.

### 2. Create the database

Create a PostgreSQL user and database. Change `change-me` to a real password.

```sh
psql postgres -c "CREATE ROLE delphi LOGIN PASSWORD 'change-me';"
psql postgres -c "CREATE DATABASE delphi OWNER delphi;"
```

If your PostgreSQL requires a different administrator command, use the equivalent for your
installation.

If the `delphi` database already exists and is owned by another user, make `delphi` its owner
and grant access to anything already inside it. Run these as a PostgreSQL administrator:

```sh
psql -d postgres -c "ALTER DATABASE delphi OWNER TO delphi;"
psql -d delphi -c "GRANT CREATE, USAGE ON SCHEMA public TO delphi;"
psql -d delphi -c "GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO delphi;"
psql -d delphi -c "GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO delphi;"
```

Without this, `init` fails with `permission denied for schema public` or
`permission denied for table schema_migrations`.

### 3. Initialize DelphiMC

```sh
./bin/delphi init --config ./panel.toml \
  --database-url 'postgres://delphi:change-me@127.0.0.1:5432/delphi?sslmode=disable' \
  --data-dir ./.delphi
```

This does four things:

1. Writes `panel.toml`.
2. Creates a private certificate authority for panel-to-agent traffic in `./.delphi/pki/`.
3. Creates the database tables.
4. Creates the first owner account and prints a **password** and an **API token**.

**Save both printed values now.** They are shown only on the first run. The password is for
signing in to the web panel; the API token is for the command-line tools. The owner email is
`owner@delphi.local`.

If the database already contained a DelphiMC owner, `init` will not print a new password or API
token. In that case you can either use an existing API token, or set a panel-wide admin token:

```sh
TOKEN=$(openssl rand -hex 32)
# Put this in panel.toml, then restart the panel:
# admin_token = "TOKEN"
# Use the same value for CLI commands:
export DELPHI_API_TOKEN="$TOKEN"
```

The panel-wide token is enough for CLI and API server management. To sign in to the web pages
you still need a user account; after starting the panel you can create one with the token:

```sh
curl -k -X POST https://127.0.0.1:8080/api/v1/users \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","role":"admin"}'
```

The response contains the new user's one-time password.


The default panel address is `https://127.0.0.1:8080/`. You can change it later in
`panel.toml` (`listen`, `data_dir`, `base_path`, and optional `[ai]` settings).

### 4. Start the panel

Use a second terminal for this; keep it running.

```sh
./bin/delphi panel --config ./panel.toml
```

Open <https://127.0.0.1:8080/> in a browser. The certificate is self-signed for local use, so
the browser will show a warning for `127.0.0.1`. Sign in with:

- Email: `owner@delphi.local`
- Password: the password printed by `init`

Do not expose this address directly to the internet. It binds to localhost by default.

### 5. Connect the machine that will run servers

The panel manages game servers through an **agent** running on each node. For this basic setup,
the node is the same computer.

In your original terminal, create an enrollment token:

```sh
export DELPHI_API_TOKEN='the API token printed by init'

./bin/delphi nodes token \
  --panel https://127.0.0.1:8080 \
  --ca ./.delphi/pki/ca.crt
```

Copy the printed token into the next command. Then enroll the agent:

```sh
export DELPHI_ENROLL_TOKEN='the token printed above'

./bin/delphi agent enroll \
  --panel https://127.0.0.1:8080 \
  --ca ./.delphi/pki/ca.crt \
  --name local \
  --state-dir ./.delphi/agent-state
```

Create an agent configuration file, `agent.toml`:

```toml
panel_url = "https://127.0.0.1:8080"
interval = "10s"
state_dir = "./.delphi/agent-state"
data_dir = "./.delphi/agent-data"
container_runtime = "auto"
```

Start the agent in another terminal and keep it running:

```sh
./bin/delphi agent run --config ./agent.toml
```

Return to the panel web page. The node should appear as **healthy**. The machine is now ready
to host servers.

### 6. Create your first server

List nodes to copy the node ID:

```sh
./bin/delphi nodes list \
  --panel https://127.0.0.1:8080 \
  --ca ./.delphi/pki/ca.crt
```

Create a Paper server from the built-in Survival template. Replace `<NODE_ID>` with the ID
from the list. The `--eula` flag means you accept the
[Minecraft EULA](https://aka.ms/MinecraftEULA).

```sh
./bin/delphi servers create \
  --name my-first-server \
  --node <NODE_ID> \
  --template builtin-survival \
  --memory-mb 2048 \
  --eula --start \
  --panel https://127.0.0.1:8080 \
  --ca ./.delphi/pki/ca.crt
```

The agent downloads Paper, creates the server files, and starts the container. Watch progress in
the web console. When the server is running, the panel shows its port (usually **25565**).

### 7. Join the server

From the same computer, connect Minecraft to `localhost:25565`. From another computer, use the
IP address of the machine running the agent and the port shown in the panel, and make sure that
port is reachable through your firewall.

If the port is already in use, create the server with a different `--port`.

## Everyday use

Open the web panel for the fleet view, live console, power buttons, file editor, audit log, and
the optional AI page. The commands below assume `DELPHI_API_TOKEN` is still set (see Step 5);
set it again in a new terminal if needed. The CLI is useful for scripting:

```sh
# Server list
./bin/delphi servers list --panel https://127.0.0.1:8080 --ca ./.delphi/pki/ca.crt

# Start, stop, restart, kill
./bin/delphi servers start <SERVER_ID> --panel https://127.0.0.1:8080 --ca ./.delphi/pki/ca.crt
./bin/delphi servers stop  <SERVER_ID> --panel https://127.0.0.1:8080 --ca ./.delphi/pki/ca.crt
```

The terminal interface is a keyboard-driven alternative to the web pages:

```sh
./bin/delphi tui \
  --panel https://127.0.0.1:8080 \
  --ca ./.delphi/pki/ca.crt \
  --token "$DELPHI_API_TOKEN"
```

For the AI page, you need an OpenAI-compatible model server (for example Ollama). Add an `[ai]`
section to `panel.toml`, restart the panel, then use the AI page:

```toml
[ai]
base_url = "http://127.0.0.1:11434/v1"
model = "qwen2.5:3b"
```

The AI assistant can read server state and propose actions. It cannot change anything without
an approval click. `base_url` is the API root, not the full completion URL: for OpenRouter use
`https://openrouter.ai/api/v1`, not `.../chat/completions`.

## Optional extras

- **More machines:** install the binary and container runtime on another machine, enroll its
  agent with a new token, then pick that node when creating a server. The API can also choose a
  healthy node automatically when no node is specified.
- **Proxy networks:** create a Velocity network from the API; the panel creates the proxy and
  its backends, gives them a private container network, and generates the configuration.
- **Backups:** available through the API, the terminal UI (`b`), and the AI tools. Scheduled
  actions are available through the API and the AI tools. Backups use an encryption key in
  `./.delphi/backup.key`. **Back up that key file.** Without it, the backups cannot be read,
  even by DelphiMC.
- **Reverse proxy:** set `base_path = "/delphi/"` in `panel.toml` if the panel should live
  behind an existing web server. Keep the panel itself on localhost.

## Troubleshooting

**The panel says “unready” or `/readyz` returns 503.** PostgreSQL is not reachable. Check the
DSN in `panel.toml`, the database service, the user, and the password.

**The node never becomes healthy.** Check that the node can reach the panel URL, that the CA
file is correct, and that the enrollment token was used before it expired. Run the agent in the
foreground to see its log. Enrollment tokens can be created again with `nodes token`.

**The first server will not start.** Check the console in the web panel. Common causes are a
missing `--eula`, too little memory, or a port already in use. Also verify the container
runtime works (`podman run --rm hello-world`, or `docker run --rm hello-world`).

**The browser warns about the certificate.** This is expected for the local self-signed
certificate on `127.0.0.1`. For a public deployment, put the panel behind a reverse proxy with a
normal TLS certificate, and do not expose the panel's own port.

**Players cannot connect.** They connect to the node's game port, not to the panel port. Check
the port shown in the panel and open it in the node's firewall. On macOS, make sure the Podman
virtual machine is running and its published ports are reachable.

**I lost the owner password or API token.** The password is printed only once by `init`. Create
a new user in the panel (or a new API key) from an account that still has owner/admin access.
If no account is available, the installation needs to be recovered administratively; see the
operations docs.

## Security

The panel binds to `127.0.0.1` by default and uses a self-signed certificate for local use. Keep
it that way unless you put it behind HTTPS and authentication you trust. Keep the printed owner
password, API token, `panel.toml`, the agent certificates, and `backup.key` private.

Report security issues as described in [SECURITY.md](SECURITY.md).

## Learn more

- [Install and host requirements](docs/operations/install.md)
- [How the pieces fit together](docs/architecture/overview.md)
- [Incident runbook](docs/operations/incident-runbook.md)
- [TUI keybindings](docs/reference/tui-keybindings.md)
- [All documentation](docs/README.md)

## License

Apache License 2.0 — see [LICENSE](LICENSE). Paper, plugins, and other third-party assets keep
their own licenses.
