//go:build linux

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	btrfsSuperMagic = 0x9123683E
	zfsSuperMagic   = 0x2FC12FC1
)

// DetectFSCapability reports what the node can do for snapshots and clones.
func DetectFSCapability(dir string) string {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return "copy"
	}
	switch uint32(st.Type) {
	case btrfsSuperMagic:
		return "reflink"
	case zfsSuperMagic:
		return "zfs"
	default:
		return "copy"
	}
}

// EnsureServerDir creates dir as its own btrfs subvolume when the filesystem
// and tooling allow it, so snapshots and dev clones are cheap. A plain
// directory is the fallback, and the reported capability says so.
func EnsureServerDir(dir string) (string, error) {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", parent, err)
	}

	capability := DetectFSCapability(parent)
	if capability == "reflink" {
		if _, err := exec.LookPath("btrfs"); err == nil {
			// Already a subvolume or not btrfs: the fallback below decides.
			_ = exec.Command("btrfs", "subvolume", "create", dir).Run()
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return capability, nil
}
