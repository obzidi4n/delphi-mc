# Networks (Velocity)

**Status:** M14 implemented for **legacy forwarding**. Modern forwarding, the Velocity
bridge plugin, live drain, and network deletion remain deferred. ADR
[0007](../decisions/0007-velocity-networks-in-v1-1.md).

A proxy is not just another server, so a network is a first-class entity:

```
Network { id, name, proxy_server_id, mode: modern|legacy }
Server  { ..., network_id, role: proxy|backend, order }
```

## Isolation by construction

- **One container network per network** (`delphi-net-<network-id>`). The proxy publishes the
  public port; backends publish **no host ports** and are reachable only from the proxy.
- Containers get hostname-safe aliases (`delphi-s-…`; server ids contain `_`, which Velocity
  rejects as an invalid hostname). Proxy configs use aliases, not raw container names.
- Verified live: the proxy listens on its published port; both backends are running with empty
  `podman port` output; `getent hosts` from the proxy resolves the backend aliases.
- This is isolation by construction, not by firewall rules an operator can misconfigure.

## Generated configuration, never hand-edited

Both sides are derived from one source: server list, try order, forwarding secret, forwarding
mode.

- Velocity: `velocity.toml` (`config-version = "2.9"`, bind, servers, try order, empty
  forced hosts, `player-info-forwarding-mode = "legacy"`) plus `forwarding.secret`.
- Paper: `server.properties` with `online-mode=false`, explicit `server-port`/`server-ip`, and
  `spigot.yml` with `settings.bungeecord: true` for legacy forwarding.
- Configs are generated at network creation and written before first boot. Changing them later
  is a file write plus restart, not a hot reload.
- The proxy template is `builtin-velocity` (Java 25: current Velocity builds require class file
  69) and the backend default is `builtin-dev`. The proxy does **not** get the Paper bridge
  jar; the agent skips/fixes that on Velocity.

## Operations

`POST /api/v1/networks` creates the proxy, N backends, the container network, and the configs,
  then starts backends first and the proxy last. `GET /api/v1/networks[/{id}]` lists them.
  There is no network delete/restart endpoint yet; operate the member servers.
- **Restart network** — stop/start backends first, proxy last, so the window is one proxy
  restart.
- **Drain backend** — not implemented. Live removal requires a Velocity plugin; without it the
  proxy only re-reads the server list on restart.
- Aggregated player counts and TPS across the network are not implemented; proxy logs are
  tailed like any server, but structured proxy events are absent.

## Console and observability

- Proxy logs are tailed and parsed like any server.
- The Paper bridge cannot run on Velocity, so structured proxy events (player switch, backend
  register/unregister) come from the M14 Velocity plugin. Until then, proxy information is
  log-derived and the UI says so.
- TPS is measured per backend via the bridge; proxy-level TPS is not a Minecraft metric and is
  labelled accordingly.

## Deferred

- Velocity bridge plugin (drain/player-switch/backend events).
- Modern forwarding (Paper `paper-global.yml` velocity secret) — legacy only for now; the
  isolation of unpublished backend ports is what makes legacy acceptable in this release.
- Live drain, network restart/delete APIs, aggregated metrics, multi-proxy networks.
- A real-player join was not automated: routing is verified by proxy status ping, running
  backends, DNS aliases, and closed backend host ports. Manual client join remains the check.
- BungeeCord: excluded through v1.1; revisit only if adoption demands.
