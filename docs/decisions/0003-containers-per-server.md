# 0003 — Container per server, rootless Podman

**Status:** accepted · 2026-11-26

## Context

Servers need CPU/memory limits, a clean teardown, and a JVM version that does not leak into
the panel's business logic. Bare-metal JVMs with cgroups and a supervisor are possible but
give up isolation and reproducible images. Docker is ubiquitous but its daemon is root.

## Decision

One container per server. **Rootless Podman is the default**; Docker is supported and
documented as root-equivalent. No socket proxy. Per-JDK images (`delphi/java:21-temurin`,
`:25`), support floor Paper 1.20.5+.

## Consequences

- cgroup v2 gives exact CPU/memory accounting at 1 s for free.
- Rootless UID mapping and cgroup delegation are install prerequisites and the alpha's top
  risk; they are tested at M2, not discovered later.
- Server data must be a bind mount, not a named volume, because snapshots, the CAS and backups
  operate on the host filesystem.
- **Forbidden:** a socket proxy presented as a security boundary; hot-swapping the runtime
  mid-server; per-server custom images as a supported path.
