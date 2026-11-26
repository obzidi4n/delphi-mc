# Security policy

DelphiMC is pre-release (M0). No version is currently supported with security updates; this
policy describes how to report issues and what is considered in scope.

## Reporting

Use GitHub's **private vulnerability reporting** (Security → Report a vulnerability) on this
repository. Do not open a public issue for a suspected vulnerability.

Include the affected component (panel, agent, bridge plugin, docs), a reproduction or proof of
concept, the impact, and the commit or version.

## In scope

- The container/runtime boundary and the agent (rootless Podman defaults, no socket proxy).
- The agent channel and enrollment: token → keypair → CSR → signed node certificate.
- The capability layer and RBAC: any path that mutates state without a `Principal` check.
- Prompt-injection paths into the AI operator (chat, logs, plugin metadata, config files).
- Secret handling: a secret in a log, an API response, or a backup readable without the master
  key.

## Out of scope

- Reports requiring root on the host.
- Docker configured root-equivalent (documented behavior, not a vulnerability).
- Missing hardening on a version that is not supported.

## Response

Best effort while pre-release: aim to acknowledge within 7 days, and to fix or mitigate before
public disclosure. Reporters are credited unless they ask otherwise.
