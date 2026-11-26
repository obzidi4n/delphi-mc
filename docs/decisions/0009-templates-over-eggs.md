# 0009 — Templates, not eggs

**Status:** accepted · 2026-11-26

## Context

The incumbent panel ecosystem models server creation with "eggs": shell scripts plus variable
substitution, which are powerful, untrusted, and unversioned. A forty-field creation form is
the alternative, and it is worse — it makes a common task error-prone and untestable.

Server creation is really "instantiate this known-good configuration".

## Decision

Templates: a declarative document (base, MC version, Java, defaults, files, plugins,
variables) with built-in presets, user editing, and file-based import/export. Variables are a
flat string map. Re-applying a newer template is an explicit, diffed job. An imported template
pins every artifact by hash and shows its diff before writing.

## Consequences

- Creation is repeatable and reviewable; "like the other two" is implementable for the AI.
- No arbitrary code execution path in templates — a template that downloads jars is treated as
  code and pinned by hash.
- No hosted registry in v1.0; sharing is file-based until a git-backed registry lands in v1.x.
- **Forbidden:** automatic template re-application (drift), and silently resolving a version
  range without recording the resolved hash.
