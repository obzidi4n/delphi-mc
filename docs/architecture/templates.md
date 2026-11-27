# Templates

**Status:** current at M3b. ADR [0009](../decisions/0009-templates-over-eggs.md).

Server creation is a **template instantiation**, not a forty-field form.

## Shape

```
Template {
  schema_version: 1
  name, description, base: paper | purpur | folia | velocity
  mc_version: "1.21.11" | channel (latest | recommended)
  java: 8 | 17 | 21 | 25
  defaults: memory_mb, cpu_cores, port (0 = allocate)
  files: map<relative path, content>   # small config sets only
  plugins: [{project, provider, version|range, required}]  # resolved and installed
  variables: map<name, {description, default, required}>
}
```

Built-ins are embedded JSON and seeded into `templates` with a null org; an org's imports sit
next to them and both list through one cursor. Purpur resolves through its own API and is
supported wherever Paper is supported; the jar's sha256 is computed by the panel because the
Purpur API exposes only an md5.

Plugins are resolved at instantiation through `internal/plugins`:

| Provider | Project form | Selection |
|---|---|---|
| `modrinth` | slug (`luckperms`) | newest paper build whose `game_versions` include the server's MC version (major-family fallback), or a pin/range |
| `hangar` | `owner/slug` (`ViaVersion/ViaVersion`) | newest PAPER download for the MC version, or a pin/range |
| `github` | `owner/repo` | latest release (or the pinned tag), preferring a `paper` jar asset |

Each artifact is downloaded once into the panel CAS (`<data_dir>/artifacts/<sha256>`), recorded
in `artifacts`, and stored on the server as `servers.plugins` with the resolved version, stable
name (`luckperms.jar`), and sha256. Nodes fetch it from `GET /agent/blobs/{sha256}` over their
node certificate and verify the hash before install.

Variables are flat strings with defaults — no variable schema, no form generator. Validation
happens at instantiation and reports the variable name; that is enough for v1.0.

## Built-ins

Embedded in the binary: **Survival**, **Creative**, **Minigames**, **Dev**, **Purpur**,
**Folia**, and **Velocity Network**. The Paper bridge is not installed on Folia or Velocity;
Folia therefore runs without bridge-provided player/TPS/chat telemetry.

## Instantiation

Creating a server resolves the template into concrete state:

1. `mc_version` (literal or channel) is resolved through the PaperMC v3 API
   (`fill.papermc.io`) for Paper/Folia/Velocity, or the Purpur API for Purpur, to a build, an
   https jar URL, and its sha256. Purpur hashes the downloaded jar because its API omits sha256. The
   result is cached briefly.
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

## UI editing

The Templates page lists built-ins and org-owned templates and opens the canonical JSON body
in an editor. Built-ins are read-only; **Save as copy** creates an org-owned template after
validation. Saving an org-owned template increments its revision and is audited as
`template.update`. Imports remain the path for external YAML/JSON files.

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
  Plugin pins and ranges are resolved at creation.
- When a template sets a range, resolution records the concrete version and hash; the prefix is
  documentation, the hash is the contract.

## AI

`create_server(template, name, overrides)`. *"Like the other two"* resolves to a sibling
server's template and variable values, which is the common request and the reason templates
are structured rather than free-form.
