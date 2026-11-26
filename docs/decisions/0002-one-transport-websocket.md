# 0002 — One transport: mTLS WebSocket per agent

**Status:** accepted · 2026-11-26

## Context

Agents dial out and need to receive work and stream console/metrics. The obvious designs are
(a) a message broker (NATS) plus HTTP, or (b) HTTP only with long-polling, or (c) one
persistent connection.

An earlier draft specified NATS core with an embedded server for single-node installs. At v1.0
scale — one to ten nodes, one panel replica — the broker adds a second transport, a second
credential system, an extra public port, and a "real cluster" story that is never used. The
plan's own rule is that a new component must remove more than it adds; the broker did not.

## Decision

One **mTLS WebSocket per agent**, carrying console batches, metrics, heartbeats, status
deltas, job claims and commands, using the same enveloped frame format as the browser
([wire protocol](../reference/wire-protocol.md)). Plain HTTP carries the JSON API and blob
downloads. Postgres `SKIP LOCKED` is the durable queue.

## Consequences

- One credential (the node certificate), one port, one connection, one framing layer.
- Console fan-out to browsers and to the panel shares a code path.
- **Forbidden:** a second message bus, an embedded broker mode, or HTTP long-polling for job
  claims. Reopening the question requires the trigger in
  [`../plan/decision-log.md`](../plan/decision-log.md) (second panel replica or >50 nodes) and
  a new ADR.
- We own reconnect, lease renewal and flow control instead of inheriting them. This is the
  cost, and it is bounded — the frames are small and the reconnection rules are few.
