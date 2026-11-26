# Artifacts and updates

**Status:** current design · implementation target M9. AI tools `plugins` and `update_server`
unlock here.

## Artifact store

- Content-addressed on the node: `sha256 -> blob`, reflinked/hardlinked into server dirs, so
  forty servers on one Paper build share one file.
- The CAS is a **node-local cache**. Cross-node restore and S3 offload reuse the same blob
  endpoint in reverse.
- Hardlinking is safe only for immutable files (jars). Anything the server writes in place is
  copied, never linked.
- Artifacts carry `refcount`/`last_referenced` so a GC job can reclaim them; a blob is not
  deleted while any server or backup references it.

## Resolution and delivery

Resolution happens in the panel (`internal/mc`); blobs never ride the agent channel.

| Mode | Behaviour | Use |
|---|---|---|
| `pull` | Agent fetches upstream directly from the panel-pinned URL + hash; requires egress | Default |
| `proxy` | Agent downloads from the panel's authenticated `/agent/blobs/<sha256>` | Egress-restricted nodes |

The panel decides what a version *is*; the node decides whether it can reach it. Both paths
verify the digest before the blob enters the CAS.

## Providers

One interface: `Resolve(project, channel) -> Version{url, hash, deps}`.

| Provider | Notes |
|---|---|
| Modrinth | Good API; the default for plugins |
| Hangar | PaperMC plugins |
| GitHub releases | Good, needs rate-limit care |
| SpigotMC | **No API** — manual upload via `POST /artifacts` (sha256-addressed), supported from day one |

Velocity version resolution arrives with networks at M14.

## Applying an update

Download → verify digest → fsync → rename. The previous jar is kept for rollback; servers
are never patched in place while running.

Update policy per plugin:

| Policy | Meaning |
|---|---|
| `pin` | Never move; the owner updates explicitly |
| `notify` | Surface the update, apply on request |
| `auto-minor` | Apply non-breaking updates (see caveat) |
| `auto-any` | Apply anything the provider publishes |

**Caveat:** many plugins do not publish semantic versions. When the version scheme cannot be
classified, `auto-minor` degrades to `notify` rather than guessing — an update that cannot be
classified is not automatically minor.

## Staging boot-check

The trust mechanism for `auto-*`:

1. Clone the server (requires `reflink`/`zfs`; a full copy per update is too expensive).
2. Apply the update to the clone.
3. Boot it on a **scratch port assigned by the panel** (part of the boot-check job, counted
   against node capacity until teardown), with proxy registration suppressed and its own
   bridge token — a staged backend must never announce itself to the network.
4. Wait for `Done (x.xxs)!`, scan startup output for errors.
5. Promote atomically; on failure, discard the clone and alert.

Defaults: **on for `auto-*`**, off for `notify`, gated on filesystem capability. The UI must
show why a boot-check is unavailable instead of silently skipping it.

On a `copy` node (ext4) the boot-check is unavailable, so **`auto-*` policies cannot be
selected** — the policy is downgraded to `notify` at set time, with the reason shown. An
unchecked automatic update is never applied just because the check could not run.

Boot-checks consume capacity: a bulk `auto-any` rollout across many servers is scheduled
serially per node unless the operator raises concurrency.

## Rollback

The previous artifact plus the previous config revision are both retained, so a failed update
is reversible without a backup restore. A rollback is itself an update job, audited like any
other mutation.
