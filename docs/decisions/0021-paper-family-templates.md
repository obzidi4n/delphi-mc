# 0021 — Purpur and Folia as Paper-family templates

**Status:** accepted · 2026-11-27

## Context

Paper was the only resolvable server base. Purpur is a Paper-compatible fork with its own
download API. Folia is a Paper-compatible regionized server whose plugin API is not the Paper
plugin API. Operators want all three without turning templates into arbitrary jar uploads.

## Decision

- Template bases are `paper`, `purpur`, `folia`, and `velocity`.
- Purpur resolves through `api.purpurmc.org`. Because that API exposes only an md5, the panel
  downloads the selected jar once to compute the sha256 stored on the server.
- Folia resolves through the PaperMC v3 API like Paper.
- The Paper bridge is installed for Paper and Purpur only. Velocity and Folia run without it;
  Folia therefore has console, files, power, and container metrics, but no bridge-provided
  player/TPS/chat telemetry.
- The Paper 1.20.5+/Java 21 support floor from ADR 0016 applies to the Paper-family bases.
- Fabric, Forge, and arbitrary server jars remain out of scope.

## Consequences

Purpur and Folia appear as first-class built-in templates, and network backends may use either.
Purpur server creation pays one jar download on first resolution; the artifact is cached.
Folia is honestly labeled as lacking bridge telemetry rather than silently installing an
unsupported plugin.
