package agent

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"delphi.dev/delphi/internal/wire"
)

// HandleBackupCreate quiesces the server, writes a tar.gz, uploads it to the
// panel, and resumes the server.
func HandleBackupCreate(ctx context.Context, sup *Supervisor, bridge *Bridge, client *http.Client, panelURL string, req wire.BackupRequest) wire.BackupResult {
	res := wire.BackupResult{Type: wire.TypeBackupResult, RequestID: req.RequestID}
	if bridge == nil || client == nil || panelURL == "" {
		res.Error = "backup transport is not configured"
		return res
	}
	dir := filepath.Join(sup.DataDir, "servers", req.ServerID)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		res.Error = "the server directory does not exist"
		return res
	}

	running := false
	if state, err := sup.Runtime.State(ctx, req.ServerID); err == nil && (state == "running" || state == "paused") {
		running = true
	}

	quiesced := false
	stopped := false
	var resumeOnce sync.Once
	resume := func() {
		resumeOnce.Do(func() {
			if quiesced {
				_ = bridge.Command(wire.BridgeCommandFrame{
					Type: wire.TypeBridgeCommand, ServerID: req.ServerID, ID: "bkp-on2-" + req.BackupID,
					Action: "command", Command: "save-on",
				})
			}
			if stopped {
				_ = sup.Runtime.Start(context.Background(), req.ServerID)
			}
		})
	}
	if running {
		if err := bridge.Command(wire.BridgeCommandFrame{
			Type: wire.TypeBridgeCommand, ServerID: req.ServerID, ID: "bkp-off-" + req.BackupID,
			Action: "command", Command: "save-off",
		}); err == nil {
			if err := bridge.Command(wire.BridgeCommandFrame{
				Type: wire.TypeBridgeCommand, ServerID: req.ServerID, ID: "bkp-flush-" + req.BackupID,
				Action: "command", Command: "save-all flush",
			}); err == nil {
				quiesced = true
				time.Sleep(1500 * time.Millisecond)
			} else {
				_ = bridge.Command(wire.BridgeCommandFrame{
					Type: wire.TypeBridgeCommand, ServerID: req.ServerID, ID: "bkp-on-" + req.BackupID,
					Action: "command", Command: "save-on",
				})
			}
		}
		if !quiesced {
			if err := sup.Runtime.Stop(ctx, req.ServerID); err != nil {
				res.Error = "stop for backup: " + err.Error()
				return res
			}
			stopped = true
		}
		defer resume()
	}

	tmpDir := filepath.Join(sup.DataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		res.Error = err.Error()
		return res
	}
	tmp, err := os.CreateTemp(tmpDir, req.BackupID+"-*.tar.gz")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	if err := writeTarGz(io.MultiWriter(tmp, hasher), dir); err != nil {
		tmp.Close()
		res.Error = err.Error()
		return res
	}
	if err := tmp.Close(); err != nil {
		res.Error = err.Error()
		return res
	}
	resume()

	file, err := os.Open(tmpName)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	url := strings.TrimRight(panelURL, "/") + "/agent/backups/" + req.BackupID
	upload, err := http.NewRequestWithContext(ctx, http.MethodPost, url, file)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	upload.ContentLength = info.Size()
	upload.Header.Set("Content-Type", "application/gzip")
	upload.Header.Set("X-Delphi-SHA256", hex.EncodeToString(hasher.Sum(nil)))
	resp, err := client.Do(upload)
	if err != nil {
		res.Error = "upload: " + err.Error()
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		res.Error = fmt.Sprintf("upload: %s: %s", resp.Status, strings.TrimSpace(string(body)))
		return res
	}
	res.OK = true
	res.Size = info.Size()
	return res
}

// HandleBackupRestore downloads a snapshot and replaces the server directory.
func HandleBackupRestore(ctx context.Context, sup *Supervisor, client *http.Client, panelURL string, req wire.BackupRequest) wire.BackupResult {
	res := wire.BackupResult{Type: wire.TypeBackupResult, RequestID: req.RequestID}
	if client == nil || panelURL == "" {
		res.Error = "backup transport is not configured"
		return res
	}
	dir := filepath.Join(sup.DataDir, "servers", req.ServerID)
	if _, err := os.Stat(dir); err != nil {
		res.Error = "the server directory does not exist"
		return res
	}

	state, _ := sup.Runtime.State(ctx, req.ServerID)
	running := state == "running" || state == "paused"
	restored := false
	if running {
		if err := sup.Runtime.Stop(ctx, req.ServerID); err != nil {
			res.Error = "stop for restore: " + err.Error()
			return res
		}
		defer func() {
			if !restored {
				_ = sup.Runtime.Start(context.Background(), req.ServerID)
			}
		}()
	}

	tmpDir := filepath.Join(sup.DataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		res.Error = err.Error()
		return res
	}
	tmp, err := os.CreateTemp(tmpDir, req.BackupID+"-*.tar.gz")
	if err != nil {
		res.Error = err.Error()
		return res
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	url := strings.TrimRight(panelURL, "/") + "/agent/backups/" + req.BackupID
	download, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	resp, err := client.Do(download)
	if err != nil {
		res.Error = "download: " + err.Error()
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		res.Error = "download: " + resp.Status
		return res
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		tmp.Close()
		res.Error = "download: " + err.Error()
		return res
	}
	if err := tmp.Close(); err != nil {
		res.Error = err.Error()
		return res
	}
	if want := resp.Header.Get("X-Delphi-SHA256"); want != "" && want != hex.EncodeToString(hasher.Sum(nil)) {
		res.Error = "backup sha256 mismatch"
		return res
	}

	old := dir + ".restore-old"
	_ = os.RemoveAll(old)
	if err := os.Rename(dir, old); err != nil {
		res.Error = "replace server directory: " + err.Error()
		return res
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_ = os.Rename(old, dir)
		res.Error = err.Error()
		return res
	}
	src, err := os.Open(tmpName)
	if err != nil {
		_ = os.RemoveAll(dir)
		_ = os.Rename(old, dir)
		res.Error = err.Error()
		return res
	}
	defer src.Close()
	if err := extractTarGz(src, dir); err != nil {
		_ = os.RemoveAll(dir)
		_ = os.Rename(old, dir)
		res.Error = "extract: " + err.Error()
		return res
	}
	if err := os.RemoveAll(old); err != nil {
		res.Error = "remove previous data: " + err.Error()
		return res
	}
	restored = true
	res.OK = true
	return res
}

// writeTarGz archives the server directory minus transient data.
func writeTarGz(dst io.Writer, dir string) error {
	gz := gzip.NewWriter(dst)
	tw := tar.NewWriter(gz)
	root := filepath.Clean(dir)
	err := filepath.Walk(root, func(full string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		slashed := filepath.ToSlash(rel)
		if excluded(slashed, info) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = slashed
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			file, err := os.Open(full)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tw, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	})
	if err != nil {
		tw.Close()
		gz.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		gz.Close()
		return err
	}
	return gz.Close()
}

func excluded(rel string, info os.FileInfo) bool {
	base := path.Base(rel)
	if base == "session.lock" {
		return true
	}
	switch rel {
	case "logs", "cache", "crash", "backups", ".delphi-state.json", "plugins/.paper-remapped":
		return true
	}
	return false
}

// extractTarGz restores a snapshot under dir, refusing escapes.
func extractTarGz(src io.Reader, dir string) error {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	root, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(header.Name)
		if name == "." || strings.HasPrefix(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe path %q", header.Name)
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			return fmt.Errorf("unsafe path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		default:
			return fmt.Errorf("unsupported tar entry %q", header.Name)
		}
	}
}
