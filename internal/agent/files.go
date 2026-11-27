package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"delphi.dev/delphi/internal/wire"
)

// MaxFileBytes caps reads and writes; larger files are not editable here.
const MaxFileBytes = 1 << 20

// serverRoot resolves a server directory, rejecting unsafe ids.
func (s *Supervisor) serverRoot(serverID string) (string, error) {
	if !strings.HasPrefix(serverID, "s_") || strings.ContainsAny(serverID, "/\\.") || len(serverID) > 40 {
		return "", errors.New("invalid server id")
	}
	return filepath.Join(s.DataDir, "servers", serverID), nil
}

// safePath joins rel under root and guarantees containment.
func cleanupRoot(root string) string { return filepath.Clean(root) }

func safePath(root, rel string) (string, error) {
	if strings.ContainsRune(rel, '\x00') || filepath.IsAbs(rel) {
		return "", errors.New("path must be relative")
	}
	cleanRoot := filepath.Clean(root)
	full := filepath.Clean(filepath.Join(cleanRoot, filepath.FromSlash(rel)))
	if full != cleanRoot && !strings.HasPrefix(full, cleanRoot+string(filepath.Separator)) {
		return "", errors.New("path escapes the server directory")
	}
	checkRoot := cleanRoot
	if resolvedRoot, err := filepath.EvalSymlinks(cleanRoot); err == nil {
		checkRoot = resolvedRoot
	}
	inside := func(path string) bool {
		return path == checkRoot || strings.HasPrefix(path, checkRoot+string(filepath.Separator))
	}
	if full != cleanupRoot(cleanRoot) {
		// The file itself (when it exists) and its parent must both resolve inside.
		if resolved, err := filepath.EvalSymlinks(full); err == nil && !inside(resolved) {
			return "", errors.New("path resolves outside the server directory")
		}
		if resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(full)); err == nil && !inside(resolvedParent) {
			return "", errors.New("parent resolves outside the server directory")
		}
	}
	return full, nil
}

// FileList returns a directory listing (relative path "" is the root).
func (s *Supervisor) FileList(serverID, rel string) ([]wire.FileEntry, error) {
	root, err := s.serverRoot(serverID)
	if err != nil {
		return nil, err
	}
	full, err := safePath(root, rel)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	out := make([]wire.FileEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, wire.FileEntry{
			Name: entry.Name(), Dir: entry.IsDir(), Size: info.Size(), ModTime: info.ModTime().UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// FileRead returns UTF-8 text up to MaxFileBytes.
func (s *Supervisor) FileRead(serverID, rel string) (string, string, int64, error) {
	root, err := s.serverRoot(serverID)
	if err != nil {
		return "", "", 0, err
	}
	full, err := safePath(root, rel)
	if err != nil {
		return "", "", 0, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", "", 0, err
	}
	if info.IsDir() {
		return "", "", 0, errors.New("path is a directory")
	}
	if info.Size() > MaxFileBytes {
		return "", "", 0, fmt.Errorf("file is larger than %d bytes", MaxFileBytes)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", "", 0, err
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return "", "", 0, errors.New("binary files are not editable")
	}
	sum := sha256.Sum256(data)
	return string(data), hex.EncodeToString(sum[:]), info.Size(), nil
}

// FileWrite atomically replaces a text file.
func (s *Supervisor) FileWrite(serverID, rel, content string) (string, error) {
	root, err := s.serverRoot(serverID)
	if err != nil {
		return "", err
	}
	if len(content) > MaxFileBytes {
		return "", fmt.Errorf("content is larger than %d bytes", MaxFileBytes)
	}
	full, err := safePath(root, rel)
	if err != nil {
		return "", err
	}
	if full == filepath.Clean(root) {
		return "", errors.New("path must name a file")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:]), nil
}

// HandleFileRequest executes one file frame from the panel.
func HandleFileRequest(supervisor *Supervisor, req wire.FileRequest) wire.FileResult {
	result := wire.FileResult{Type: wire.TypeFileResult, RequestID: req.RequestID}
	switch req.Op {
	case "list":
		entries, err := supervisor.FileList(req.ServerID, req.Path)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Entries = entries
	case "read":
		content, sum, size, err := supervisor.FileRead(req.ServerID, req.Path)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Content, result.SHA256, result.Size = content, sum, size
	case "write":
		sum, err := supervisor.FileWrite(req.ServerID, req.Path, req.Content)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.SHA256 = sum
	default:
		result.Error = "unknown file operation"
	}
	return result
}
