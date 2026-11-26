# Node enrollment

**Status:** design · implemented at M1. Security rationale:
[security](../architecture/security.md).

Enrollment turns a pasted token into a durable node identity. The token is the only secret
that travels; the certificate is what the node uses from then on.

## Flow

```
panel                                  node
  │  create token (single use, short TTL) │
  │───────────────────────────────────────▶│  install agent, pass token
  │                                        │  generate keypair
  │◀───────────── CSR + token ─────────────│  POST /agent/enroll
  │  verify token, sign cert from org CA   │
  │─────────────────── cert ──────────────▶│
  │◀═══════════ mTLS WebSocket ════════════│  hello: version, capabilities
  │  assign node id, record cert_serial    │
```

## Rules

- Tokens are **single-use, short-TTL** (default 15 minutes), bound to an org, and listed with
  their state (issued / used / expired) so an unused token is visible.
- The node private key never leaves the node. The panel stores only `cert_serial`,
  `cert_expires_at` and `revoked_at`.
- One certificate authenticates the agent WebSocket (console, metrics, claims) and the blob
  download endpoint. There is no second credential.
- Rotation at 30 days: the agent generates a new key and CSR, the panel signs it while the old
  certificate is still valid, then the old serial is retired.
- Revocation writes `revoked_at` and is checked on every reconnect. A revoked node's servers
  keep running; it simply cannot report or receive work.

## Failure modes

| Symptom | Cause | Action |
|---|---|---|
| `enroll: token expired` | Token older than TTL | Issue a new token |
| `enroll: token already used` | Token replay or a retried install | Issue a new token; check the first node |
| Connects, then `403` on reconnect | Certificate revoked | Re-enroll deliberately; investigate why it was revoked |
| Certificate expired | Rotation failed (node offline > 30 d) | Re-enroll; the panel offers a renewal token |
| Node healthy, servers missing | Agent started before its data volume mounted | Check the unit's dependencies, restart |

## Compromise procedure

1. Revoke the node in the panel (immediate on next reconnect; no inbound port to block).
2. Snapshot and stop its servers from another healthy node only if the data is reachable —
   a compromised host is not a backup source.
3. Rebuild the host, re-enroll with a fresh token, restore from the last verified backup.
4. Rotate any secret the node could read: provider keys are panel-side, but per-server bridge
   tokens and database credentials must be treated as exposed.
