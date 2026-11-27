package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"delphi.dev/delphi/internal/wire"
)

// HandleDevClone copies a server directory into a new temporary server and
// rewrites only the settings needed to run it in isolation.
func HandleDevClone(ctx context.Context, sup *Supervisor, bridge *Bridge, req wire.DevCloneRequest) wire.DevCloneResult {
	started := time.Now()
	res := wire.DevCloneResult{Type: wire.TypeDevCloneResult, RequestID: req.RequestID}
	if sup != nil && sup.Log != nil {
		sup.Log.Info("dev clone start", "source", req.SourceServerID, "target", req.TargetServerID)
		defer func() {
			sup.Log.Info("dev clone finish", "target", req.TargetServerID, "ok", res.OK, "method", res.Method, "bytes", res.Bytes, "elapsed", time.Since(started), "err", res.Error)
		}()
	}
	if req.SourceServerID == "" || req.TargetServerID == "" || req.SourceServerID == req.TargetServerID {
		res.Error = "source and target server ids are required and must differ"
		return res
	}
	sourceDir, err := sup.serverRoot(req.SourceServerID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	targetDir, err := sup.serverRoot(req.TargetServerID)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if info, err := os.Stat(sourceDir); err != nil || !info.IsDir() {
		res.Error = "the source server directory does not exist"
		return res
	}
	if _, err := EnsureServerDir(targetDir); err != nil {
		res.Error = err.Error()
		return res
	}

	running := false
	if state, err := sup.Runtime.State(ctx, req.SourceServerID); err == nil && state == "running" {
		running = true
	}
	if running && bridge != nil {
		_ = bridge.Command(wire.BridgeCommandFrame{
			Type: wire.TypeBridgeCommand, ServerID: req.SourceServerID,
			ID: "dev-off-" + req.RequestID, Action: "command", Command: "save-off",
		})
		_ = bridge.Command(wire.BridgeCommandFrame{
			Type: wire.TypeBridgeCommand, ServerID: req.SourceServerID,
			ID: "dev-flush-" + req.RequestID, Action: "command", Command: "save-all flush",
		})
		time.Sleep(500 * time.Millisecond)
		defer func() {
			_ = bridge.Command(wire.BridgeCommandFrame{
				Type: wire.TypeBridgeCommand, ServerID: req.SourceServerID,
				ID: "dev-on-" + req.RequestID, Action: "command", Command: "save-on",
			})
		}()
	}

	method, bytes, err := cloneServerDir(sourceDir, targetDir)
	if err != nil {
		_ = os.RemoveAll(targetDir)
		res.Error = err.Error()
		return res
	}
	for _, name := range []string{"logs", "cache", "crash", "backups", "session.lock", ".delphi-restart.json", "plugins/.paper-remapped"} {
		_ = os.RemoveAll(filepath.Join(targetDir, filepath.FromSlash(name)))
	}
	if req.FreshWorld {
		for _, name := range []string{"world", "world_nether", "world_the_end"} {
			_ = os.RemoveAll(filepath.Join(targetDir, name))
		}
	}
	if err := rewriteDevProperties(targetDir, req.Port); err != nil {
		_ = os.RemoveAll(targetDir)
		res.Error = err.Error()
		return res
	}
	if err := writeWhitelist(targetDir, req.Whitelist); err != nil {
		_ = os.RemoveAll(targetDir)
		res.Error = err.Error()
		return res
	}
	res.OK = true
	res.Method = method
	res.Bytes = bytes
	return res
}

func cloneServerDir(source, target string) (string, int64, error) {
	if _, err := exec.LookPath("cp"); err == nil {
		out, err := exec.Command("cp", "-a", source+"/.", target).CombinedOutput()
		if err == nil {
			var size int64
			_ = filepath.Walk(target, func(_ string, info os.FileInfo, err error) error {
				if err == nil && info.Mode().IsRegular() {
					size += info.Size()
				}
				return nil
			})
			return "copy", size, nil
		}
		_ = out
	}
	tmpDir := filepath.Join(filepath.Dir(target), "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(tmpDir, "dev-clone-*.tar.gz")
	if err != nil {
		return "", 0, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	counter := &countingWriter{w: tmp}
	if err := writeTarGz(counter, source); err != nil {
		tmp.Close()
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	file, err := os.Open(tmpName)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	if err := extractTarGz(file, target); err != nil {
		return "", 0, err
	}
	return "tar", counter.n, nil
}

func rewriteDevProperties(dir string, port int) error {
	path := filepath.Join(dir, "server.properties")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return err
	}
	values := map[string]string{
		"server-port":       fmt.Sprintf("%d", port),
		"white-list":        "true",
		"enforce-whitelist": "true",
	}
	lines := make([]string, 0)
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			lines = append(lines, line)
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			lines = append(lines, line)
			continue
		}
		key = strings.TrimSpace(key)
		if value, ok := values[key]; ok {
			lines = append(lines, key+"="+value)
			seen[key] = true
		} else {
			lines = append(lines, line)
		}
	}
	for key, value := range values {
		if !seen[key] {
			lines = append(lines, key+"="+value)
		}
	}
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	return os.WriteFile(path, []byte(out), 0o644)
}

func writeWhitelist(dir string, entries []wire.WhitelistEntry) error {
	if entries == nil {
		entries = []wire.WhitelistEntry{}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "whitelist.json"), data, 0o644)
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
