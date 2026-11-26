# Decision log

One line per decision; reasoning lives in the [ADRs](../decisions/README.md). Superseding a
decision updates this table and adds an ADR — it never edits the old one.

| # | Decision | Choice | ADR |
|---|---|---|---|
| D1 | Backend language | Go everywhere — agent + control plane, single binary | [0001](../decisions/0001-go-monorepo.md) |
| D2 | Transport | One mTLS WebSocket per agent; Postgres `SKIP LOCKED` is the queue | [0002](../decisions/0002-one-transport-websocket.md) |
| D3 | Isolation | Container per server, rootless Podman default; no socket proxy | [0003](../decisions/0003-containers-per-server.md) |
| D4 | Server flavors | Paper-family; Velocity from v1.1; Folia/Fabric/Forge out | [0007](../decisions/0007-velocity-networks-in-v1-1.md), [0016](../decisions/0016-paper-1-20-java-21-floor.md) |
| D5 | Capability layer | Single typed mutation surface for HTTP, CLI and AI | [0004](../decisions/0004-capability-layer-as-single-mutation-surface.md) |
| D6 | AI safety | Two-phase approvals, untrusted-content fencing, provenance | [0005](../decisions/0005-ai-two-phase-approvals.md) |
| D7 | Docs | In-repo `docs/`, doubling as the RAG corpus | [0006](../decisions/0006-docs-in-repo.md) |
| D8 | Filesystem | Capability-detected (`reflink`/ZFS/copy); installer offers btrfs; features gated | [0010](../decisions/0010-filesystem-capability-detection.md) |
| D9 | Ingress | Panel-proxied, gateway-ready; agents stay outbound-only | [0002](../decisions/0002-one-transport-websocket.md) |
| D10 | Tenancy | `org_id` on owned roots, single-org UX | [0011](../decisions/0011-org-ready-single-org-ux.md) |
| D11 | AI default | No bundled model runtime; OpenAI-compatible endpoint; probe gates tools | [0012](../decisions/0012-ai-no-bundled-runtime.md) |
| D12 | Proactive AI | Event-driven on (triage, alert explanations); digests opt-in; daily cap; never acts | [0018](../decisions/0018-bounded-proactive-ai.md) |
| D13 | Update boot-check | v1.0, default on for `auto-*`, gated on reflink | [0010](../decisions/0010-filesystem-capability-detection.md) |
| D14 | Approval threshold | Card always; name-typing above one server or for destructive verbs | [0013](../decisions/0013-approval-threshold.md) |
| D15 | TUI auth | Device code + `--token` fallback; keychain or `0600` file | [0008](../decisions/0008-native-tui-as-api-client.md) |
| D16 | Template sharing | Import/export now; git registry in v1.x; no hosted registry | [0009](../decisions/0009-templates-over-eggs.md) |
| D17 | Grouping | `server_groups` for targeting; tags for display | [0014](../decisions/0014-groups-for-targeting.md) |
| D18 | Delivery shape | Alpha = M0–M5 (vertical slice); no dates | [0015](../decisions/0015-v1-scope-small-operator.md) |
| D19 | v1.0 scope | Technical owner-operator, 1–10 servers; TUI/networks/fleet ops at v1.1 | [0015](../decisions/0015-v1-scope-small-operator.md) |
| D20 | Support floor | Paper 1.20.5+ / Java 21+; two JDK images | [0016](../decisions/0016-paper-1-20-java-21-floor.md) |
| D21 | Migrations | Forward-only; rollback = snapshot + previous binary | [0019](../decisions/0019-forward-only-migrations.md) |
| D22 | License | Apache-2.0 (`LICENSE`); no `NOTICE` yet | [0020](../decisions/0020-apache-2-0.md) |

Restart policy ownership (agent-local) is recorded as ADR
[0017](../decisions/0017-agent-owned-restart-policy.md).

## Still open

Deliberately deferred; none block M0. Each has an owner decision, not a default.

1. **Hosted SaaS vs self-hosted only.** The plan assumes self-hosted. SaaS adds billing,
   quotas and a different threat model.
2. **Reference fleet.** The performance rules (one aggregate query, 20 concurrent console
   viewers, 16 ms batching) are unquantified until a target is written down — e.g. 10 nodes /
   200 servers / 50 operators. Same number feeds the transport trigger below.
3. **Broker / gateway trigger.** At what console fan-out, node count or panel-replica count do
   we reintroduce a message bus and split the gateway? Default: one transport until then.
4. **Master key storage and rotation.** Encryption at rest needs a key that is not stored in
   Postgres. Options: host env var, mounted file, external KMS — decided at M0, before the
   first secret exists.
5. **Audit retention.** Append-only audit has no pruning policy yet; decided alongside backup
   retention (M10).
6. **Disk accounting source.** cgroup v2 provides I/O, not usage. Per-server disk series need a
   source (`statfs`, btrfs qgroup) and a polling budget; per-server quotas are a related,
   separate decision.
7. **Alert channel defaults.** Discord/webhook ship first (M11); email needs SMTP config.
8. **Map render / player map.** A common ask; a whole tile-serving subsystem. Out until asked
   for by a paying user.
9. **BungeeCord.** Excluded through v1.1 (Velocity only); revisit only if adoption demands.
