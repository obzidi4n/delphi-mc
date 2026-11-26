# Networks (Velocity)

**Status:** deferred to **v1.1** (M14). The `networks` entity and a nullable `network_id`
exist from M0 so this is additive, not a migration. ADR
[0007](../decisions/0007-velocity-networks-in-v1-1.md).

A proxy is not just another server, so a network is a first-class entity:

```
Network { id, name, proxy_server_id, mode: modern|legacy }
Server  { ..., network_id, role: proxy|backend, order }
```

## Isolation by construction

- **One container network per network.** The proxy publishes the public port; backends
  publish nothing and are reachable only from the proxy.
- This is isolation by construction, not by firewall rules an operator can misconfigure.

## Generated configuration, never hand-edited

Both sides are derived from one source: server list, try order, forwarding secret, forwarding
mode.

- Velocity: `velocity.toml` servers, try order, `player-info-forwarding-mode`, secret.
- Paper: `settings.velocity.enabled`, matching secret, `online-mode=false` for modern
  forwarding; `settings.bungeecord` + legacy forwarding for `legacy` mode.
- The secret is generated once per network and never exposed as an editable field — drift
  between the two sides is the most common way a network breaks.

## Operations

- **Restart network** — backends first, proxy last, so the window is one proxy restart.
- **Drain backend** — remove from the try list, wait for players to leave, then act.
  Live removal requires the **Velocity plugin** shipped with M14; without it, the proxy only
  re-reads the server list on restart. Do not promise live drain before that plugin exists.
- Aggregated player counts and TPS across the network.

## Console and observability

- Proxy logs are tailed and parsed like any server.
- The Paper bridge cannot run on Velocity, so structured proxy events (player switch, backend
  register/unregister) come from the M14 Velocity plugin. Until then, proxy information is
  log-derived and the UI says so.
- TPS is measured per backend via the bridge; proxy-level TPS is not a Minecraft metric and is
  labelled accordingly.

## Deferred

- Velocity bridge plugin: shipped minimally with M14 for drain and events.
- BungeeCord: excluded through v1.1; revisit only if adoption demands.
- Multi-proxy networks: not planned.
