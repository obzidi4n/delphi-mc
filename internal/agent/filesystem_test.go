package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureServerDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "servers", "s_1")

	capability, err := EnsureServerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	switch capability {
	case "reflink", "zfs", "copy":
	default:
		t.Fatalf("unexpected capability %q", capability)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("server dir is not a directory")
	}

	// Idempotent: a second call succeeds.
	if _, err := EnsureServerDir(dir); err != nil {
		t.Fatalf("second call: %v", err)
	}
}
