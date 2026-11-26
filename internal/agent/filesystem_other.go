//go:build !linux

package agent

import (
	"fmt"
	"os"
)

// DetectFSCapability reports "copy" outside Linux: the panel's node is a
// container host, and on a macOS dev machine the server dir is a shared folder.
func DetectFSCapability(string) string { return "copy" }

// EnsureServerDir creates a plain directory; subvolumes are a Linux concern.
func EnsureServerDir(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return "copy", nil
}
