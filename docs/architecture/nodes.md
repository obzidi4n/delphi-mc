# Nodes, agents, and groups

**Status:** current.

These are three different things:

| Concept | Meaning |
|---|---|
| **Node** | One enrolled machine or VM that can run server containers. |
| **Agent** | The daemon that owns containers, ports, logs, metrics and snapshots on one node. |
| **Group** | A logical targeting set of servers. It is not a machine boundary. |

## One agent per node

Run **one agent per host or VM**, not one per Minecraft server.

The agent is a node daemon. It owns every managed server on that host: each server is its own
container, with its own data directory, JVM, memory limit, CPU limit and published port. The
agent reconciles them all through one mTLS WebSocket to the panel.

```text
physical host / VM
└── one delphi agent
    ├── container: survival-1
    ├── container: creative-1
    ├── container: minigames-1
    └── container: velocity-proxy
```

Do not run one agent inside each Minecraft container. The agent manages containers; it is not
part of a server's JVM. It also needs host-level access to the runtime, server directories,
cgroup metrics and snapshots.

## When to add another node

Add another node and agent when you add:

- another physical machine,
- another VM or cloud instance,
- a separate failure domain,
- a separate security or ownership boundary.

Do not add a node merely because you are adding another Minecraft server on the same machine.
The existing agent already manages it.

| Setup | Agents |
|---|---|
| One host, three Paper/Purpur/Folia servers | 1 |
| One host, Velocity proxy plus three backends | 1 |
| Two physical hosts, several servers each | 2 |
| Three VMs on one hypervisor | 3 |
| Separate production and development hosts | 2 |
| One agent per Minecraft server on one host | Not recommended |

Treating every Minecraft server as its own node fragments the port pool, duplicates TLS and
monitoring connections, and makes placement see many fake hosts instead of the real machine.
It also makes host-level capacity and failure domains harder to reason about.

## Groups are not nodes

Groups organize servers for people and bulk actions. A group can contain servers on different
nodes. A node can host servers from several groups. Membership in a group does not place a
server on a particular node and does not imply a separate agent.

The home page renders servers by group, with an `Ungrouped` section. The Groups page manages
membership. Placement happens per node at server creation; v1.0 has no live migration, only
drain plus stop/snapshot/restore.

## Capacity and placement

Resource pressure is per node, not per group. The panel evaluates placement by node capacity;
the agent enforces each server's container limits. Ports come from a per-node pool owned by the
panel. A single host can run as many servers as its RAM, CPU, disk and ports allow, subject to
the per-server limits you set.

For the placement formula and drain behavior, see [`fleet.md`](fleet.md).

## Current network constraint

A Velocity network is currently created on **one node**: its proxy and backends share that
node's private container network, and only the proxy publishes a host port. A multi-node
network is not implemented yet. See [`networks.md`](networks.md).

## Failure behavior

The agent is autonomous. If the panel is unreachable, its servers keep running, crash restart
policy continues locally, and console/log buffers continue on the node. The panel reconnects
later and resumes observation and mutations. A node failure affects every server on that node;
a group spanning two nodes does not create a shared failure domain.

For enrollment and the first connect, see
[`../operations/node-enrollment.md`](../operations/node-enrollment.md). For agent internals,
see [`agent.md`](agent.md).
