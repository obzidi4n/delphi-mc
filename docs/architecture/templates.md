# Templates

**Status:** current at M3b. ADR [0009](../decisions/0009-templates-over-eggs.md).

Server creation is a **template instantiation**, not a forty-field form.

## Shape

```
Template {
  schema_version: 1
  name, description, base: paper | velocity
  mc_version: "1.21.11" | channel (latest | recommended)
  java: 8 | 17 | 21 | 25
  defaults: memory_mb, cpu_cores, port (0 = allocate)
  files: map<relative path, content>   # small config sets only
  plugins: [{project, provider, version|range, required}]  # metadata until M9
  variables: map<name, {description, default, required}>
}
```

Built-ins are embedded JSON and seeded into `templates` with a null org; an org's imports sit
next to them and both list through one cursor. `purpur` is rejected until provider plugins
(M9) add a resolver with a sha256. Plugin jars are not downloaded yet: the provider manager
lands at M9, so `plugins` is metadata that instantiation records but does not install.

Variables are flat strings with defaults — no variable schema, no form generator. Validation
happens at instantiation and reports the variable name; that is enough for v1.0.

## Built-ins

Embedded in the binary: **Survival**, **Creative**, **Minigames**, **Dev**, and **Velocity
Network** (written at v1.1, shipped disabled until then).

## Instantiation

Creating a server resolves the template into concrete state:

1. `mc_version` (literal or channel) is resolved through the PaperMC v3 API (`fill.papermc.io`)
   to a build, an https jar URL, and its sha256. The result is cached briefly.
2. `defaults` are applied, then overrides (`memory_mb`, `cpu_cores`, `port`, `java`, custom
   variables). A zero port is allocated from the node's pool before rendering, so variables
   see the final port.
3. Variables are substituted into the file set; an unknown reference or override fails the
   request. The rendered server gets `base`, `files`, `variables`, `template_id` and
   `template_version`.
4. The agent writes the files on first prepare and never overwrites them afterwards: template
   application is a first-boot action, later edits belong to the operator. The container
   listens on the server's assigned port directly (published 1:1), which is what templates
   render.

For Velocity, the agent skips `eula.txt`/`server.properties` and launches without `--nogui`.

## Drift is never automatic

- Re-applying a newer template is an **explicit, diffed action**, never automatic. Template
  drift is how you break a running server. The diff/apply action is not built yet; the
  server's `template_id`/`template_version` is recorded now so it can be audited.
- The diff will cover files, jar and plugin versions; applying it is a normal job with an
  audit record.

## Import, export, trust

- Org templates are imported (API `POST /api/v1/templates`, or `delphi templates import`);
  `delphi templates export` prints JSON or YAML. Edit/duplicate land with the template editor.
- Sharing is file-based in v1.0; a git-backed registry (`delphi templates add <git-url>`)
  lands in v1.x. DelphiMC does not host a registry early — that is a moderation and
  supply-chain burden.
- **An imported template is not inert.** Import validates the whole body and the files are
  written on first boot; until the diff/approval flow lands, only org admins can import.
  Plugin pins are validated but resolved only at M9.
- When a template pins a range, M9 resolves it and records the resolved hash; the range is
  documentation, the hash is the contract.

## AI

`create_server(template, name, overrides)`. *"Like the other two"* resolves to a sibling
server's template and variable values, which is the common request and the reason templates
are structured rather than free-form.
