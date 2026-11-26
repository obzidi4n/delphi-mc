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
| `subscribe` | browser → panel | topics |
| `error` | either | code, message, `reply_to` |

**Implemented at M1:** `hello`, `heartbeat` and `hello.ack`. Console, metrics, claim and job
frames remain the contract for M2/M4; none of them are reserved differently.

## Console framing

Console is the only high-frequency path, so it is binary and batched. A console batch never
uses the JSON envelope; the server is implied by the subscription.

```
u8  kind           // 1 = console batch
u64 seq            // monotonic per server — the only sequence source for console
u32 payload_len
u8[] payload       // newline-delimited raw log lines
```

Batches are emitted every ~16 ms. `seq` is the single sequence number for console traffic;
the envelope's `seq` belongs to text topics and is never mixed with it. On reconnect the
subscriber passes `since_seq`; the agent replays from its ring buffer, and the panel marks an
explicit gap if the buffer no longer holds it. Dropped frames never block the log reader.

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
