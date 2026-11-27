# Wire protocol

**Status: provisional** until M1 lands. This is the contract the panel, agent, CLI and web UI
are built against; breaking changes require a version bump and an ADR. At M1 `internal/wire`
becomes the source of truth and this page links to it rather than restating it.

## Transports

| Transport | Carries |
|---|---|
| HTTPS + JSON | The public API (spec-first OpenAPI), blob download |
| WebSocket | Agent channel; browser subscriptions |
| Unix socket | Agent ↔ bridge plugin (inside the container namespace) |

There is no message broker. Postgres is the durable queue; the WebSocket carries live
traffic and claims.

## WebSocket envelope

Text frames carry a JSON envelope; console payloads are binary frames with a fixed header.

```json
{
  "v": 1,
  "type": "console.batch",
  "id": "01J…",          // optional correlation id
  "topic": "server:123:console",
  "seq": 1042,
  "reply_to": "01J…",    // optional, for request/reply frames
  "payload": { }
}
```

| Type | Direction | Payload |
|---|---|---|
| `hello` | agent → panel | identity, agent version, capability flags, max frame size |
| `hello.ack` | panel → agent | assigned node id, protocol version, server time |
| `heartbeat` | agent → panel | uptime, load, capacity snapshot |
| `state.observed` | agent → panel | server id, state, `applied_revision` |
| `claim` | agent → panel | free slot count |
| `job.assigned` | panel → agent | job id, kind, payload, lease expiry |
| `job.result` | agent → panel | job id, outcome, error |
| `console.subscribe` | panel → agent | server id, `since_seq` |
| `console.batch` | agent → panel | server id, `seq`, raw bytes |
| `metrics.batch` | agent → panel | server id, samples |
| `command` | panel → agent | server id, command, idempotency key |
| `bridge.state` | agent → panel | server id, tps, players, root commands, updated |
| `bridge.event` | agent → panel | server id, one chat/join/quit/moderation event |
| `bridge.command` | panel → agent | server id, command id, kick/ban, player, reason |
| `subscribe` | browser → panel | topics |
| `error` | either | code, message, `reply_to` |

**Implemented:** `hello`, `heartbeat`, `hello.ack` (M1); `servers`, `server.state` (M2);
binary console batches and `metrics.batch` (M4); `bridge.state`, `bridge.event` (agent →
panel) and `bridge.command` (panel → agent) for M5, where the plugin itself speaks a file
protocol described in [agent](../architecture/agent.md#bridge-plugin-v0). Claim and job frames
remain the contract for a later milestone.

## Console framing

Console is the only high-frequency path, so it is binary and batched; it never uses the JSON
envelope. One agent channel carries a node's whole fleet, so the frame names the server.

```
u8  kind             // 1 = console batch
u16 server_id_len
u8[] server_id
u64 seq              // first line; each following line is seq+1
u32 payload_len
u8[] payload         // newline-delimited raw log lines, no trailing newline
```

Batches are emitted every ~16 ms. `seq` is the single sequence number for console traffic;
the envelope's `seq` belongs to text topics and is never mixed with it. The panel keeps a 5k
ring and marks overload gaps; a browser receiving a sequence greater than `last+1` inserts an
explicit `[delphi] N line(s) missing` line. Dropped frames never block the log reader.

Metrics ride the text envelope: `{"type":"metrics.batch","server_id","cpu_percent",
"memory_bytes","at"}` once per second per running server. The browser dashboard receives the
same facts as `{"type":"stats",...}` text frames over `<base>ws/servers/<id>`.

## Panel endpoints

Without an identity (rate-limited, single use):

| Endpoint | Purpose |
|---|---|
| `POST /agent/enroll` | Token + CSR → signed node certificate; the only pre-identity call |

Authenticated by the node certificate:

| Endpoint | Purpose |
|---|---|
| `GET /agent/blobs/{sha256}` | Blob download (proxy mode) |

User-authenticated (session or bearer):

| Endpoint | Purpose |
|---|---|
| `POST /artifacts` | Upload a jar or template bundle (multipart, sha256-verified, size-capped); returns the artifact |
| `GET /artifacts/{sha256}` | Download an artifact; distinct from the agent-only blob path |

Operator-facing:

| Endpoint | Purpose |
|---|---|
| `GET /healthz`, `GET /readyz` | Liveness and readiness |
| `GET /metrics` | Prometheus: queue depth, agent lag, fan-out, WS counts (M12) |

Job claims are **not** an HTTP long-poll: they ride the WebSocket `claim` frame so a node has
one connection and one credential.

## Versioning and compatibility

- `v` is the envelope version; the panel and agent negotiate the highest common value at
  `hello`.
- Capability flags, not versions, gate features: the agent advertises what it can do, the
  panel never infers from a build number. **Unknown flags are ignored** by both sides, so a
  newer peer never breaks an older one.
- Compatibility window: panel *N* ↔ agent *N-1…N*, both directions.
- The bridge plugin negotiates its own protocol version and capability flags with the agent;
  plugin *N-1* is tolerated.

## Browser topics

`server:<id>:console`, `server:<id>:stats`, `server:<id>:events`,
`fleet:alerts`, `conversation:<id>`. Topics are subscribed once per tab; the panel fans out
per topic and enforces per-topic rate limits.
