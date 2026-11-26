# Template schema

**Status:** design · implemented at M3b, when the canonical JSON Schema file lands and this page
links to it. Rationale: [templates](../architecture/templates.md).

Import/export format is JSON (YAML accepted on import; export is JSON). A template is data
plus an optional file set; nothing in it executes at import time.

```json
{
  "schema": 1,
  "name": "Survival",
  "description": "Paper survival server with the house ruleset",
  "icon": "survival",
  "builtin": true,
  "base": "paper",
  "mc_version": "1.21.4",
  "java": 21,
  "defaults": {
    "memory_mb": 4096,
    "cpu_cores": 2.0,
    "jvm_profile": "aikar",
    "startup": "paper.jar --nogui",
    "env": {}
  },
  "files": [
    { "path": "server.properties", "mode": "0644", "content": "..." },
    { "path": "plugins/config/essentials.yml", "mode": "0644", "source": "files/essentials.yml" }
  ],
  "plugins": [
    { "project": "essentialsx", "provider": "modrinth", "version": "2.20.1",
      "sha256": "…", "required": true }
  ],
  "variables": {
    "motd": { "default": "A DelphiMC server" },
    "difficulty": { "default": "normal" }
  }
}
```

## Field rules

| Field | Rule |
|---|---|
| `base` | `paper` or `purpur` in v1.0; `velocity` with networks (v1.1) |
| `mc_version` | A literal version, or `latest` / `recommended` resolved at instantiation and then pinned |
| `java` | `21` or `25` (support floor: Paper 1.20.5+) |
| `files[].path` | Relative, no `..`, no absolute paths, no symlinks |
| `files[].content` / `source` | Exactly one; `source` is resolved from the template bundle at import |
| `plugins[].sha256` | Required after resolution; a version range without a hash is rejected on import |
| `variables` | Flat string map; substitution is `{{name}}` only, no expressions |

## Installation-time semantics

1. Resolve `mc_version` and each plugin (`version|range` → concrete build + hash).
2. Render variables into files; unresolved variables fail the instantiation, not the server.
3. Write files atomically; the bundle and every jar are sha256-addressed artifacts fetched over
   the blob path — the job references hashes, never bytes.
4. Record `template_id` + `template_version` on the server.

## Re-apply

Re-applying a template is a diffed job: file changes, jar changes, plugin changes. It never
runs implicitly, and it never touches world data. A rejected diff leaves the server untouched.

## Trust

An imported template pins every artifact by hash and shows the diff before anything is
written. A template that downloads jars is code; treat it as such in review and in the UI.
