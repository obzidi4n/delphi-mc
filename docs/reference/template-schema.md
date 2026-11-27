# Template schema

**Status:** current at M3b. Rationale: [templates](../architecture/templates.md). The Go
types in `internal/template` are the source of truth; built-ins live in
`internal/template/builtin/*.json`.

Import/export is JSON or YAML. A template is data plus a small file set; nothing in it executes
at import time.

```json
{
  "schema_version": 1,
  "name": "Survival",
  "description": "Paper survival server",
  "base": "paper",
  "mc_version": "1.21.11",
  "java": 21,
  "defaults": { "memory_mb": 4096, "cpu_cores": 2.0, "port": 0 },
  "files": {
    "server.properties": "server-port={{server.port}}\nmotd={{server.name}}\n"
  },
  "plugins": [
    { "project": "essentialsx", "provider": "modrinth", "version": "2.20.1", "required": true }
  ],
  "variables": {
    "difficulty": { "description": "World difficulty", "default": "normal" }
  }
}
```

Built-in variables: `{{server.name}}`, `{{server.port}}`, `{{server.memory_mb}}`,
`{{server.cpu_cores}}`, `{{server.java}}`, `{{template.mc_version}}`. Custom variables are
declared in `variables` and substituted by name.

## Field rules

| Field | Rule |
|---|---|
| `schema_version` | must be `1` |
| `name` | 1–64 characters; unique per org on import |
| `base` | `paper`, `purpur`, `folia`, or `velocity` |
| `mc_version` | literal `X.Y[.Z]`, or `latest` / `recommended` (resolved at instantiation) |
| `java` | `8`, `17`, `21`, or `25` |
| `defaults.memory_mb` | 512–32768 when set |
| `defaults.cpu_cores` | 0–16 |
| `defaults.port` | `0` (allocate) or 1024–65535 |
| `files` | at most 64 files, 64 KiB each, 256 KiB total; relative paths only; `eula.txt` and `paper.jar` are reserved |
| `plugins[].provider` | `modrinth`, `hangar`, or `github`; `version` and `range` are mutually exclusive |
| `variables` | name `^[a-z][a-z0-9_-]{0,31}$`; `server*` and `template*` are reserved |

## Installation-time semantics

1. Resolve `mc_version` through PaperMC v3 for Paper/Folia/Velocity, or the Purpur API for
   Purpur, to a build URL and sha256.
2. Apply defaults and overrides; allocate a port when it is zero.
3. Render files; unresolved references fail the create request.
4. Store files, variables, `template_id`, and `template_version` on the server; the agent
   writes the files once, on first boot, and never overwrites them.

## Re-apply

Re-applying a template is a diffed job: file changes, jar changes, plugin changes. It never
runs implicitly, and it never touches world data. A rejected diff leaves the server untouched.

## Trust

An imported template pins every artifact by hash and shows the diff before anything is
written. A template that downloads jars is code; treat it as such in review and in the UI.
