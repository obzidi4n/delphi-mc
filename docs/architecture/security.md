# Security model

**Status:** current. Landed: node identity (M1), users/API keys/sessions, scopes, and audit
(M3a). Hardening and 2FA land at M12.

## Trust boundaries

The container socket is root-equivalent. It is the largest attack surface in the product and
the reason Minecraft panels get popped.

- **Rootless Podman is the default.** Docker is supported and documented as root-equivalent.
- **No socket proxy.** It cannot restrict `exec`, so it adds a component and no security.
- The agent is the only client of the runtime; the socket is never exposed to the panel or
  the network.
- The agent runs as a dedicated `delphi` user under systemd, never root, with the rootless
  prerequisites checked at install ([install](../operations/install.md)).

The broker-free transport means there is no second credential system to secure: one node
certificate covers the agent WebSocket and the blob endpoint.

## Identity

**Enrollment:** a single-use, short-TTL token → the agent generates a keypair → CSR → the
panel signs a node certificate from the org CA. That identity authenticates the agent
WebSocket channel (console, metrics, claims) and the blob download endpoint. Rotation at 30
days; revocation records `revoked_at` and is checked on every reconnect. Details in
[node enrollment](../operations/node-enrollment.md).

**Bridge:** each server has its own token in the plugin handshake; the socket is created by
the agent inside that server's container namespace.

**Users:** Argon2id password hashing, httpOnly `Secure` SameSite=Lax session cookies, and a
same-origin check on `POST /login` and `POST /logout` (no token yet). TOTP 2FA lands with
hardening (M12). The panel must be served over TLS; the agent channel refuses plaintext.

**v1.0 account model:** one owner plus admin-created users with generated passwords. No email
invites — SMTP only arrives with alert email at M11.

**Tokens:** CLI (and TUI at v1.1) tokens live in the OS keychain, or a `0600` file where no
keyring exists, and never in shell history. API tokens are device-scoped, hashed at rest,
revocable, and scoped like any other principal.

## Authorization

- Two scope shapes: `org:<org>:<verb>` for org-wide actions (`read`, `admin`,
  `server:create`, `user:read`, `user:admin`) and `server:<id>:<verb>` (`read`, `update`,
  `power`, `delete`). `*` matches one segment, so `server:*:read` grants reads on every
  server and `server:s_1:*` grants everything on one ([capability layer](capability-layer.md)).
- Roles map to profiles: `owner` = `*`; `admin` = org read/admin/create plus `server:*:*`;
  `developer` = org read/create plus server read/update/power; `viewer` = org read plus
  server read. An org binding applies the profile with wildcards; a server binding narrows
  it to that id; a group binding expands to the group's member servers.
- Only an owner can grant `owner`, and the last owner cannot be demoted. API keys store
  their scope set at mint time; session principals resolve scopes from `role_bindings`.
- The UI enforces the same capabilities as the API: the Team page needs `user:read`/
  `user:admin`, group mutation needs org `admin`, and the console WebSocket needs
  `server:<id>:read`. UI mutations are same-origin checked, and the WS upgrade is
  origin-checked and accepts a session cookie or bearer key.
- Every mutation is audited with `Via` (ui/cli/api/ai) and, for AI actions, `Model`.
- The AI tool registry is filtered by scope and capability, and the capability layer checks
  again — two independent gates for the same rule.

## Secrets at rest

Secrets (database credentials, provider keys, the Velocity forwarding secret) are encrypted
with an org key and never logged. They are never rendered in the UI except through an
explicit, audited **one-time reveal** (database credentials are useless to the owner
otherwise) and a "set/rotate" affordance.

Backups are encrypted with a key separate from Postgres; they contain worlds, configs and
often third-party credentials. The master key is exported at install and **is not recoverable
from the panel** — losing it makes every encrypted backup unreadable. Where it lives (host
env, mounted file, KMS) and how it rotates is open item 4 in
[`../plan/decision-log.md`](../plan/decision-log.md), decided at M0 before the first secret is
stored.

## Isolation

- Backends publish no host ports and live only on their network's container network; the
  proxy is the only public listener (v1.1).
- Containers: non-root, read-only rootfs, `tmpfs /tmp`, `--cap-drop=ALL`,
  `no-new-privileges`, default seccomp, no host network, explicit port publish.
- A compromised plugin can still reach the network. Egress policy is allow-by-default in
  v1.0; per-network egress is a v1.1 consideration, stated here so omission is a decision,
  not an oversight.

## Transport and browser surface

- Agent and browser channels are WebSocket over TLS; the WS upgrade requires a session
  cookie and an Origin check, not just a bearer header.
- One WebSocket per browser tab, topic-multiplexed with server-side rate limits per topic.
- The panel is single-replica in v1 and claims no HA; there is no leader-election surface to
  attack.

## Prompt injection

The AI's threat model — attacker-controlled chat, logs, plugin metadata — is covered in
[ai.md](ai.md#guardrails). The short version: untrusted text is data, mutations need an
approval card, and redaction applies before persistence as well as before egress.
