# 0010 — Capability-detected filesystem features

**Status:** accepted · 2026-11-26

## Context

Reflink snapshots, ZFS clones and dev environments are the difference between seconds and
minutes. Requiring btrfs/XFS would refuse most hosts; pretending ext4 works the same would
ship a product that silently takes minutes per backup.

## Decision

Probe the filesystem at agent handshake and report per node: `reflink`, `zfs`, or `copy`. Use
the best available mechanism, fall back to `tar`, and **gate reflink-dependent features in the
UI** with an explicit reason. The installer offers a dedicated btrfs volume so ext4 hosts can
opt in.

## Consequences

- Every node can run DelphiMC; only some get instant snapshots.
- Features must check capability, not assume it: staging boot-check, dev environments and
  restore speed all consult the flag.
- Each server directory is created as its own subvolume/dataset at create time (required for
  btrfs snapshots / `zfs clone`), which is a layout decision, not a snapshot-time one.
- `tar` remains the honest backup fallback; what is forbidden is offering a feature whose
  safety or speed *depends* on reflink (staging boot-check, dev environments) and then
  running a degraded version anyway.
- **Forbidden:** silently substituting a slow path where the fast path is required, or a UI
  that offers an action the node cannot perform as designed.
