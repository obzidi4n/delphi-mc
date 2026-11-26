# Templates

**Status:** current design · implementation target M3b. ADR
[0009](../decisions/0009-templates-over-eggs.md).

Server creation is a **template instantiation**, not a forty-field form.

## Shape

```
Template {
  name, description, icon, builtin: bool
  base:        paper | purpur | velocity (v1.1)
  mc_version:  "1.21.4" | channel (latest|recommended)
  java:        21 | 25
  defaults:    memory, cpu, jvm_profile, startup, env
  files:       bundled config set (server.properties, spigot.yml, ...)
  plugins:     [{project, provider, version|range, required}]
  variables:   flat string map + defaults ({{server.name}}, {{port}}, {{memory}}, custom)
}
```

Variables are flat strings with defaults — no variable schema, no form generator. Validation
happens at instantiation and reports the variable name; that is enough for v1.0.

## Built-ins

Embedded in the binary: **Survival**, **Creative**, **Minigames**, **Dev**, and **Velocity
Network** (written at v1.1, shipped disabled until then).

## Instantiation

Creating a server resolves the template into concrete state: jar, image, ports, files,
plugins, memory. The server records `template_id` + `template_version` so drift is auditable.

The file set and jars are stored as sha256-addressed artifacts; the node fetches them through
the blob path. A create job carries hashes, never file bodies.

## Drift is never automatic

- Re-applying a newer template is an **explicit, diffed action**, never automatic. Template
  drift is how you break a running server.
- The diff covers files, jar and plugin versions; applying it is a normal job with an audit
  record.

## Import, export, trust

- Owners can edit, duplicate, and import/export templates as JSON/YAML.
- Sharing is file-based in v1.0; a git-backed registry (`delphi templates add <git-url>`)
  lands in v1.x. DelphiMC does not host a registry early — that is a moderation and
  supply-chain burden.
- **An imported template is never inert.** It renders a diff, requires approval, and pins
  plugin versions by hash. A template that downloads jars is code.
- When a template pins a range, import resolves it and records the resolved hash; the range
  is documentation, the hash is the contract.

## AI

`create_server(template, name, overrides)`. *"Like the other two"* resolves to a sibling
server's template and variable values, which is the common request and the reason templates
are structured rather than free-form.
