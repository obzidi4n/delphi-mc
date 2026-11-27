# Fleet operations

**Status:** M15 implements placement, drain, signed self-update, and protocol skew; see below.

 deferred to **v1.1** (M15). v1.0 supports multiple nodes but does not schedule
across them: the node is chosen at server creation and the UI shows per-node capacity.

## Placement

Bin-pack on **RAM** (Minecraft is memory-bound), spread CPU, hard-filter disk, honor
labels and anti-affinity.

`capacity = total_ram − reserve − Σ(heap + ~400MB)` — the ~400 MB accounts for JVM overhead
and off-heap, and is the number that stops a "fits on paper" placement from OOM-killing a
neighbor.

## Drain

Stop placing new servers; the admin drives stop → snapshot → restore elsewhere. There is no
zero-downtime migration; claiming one would be a lie about Minecraft.

A drain plan is declarative and visible before it runs, like any destructive operation.

## Health

Heartbeat plus capability report. A node is marked unhealthy after N misses; **its servers
keep running** because the agent is autonomous. The UI distinguishes "panel cannot see node"
from "node is down" — they are different incidents.

## Agent rollout and skew

- Capability handshake on connect. Contract: panel *N* supports agents *N-1…N* and agent *N*
  supports panels *N-1…N*.
- Upgrade order: panel first, then agents, draining each node.
- Self-update: two-slot swap, health-gated, automatic rollback. Artifacts are signed
  (ed25519); the agent verifies against a key pinned in the binary. Slots handle a bad build;
  the signature handles a hijacked channel.

## Quotas and scale triggers

v1.0 has no quota system. The design targets a reference fleet before any of this is
optimized; the number is open item 2 in
[`../plan/decision-log.md#still-open`](../plan/decision-log.md#still-open), and the transport
trigger shares it.

## What the panel must never do

- Never assume an unhealthy node's servers are down.
- Never let two sides believe they own lifecycle: desired state and revision, always.
- Never require the panel to be up for a crashed server to restart.


## Implemented at M15

- **Placement:** an empty/`auto` `node_id` during server creation selects the least-loaded
  healthy, non-revoked, non-draining node with a free port. Networks still require an explicit
  node.
- **Drain:** `POST /api/v1/nodes/{id}/drain` toggles a boolean; drained nodes are excluded from
  placement but their servers keep running. There is no migration/evacuation.
- **Self-update:** admin-only `POST /api/v1/fleet/agent-update` stores a signed binary and
  pushes `agent.update` to every healthy node; `delphi fleet update` signs/uploads in one
  command. Agents verify ed25519 against `update_public_key`, pre-flight `version`, keep
  `<exe>.rollback`, and re-exec in place. Manual rollback only.
- **Skew:** agent hello carries protocol 1; the panel rejects newer protocols (old agents that
  omit it are accepted). Capabilities still gate tools; version strings are advisory.
