package upgrade

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSwapAndRollback(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "delphi")
	candidate := filepath.Join(dir, "new-delphi")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Swap(dest, candidate); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "new" {
		t.Fatalf("dest = %q", got)
	}
	backup, _ := os.ReadFile(RollbackPath(dest))
	if string(backup) != "old" {
		t.Fatalf("rollback = %q", backup)
	}
	if err := Rollback(dest); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(dest)
	if string(got) != "old" {
		t.Fatalf("restored = %q", got)
	}
}
