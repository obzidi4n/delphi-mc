# Data model

**Status:** current at M3a · schema created at M0–M3a. Postgres + `pgx` + `sqlc`; embedded
forward-only SQL migrations applied by the panel at startup ([ADR 0019](../decisions/0019-forward-only-migrations.md)).

```
users, sessions, api_keys, orgs, role_bindings (global, per-group, per-server)
nodes            (id, org_id, labels, capacity, agent_version, status, last_seen,
                  fs_capability: reflink|zfs|copy,
                  cert_serial, cert_expires_at, revoked_at)
servers          (id, org_id, node_id, name, image, java_version, memory_mb, cpu_cores,
                  port, desired_state, desired_revision, observed_state, applied_revision,
                  kill_requested, accept_eula, jar_url, jar_sha256, last_error,
                  base, template_id, template_version, variables jsonb, files jsonb,
                  network_id, network_role, tags[],
                  deleted_at, purge_after)
server_groups    (id, org_id, name, description)          # targeting (many-to-many)
role_bindings    (user_id, org_id, scope_kind: org|group|server, scope_id,
                  role: owner|admin|developer|viewer)     # scopes derive from the role
server_group_members (group_id, server_id)
server_summary   (server_id, state, updated_at)            # per-server state rollup
templates        (id, org_id nullable=built-in, name, revision, body jsonb, builtin)
networks         (id, org_id, name, proxy_server_id, mode)
players          (server_id, uuid, name, first_seen, last_seen)
schedules        (id, org_id, cron, tz, target_kind, target_id, actions[], enabled, next_run)
alert_rules      (id, org_id, scope, kind, metric, comparator, threshold, duration, severity)
alert_channels   (id, org_id, kind: discord|email|webhook, config, secret_ref)
alerts           (rule_id, server_id, state: firing|resolved, started_at)
maintenance_windows (org_id, scope, starts_at, ends_at, suppress_alerts, block_updates)
artifacts        (sha256, size, kind, source, refcount, last_referenced)
conversations    (id, org_id, user_id, title, created_at, updated_at)
messages         (id, conversation_id, role, content, tool_calls jsonb, tool_call_id, created_at)
docs_chunks      (path, ordinal, heading, content, tsv)       # GIN FTS, replaced on startup when the corpus changed
docs_meta        (id=1, hash)
ai_approvals     (id, org_id, user_id, conversation_id, status, plan jsonb, results jsonb,
                  created_at, decided_at, executed_at)
plugins          (server_id, project, provider, installed_version, installed_hash,
                  previous_version, previous_hash, pinned_version, policy)
file_revisions   (server_id, path, revision, sha256, author, via, created_at)
snapshots, backups          # storage location, size, encryption key ref, retention
jobs             (kind, target, state, priority, idempotency_key, attempts,
                  lease_until, payload, error)
databases        (server_id, engine, name, secret_ref)
metrics_rollups  (server_id, ts, cpu, mem, tps, players)
audit_log        (actor, via, model, action, target, diff, ts)
known_issues     (signature_hash, cause, fix, occurrences)
conversations, messages, tool_calls, approvals, ai_usage, ai_feedback
docs_chunks      (doc_id, heading, body, tsv, source_url, corpus_version)   # FTS
org_settings     (org_id, ai_egress, token_cap, quiet_hours, ...)
```

## Ownership rule

`org_id` belongs on **owned roots** — nodes, servers, groups, templates, networks, schedules,
alert rules and channels, maintenance windows, conversations. Children (`plugins`,
`file_revisions`, `metrics_rollups`, `alerts`, `jobs` payloads) derive it through their parent
FK. The rule is enforced with composite FKs where it matters, not by discipline.

## Notes that constrain the schema

- **`jobs`** is the queue: lease-based claims, idempotency key, attempt counter. No external
  broker (ADR [0002](../decisions/0002-one-transport-websocket.md)).
- **`server_summary`** is the one aggregate the fleet list and AI tier 1 read. It is
  materialized and event-driven at M3a — not computed per request. The **panel** writes it
  from `state.observed` and bridge events; a periodic reconciler repairs drift after downtime.
  Nothing refreshes it on read.
- **`servers.deleted_at` / `purge_after`** back the seven-day retention: deletion is a
  tombstone plus a purge job, never a synchronous `rm`.
- **`plugins`** stores installed version *and* hash; update detection is impossible without
  both, and rollback is impossible without the previous hash.
- **`artifacts`** carries a refcount/last-referenced pair so the CAS can be garbage-collected;
  an unreferenced blob is deletable, a referenced one never is.
- **`file_revisions`** backs diff, rollback, and the audit of who wrote what.
- **`alert_rules.kind`** exists because "server down", "crash loop" and "update available" are
  not metric comparisons; a generic comparator column alone cannot express them.
- **`docs_chunks`** is FTS (`tsvector` + GIN), not embeddings. No pgvector in v1.0. Each row
  carries `source_url` and `corpus_version`, so "Sources:" links upstream and a corpus refresh
  is auditable.
- **`known_issues`** is per-install; cross-install sharing is opt-in telemetry, not a schema
  promise.

## Not modelled

Quota/entitlement accounting, billing, cross-org sharing, player identities across servers
(players are per server in v1.0), and panel telemetry. Each would be additive.

## Retention

- Console ring buffer: memory only, ~5k lines per server.
- Crash artifacts: on disk under the server directory (`crash/<timestamp>/`), last 10 or 30
  days, excluded from backups unless the operator asks.
- Logs: on disk, never ingested.
- Metrics: 1 h @ 1 s / 24 h @ 15 s / 30 d @ 5 m rollups.
- Audit: append-only, pruned by a retention policy decided with backups (not yet fixed).
- Deleted servers: directory retained 7 days unless purged.
