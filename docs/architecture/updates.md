# Artifacts and updates

**Status:** M9 implemented. AI tools `plugins` (read) and `update_server` (approval) landed
here. The node-local CAS, staging-clone boot-check, and `auto-*` policies are deferred;
manual Spigot jar upload is available from the Plugins page.

## Artifact store

- The panel keeps a **content-addressed store** at `<data_dir>/artifacts/<sha256>`. Every
  plugin artifact is downloaded once, hashed, recorded in `artifacts`, and never written twice.
- Nodes do not have their own CAS yet. The panel serves immutable blobs at
  `GET /agent/blobs/{sha256}`; a node must present an enrolled, non-revoked certificate and
  re-hashes the body before installing it into `plugins/<name>`.
- The server jar still comes from its resolved upstream URL + sha256. Plugin downloads always
  go through the panel, so an egress-restricted node can install them without provider access.
- A later milestone moves the CAS onto nodes (reflink/hardlink) and adds `pull` mode; both
  reuse the same blob endpoint and digest checks.

## Resolution and delivery

| Provider | Project form | Selection |
|---|---|---|
| Modrinth | slug | newest paper build compatible with the MC version (major-family fallback) |
| Hangar | `owner/slug` | newest PAPER download for the MC version |
| GitHub | `owner/repo` or a `github.com` repo/release URL | latest release, preferring a `paper` jar asset |
| Jenkins | absolute Jenkins job URL | `lastSuccessfulBuild` jar artifact (`paper` preferred) |

A template may pin an exact `version` or set a **version-prefix range** (`5.5`, `v5.5`,
`5.5.71`). Both are resolved to a concrete version and sha256 at server creation. Semver
range expressions are not interpreted.

## Plugin catalog and per-server management

The Plugins page for a server has two jobs:

- **Catalog search** queries Modrinth's plugin search or Hangar's project search.
  The panel returns project metadata only; installing resolves the newest build
  compatible with that server's Minecraft version and downloads it into the
  artifact store.
- **Installed management** lists the managed set, checks for newer builds, applies
  all updates, and removes individual projects. Installing, removing, or uploading
  stages jars on disk without bumping the desired revision, so a running server keeps
  running; restart it when ready to load or unload the change. Applying updates is an
  explicit restart with the guarded boot check.

`POST /api/v1/servers/{id}/plugins` accepts `{provider, project, version?, range?}`.
`DELETE /api/v1/servers/{id}/plugins?project=...` removes one project. Both are
scoped and audited (`server.plugin.add`, `server.plugin.update`,
`server.plugin.remove`). GitHub remains a resolver-only provider because it has
no public project search API.

SpigotMC has no public API and its downloads normally require an authenticated
browser session, so DelphiMC does not scrape it. Instead,
`POST /api/v1/servers/{id}/plugins/upload` accepts a multipart form with a
`file` jar plus optional `project` and `version`. The jar is validated as a zip,
stored in the artifact CAS, recorded with provider `spigot` and policy `manual`,
and excluded from remote update checks. Re-uploading the same project replaces
the managed entry. This is the manual Spigot path; Modrinth/Hangar projects can
still be browsed and resolved normally.

The page checks for updates on load and every five minutes while it is open.
To make updates automatic, create a schedule with action `updates.apply`; the
existing scheduler runs detection, downloads changed artifacts, applies the new
revision, and the same boot-check/rollback path runs. The Plugins page exposes
this as a cron form.

## Policies

| Policy | Meaning |
|---|---|
| `pinned` | Excluded from automatic updates; detection still shows it and an explicit apply moves it |
| `range` | Move within the prefix |
| `latest` | Follow the newest compatible build |

`auto-minor`/`auto-any` and version-scheme classification are deferred until the staged
boot-check exists.

## Applying an update

`POST /servers/{id}/updates/apply`:

1. Resolve every non-pinned plugin and compare versions/hashes.
2. Download changed artifacts into the CAS and record the new managed set on the server.
3. Bump the revision and sync the node. A running server restarts.
4. Boot-check: watch the new revision for up to 180 s. `running` verifies the update;
   `exited`/`error`/timeout restores the previous jar + plugin set, restarts it, and audits
   `server.update.rollback`.

The response returns before the boot-check finishes (`boot_check: pending`); the outcome is
audited as `server.update.verified` or `server.update.rollback`. This is an **in-place guarded
apply**, not the staged clone in the original design: a bad update can start the real server
before it is rolled back. A staged clone (scratch port, bridge registration suppressed,
reflink required) remains the gate for automatic policies.

Rollback uses the previous plugin artifacts, which stay in the CAS as long as a live server or
the GC age window references them. GC is admin-only and skips referenced and recent blobs.

## Garbage collection

`POST /api/v1/artifacts/gc?min_age_seconds=3600` (default one hour) scans
`servers.plugins` for live references, then deletes unreferenced blob files and rows. The
default window protects a rollback that is still in flight. No refcount is stored, so it
cannot drift.

## Deferred

- Staged boot-checks and `auto-*` policies (gated on reflink/zfs capability).
- Node-local CAS plus a `pull` delivery mode.
- Release channels / Paper and Velocity jar update policy. M9 updates managed plugins only.
- Dev/staging clone and promote flow (planned M17); applying updates still uses the
  in-place guarded restart until the staged clone exists.
