# Testing

**Status:** harness starts at M0; end-to-end suite required by M5. This page exists because
the alpha's exit criteria are otherwise unfalsifiable.

## Layers

| Layer | Scope | Runs |
|---|---|---|
| Unit | Capability contract, reconciler, console batcher, approval loop, redaction, template rendering | Every PR, no external services |
| Contract | `openapi.yaml` ↔ handlers ↔ generated Go/TS clients; agent frames against the wire spec | Every PR |
| Integration | Panel with a fake agent; agent against a fake panel; Postgres via testcontainers | Every PR |
| End-to-end | Real Linux host, rootless Podman, real Paper jar, real WebSocket clients | Every merge to main |
| AI evals | Golden prompts against a recorded provider; safety suite | Every PR (recorded), nightly (live) |

## End-to-end host

The runner must provide exactly what the installer checks: cgroup v2, rootless Podman,
`/etc/subuid` ranges, `newuidmap`, and a delegated systemd user session. A runner without
them proves nothing about installation, which is alpha criterion 1.

- Paper jar is downloaded once, pinned by sha256, and cached; tests never depend on "latest".
- Ports come from an ephemeral range; no test binds a fixed port.
- Every wait is a poll with a deadline (readiness = bridge `Done (…)` with a port-open
  fallback), never a fixed `sleep`.

## Mapping to the alpha exit criteria

| Criterion | Test |
|---|---|
| Installs on a clean host with prerequisites | Clean-container provision script + installer run, asserted on `/readyz` |
| Enrolls with a token, holds an identity | Enrollment test; a revoked node is rejected on reconnect |
| Creates from the Survival template and boots | Template instantiation + `Done (…)` within a deadline |
| Console `<50 ms` with 20 viewers | 20 WS subscribers on one server, latency measured from agent emit to panel fan-out; browser paint is a manual check |
| Bridge auto-installs; chat/players/TPS appear | Bridge handshake + a scripted chat event, asserted through the API |
| Power works across a panel restart | Kill the panel mid-operation, restart, assert convergence and no duplicate jobs |

## Performance checks

- Console batcher: micro-benchmark for frames/second and allocation per line (16 ms batch).
- Fan-out: one agent subscription, 20 subscribers, assert constant per-server upstream cost.
- Fleet list: one aggregate query, asserted with `pg_stat_statements` in the integration run.

## Rules

- Tests may not depend on the network except the pinned jar download and recorded AI fixtures.
- No retry-until-green. A flaky test is quarantined the same day and fixed within the week.
- Do not test wiring, echoed defaults, or framework behavior; do test what a user can observe.
- The AI safety suite is a release gate: prompt injection, privilege escalation, mutation
  without approval, and a redaction leak all fail the build.
