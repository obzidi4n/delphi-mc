# DelphiMC docs

The map. Every page here is versioned with the code and changes in the same PR as the
behavior it describes.

**Status:** M15 landed — automatic placement, node drain, and signed one-command agent
self-update with rollback slots, on top of M14 networks, M13 TUI, v1.0/M12, M11 automation,
M10 backups, M9 plugins/artifacts, M8 files, M7 approvals, M6 assistant, M5 bridge, M4
console, M3b templates, the M3a spec-first API, identity/RBAC, paginated lists, Team/Groups
pages, and the CLI. See [`plan/roadmap.md`](plan/roadmap.md); fleet limits are in
[`architecture/fleet.md`](architecture/fleet.md).

## Architecture — what and how

| Doc | Covers | Status |
|---|---|---|
| [`overview.md`](architecture/overview.md) | Planes, invariants, delivery shape, budgets | current |
| [`nodes.md`](architecture/nodes.md) | Nodes vs agents vs groups, multi-server hosts, capacity | current |
| [`capability-layer.md`](architecture/capability-layer.md) | The single mutation surface | current |
| [`agent.md`](architecture/agent.md) | Containers, console, metrics, snapshots, bridge | current |
| [`updates.md`](architecture/updates.md) | Artifacts, CAS, providers, update policies, boot-check | current |
| [`ai.md`](architecture/ai.md) | Providers, context, approvals, guardrails, evals | current |
| [`security.md`](architecture/security.md) | Trust boundaries, identity, secrets, isolation | current |
| [`clients.md`](architecture/clients.md) | Web UI, CLI, TUI (v1.1) | current |
| [`templates.md`](architecture/templates.md) | Template instantiation and drift | current |
| [`automation.md`](architecture/automation.md) | Schedules, restart policy, alerts | current |
| [`networks.md`](architecture/networks.md) | Velocity, forwarding, drain | v1.1 |
| [`fleet.md`](architecture/fleet.md) | Placement, drain, rollout, skew | v1.1 |

## Decisions

[`plan/decision-log.md`](plan/decision-log.md) is the single index — D1–D22, one line each,
with links. ADRs are append-only: [`decisions/README.md`](decisions/README.md) holds the
numbering rule and format, not a second table.

## Planning

| Doc | Covers |
|---|---|
| [`plan/roadmap.md`](plan/roadmap.md) | Milestones, alpha exit criteria, risks, changelog |
| [`plan/decision-log.md`](plan/decision-log.md) | D1–D22 and open items |

The roadmap and the decision log are the only pages expected to churn.

## Reference — contracts, not prose

| Doc | Covers |
|---|---|
| [`reference/data-model.md`](reference/data-model.md) | Tables and ownership |
| [`reference/ai-tools.md`](reference/ai-tools.md) | Tool catalog, scopes, unlock milestones |
| [`reference/tui-keybindings.md`](reference/tui-keybindings.md) | TUI keys, scopes, and implemented surface |
| [`reference/template-schema.md`](reference/template-schema.md) | Template format |
| [`reference/wire-protocol.md`](reference/wire-protocol.md) | Agent/browser frames and endpoints |
| [`reference/api-conventions.md`](reference/api-conventions.md) | Pagination, errors, idempotency, preconditions |
| [`reference/configuration.md`](reference/configuration.md) | Panel and agent config keys and defaults |

## Operations

| Doc | Covers |
|---|---|
| [`operations/install.md`](operations/install.md) | Host prerequisites and install flow (target: M5) |
| [`operations/node-enrollment.md`](operations/node-enrollment.md) | Token → certificate → first connect |
| [`operations/upgrades.md`](operations/upgrades.md) | Panel and agent upgrades |
| [`operations/incident-runbook.md`](operations/incident-runbook.md) | Failure modes and what still works |

## Contributing

| Doc | Covers |
|---|---|
| [`contributing/dev-setup.md`](contributing/dev-setup.md) | Toolchain, build, run, test |
| [`contributing/conventions.md`](contributing/conventions.md) | Repo layout, code and doc rules |
| [`contributing/testing.md`](contributing/testing.md) | Test layers, e2e host, AI evals |
| [`contributing/ai-evals.md`](contributing/ai-evals.md) | Manual live AI prompt suite and pass criteria |

## Deliberately not written yet

- `contributing/testing.md` exists; its e2e harness is written with M2.

## Rules

- **Frozen after docs v1.0:** `architecture/` and `reference/` change only when code contradicts
  them. The roadmap and decision log are the living pages; everything else grows with its
  milestone. Do not add a page for a feature that has no code.
- Machine-readable contracts win: at M1 `internal/wire` defines frames; at M3b the template
  schema becomes a file. These docs then link to the source instead of restating it.
- One concept per page; split rather than grow. Keep pages under ~300 lines.
- Descriptions of unshipped behavior carry a status line (`target: Mx`, `v1.1`) — never
  present a plan as an existing feature.
- Every mutating capability and AI tool gets a row in
  [`reference/ai-tools.md`](reference/ai-tools.md); that table is the authorization contract.
- ADRs are append-only. Supersede with a new number and a link, never by editing.
