# Architecture overview

**Status:** current design · implementation target M0–M12.

## What this is

A control plane for Minecraft (Java Edition) servers. It owns server lifecycle, jars,
plugins, configs, backups and automation, and it exposes three surfaces over one typed API:
a web UI, a scriptable CLI (a native TUI at v1.1), and an AI operator that acts as the
requesting user.

## Who v1.0 is for

The technical owner-operator: one to ten self-hosted servers, single org, who wants AI, safe
plugin updates, backups and automation without learning a control panel. Scope decisions are
judged against that user (ADR
[0015](../decisions/0015-v1-scope-small-operator.md)). Larger fleets use the same
architecture in v1.1, not the same release.

## Two planes

```
web UI / CLI ──HTTP+WS──┐
                        ▼
                     panel (stateless; single replica in v1)
                        ├── Postgres   (source of truth, jobs, FTS)
                        ├── local disk (backups; S3 at v1.1)
                        └── AI provider (cloud or local endpoint)
                        ▲
                        │ one mTLS WebSocket per node, dialed outbound:
                        │ console, metrics, heartbeats, job claims, commands
          ┌─────────────┴─────────────┐
       agent (node A)             agent (node B)
          │ podman (rootless)         │
          ▼                           ▼
   server containers           server containers
          │
          │ unix socket (localhost, per server)
          ▼
   bridge plugin (in-JVM, Paper)
```

## Invariants

- **Agents dial out.** No inbound ports per node. Enrollment is a pasted token that becomes
  a signed node identity ([node enrollment](../operations/node-enrollment.md)).
- **Agent owns actual state; panel owns desired state.** The agent reconciles; neither side
  believes it controls lifecycle. See [capability layer](capability-layer.md).
- **One transport.** Agent and browser speak one enveloped frame format over WebSocket;
  plain HTTP carries blobs and the JSON API. No message broker, no extra port, and no second
  **agent** credential; the broker trigger lives in
  [`../plan/decision-log.md`](../plan/decision-log.md) (ADR
  [0002](../decisions/0002-one-transport-websocket.md)).
- **Durable work lives in Postgres.** `SELECT … FOR UPDATE SKIP LOCKED` is the queue; the
  live channel is telemetry only.
- **The panel is stateless in v1** (single replica, no HA claim); live subscriptions are
  in-memory and rebuilt on reconnect.
- **The AI never touches infrastructure.** It calls typed capabilities under the user's
  RBAC, and every mutation is an approval card ([ai.md](ai.md)).
- **Log files never leave the node.** Search runs on the agent; only matched, redacted
  snippets enter a conversation.

## Delivery shape

| Phase | Milestones | Outcome |
|---|---|---|
| Alpha | M0–M5 | Install → enroll → create from template → boot → console → bridge; nothing else |
| v1.0 | M6–M12 | AI, files, jar/plugin updates, backups, automation, hardening |
| v1.1 | M13–M19 | TUI, Velocity networks, fleet ops, S3, dev envs, databases, proactive AI |

The alpha is a vertical slice through every layer, deliberately thin. See
[`../plan/roadmap.md`](../plan/roadmap.md) for exit criteria and ordering rationale.

## Simplicity and speed budget

Rules the implementation is held to; violating one needs a reason in the PR.

**Simplicity**

- One binary, one Postgres, no extra services in the common case.
- One transport, one broker-free path. Adding a message bus requires a written trigger and
  must remove more than it adds.
- No component ahead of its scale: fleet ops, a second replica, and the TUI arrive with
  their release, not their ambition.
- Every new component must remove more than it adds.

**Speed**

- Console ring buffer stores raw bytes; parse on render or search, never per line.
- Agents reconcile in parallel and send a heartbeat before reconciliation finishes.
- Fleet lists use one aggregate query; every list endpoint is paginated.
- Static assets are content-hashed and served `immutable`; the console route is lazy-loaded.
- Metric points are downsampled server-side for wide ranges.
- Build with `-trimpath -ldflags="-s -w"`; panel ready to serve < 1 s, static assets < 100 ms.

## Where things live

- Mutations: [capability layer](capability-layer.md).
- Node internals: [agent](agent.md).
- Updates: [updates](updates.md). AI: [ai](ai.md). Security: [security](security.md).
- Repo layout and code rules: [`../contributing/conventions.md`](../contributing/conventions.md).
