// Package artifact stores jars by sha256 under the panel's data directory so
// nodes download each artifact once and the panel can garbage-collect it.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Store is a content-addressed directory.
type Store struct {
	Dir string
}

// New creates the artifact directory.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("artifact: directory is required")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("artifact: create %s: %w", dir, err)
	}
	return &Store{Dir: dir}, nil
}

// Path returns the file path for a content address.
func (s *Store) Path(sha string) (string, error) {
	if !shaPattern.MatchString(sha) {
		return "", errors.New("artifact: invalid sha256")
	}
	return filepath.Join(s.Dir, sha), nil
}

// Put streams an artifact to disk, computing its sha256. When expect is set
// the computed hash must match it.
func (s *Store) Put(r io.Reader, expect string, maxBytes int64) (string, int64, error) {
	if expect != "" && !shaPattern.MatchString(expect) {
		return "", 0, errors.New("artifact: invalid expected sha256")
	}
	tmp, err := os.CreateTemp(s.Dir, ".download-*")
	if err != nil {
		return "", 0, fmt.Errorf("artifact: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	var src io.Reader = r
	if maxBytes > 0 {
		src = io.LimitReader(r, maxBytes+1)
	}
	written, err := io.Copy(io.MultiWriter(tmp, hasher), src)
	closeErr := tmp.Close()
	if err != nil {
		return "", 0, fmt.Errorf("artifact: download: %w", err)
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("artifact: close: %w", closeErr)
	}
	if maxBytes > 0 && written > maxBytes {
		return "", 0, fmt.Errorf("artifact: exceeds %d bytes", maxBytes)
	}
	sha := hex.EncodeToString(hasher.Sum(nil))
	if expect != "" && sha != expect {
		return "", 0, fmt.Errorf("artifact: sha256 mismatch: got %s, want %s", sha, expect)
	}
	target, err := s.Path(sha)
	if err != nil {
		return "", 0, err
	}
	if _, err := os.Stat(target); err == nil {
		return sha, written, nil
	}
	if err := os.Rename(tmpName, target); err != nil {
		return "", 0, fmt.Errorf("artifact: publish: %w", err)
	}
	return sha, written, nil
}

// List returns the content addresses currently on disk.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, fmt.Errorf("artifact: list: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !shaPattern.MatchString(entry.Name()) {
			continue
		}
		out = append(out, entry.Name())
	}
	return out, nil
}

// Remove deletes an artifact, ignoring a missing file.
func (s *Store) Remove(sha string) error {
	path, err := s.Path(sha)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("artifact: remove: %w", err)
	}
	return nil
}
