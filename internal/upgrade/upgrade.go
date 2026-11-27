// Package upgrade swaps a panel binary while keeping one rollback slot.
package upgrade

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RollbackPath is the previous binary kept next to dest.
func RollbackPath(dest string) string { return dest + ".rollback" }

// Swap replaces dest with candidate, saving the current dest in the rollback
// slot. The running process is unaffected until it restarts.
func Swap(dest, candidate string) (string, error) {
	if dest == "" || candidate == "" {
		return "", errors.New("upgrade: destination and candidate are required")
	}
	dest, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(candidate)
	if err != nil {
		return "", fmt.Errorf("upgrade: candidate: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("upgrade: candidate is not a regular file")
	}
	if _, err := os.Stat(dest); err != nil {
		return "", fmt.Errorf("upgrade: destination: %w", err)
	}
	backup := RollbackPath(dest)
	if err := copyAtomic(dest, backup, 0o755); err != nil {
		return "", fmt.Errorf("upgrade: save rollback: %w", err)
	}
	if err := copyAtomic(candidate, dest, 0o755); err != nil {
		return "", fmt.Errorf("upgrade: install: %w", err)
	}
	return backup, nil
}

// Rollback restores the rollback slot over dest.
func Rollback(dest string) error {
	if dest == "" {
		return errors.New("upgrade: destination is required")
	}
	dest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	backup := RollbackPath(dest)
	if _, err := os.Stat(backup); err != nil {
		return fmt.Errorf("upgrade: no rollback binary: %w", err)
	}
	if err := copyAtomic(backup, dest, 0o755); err != nil {
		return fmt.Errorf("upgrade: rollback: %w", err)
	}
	return nil
}

func copyAtomic(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".upgrade-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}
