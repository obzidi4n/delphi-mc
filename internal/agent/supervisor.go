package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"delphi.dev/delphi/internal/wire"
)

// Supervisor turns desired state into files and container actions. It is
// idempotent: applying the same revision twice does nothing.
type Supervisor struct {
	DataDir string
	Runtime Runtime
	HTTP    *http.Client
	Log     *slog.Logger
	// BlobBase is the panel base URL used to fetch managed plugin artifacts.
	BlobBase string
	// BlobHTTP carries the node certificate for panel blob requests; HTTP
	// stays a normal client for upstream jar downloads.
	BlobHTTP *http.Client
	// RestartDelay is the first crash-restart delay (default 2s).
	RestartDelay time.Duration
	mu           sync.Mutex
}

// NewSupervisor builds a supervisor with a long download timeout.
func NewSupervisor(dataDir string, rt Runtime, log *slog.Logger) *Supervisor {
	// Container mounts require an absolute source path.
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	return &Supervisor{
		DataDir: dataDir,
		Runtime: rt,
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
		Log:     log,
	}
}

type appliedState struct {
	AppliedRevision int64  `json:"applied_revision"`
	State           string `json:"state"`
}

// Apply converges one server and returns what the node observed.
func (s *Supervisor) Apply(ctx context.Context, spec wire.ServerSpec) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.DataDir, "servers", spec.ID)
	if _, err := EnsureServerDir(dir); err != nil {
		return "error", err.Error()
	}

	if spec.Deleted {
		if state, err := s.Runtime.State(ctx, spec.ID); err == nil && state != "missing" {
			if state == "running" || state == "paused" {
				_ = s.Runtime.Stop(ctx, spec.ID)
			}
			_ = s.Runtime.Remove(ctx, spec.ID)
		}
		s.recordApplied(dir, spec.Revision, "deleted")
		return "deleted", ""
	}

	applied := s.appliedRevision(ctx, spec.ID, dir)

	if spec.DesiredState == "running" {
		if err := s.prepare(ctx, spec, dir); err != nil {
			return "error", err.Error()
		}
		state, err := s.Runtime.State(ctx, spec.ID)
		if err != nil {
			return "error", err.Error()
		}
		switch {
		case state == "missing":
			if err := s.Runtime.Ensure(ctx, spec, dir); err != nil {
				return "error", err.Error()
			}
		case state == "running" && spec.Revision > applied:
			if err := s.Runtime.Restart(ctx, spec.ID); err != nil {
				return "error", err.Error()
			}
		case state != "running" && state != "paused":
			if err := s.Runtime.Start(ctx, spec.ID); err != nil {
				return "error", err.Error()
			}
		}
	} else if state, err := s.Runtime.State(ctx, spec.ID); err == nil && state == "running" {
		if spec.KillRequested {
			if err := s.Runtime.Kill(ctx, spec.ID); err != nil {
				return "error", err.Error()
			}
		} else if err := s.Runtime.Stop(ctx, spec.ID); err != nil {
			return "error", err.Error()
		}
	}

	state, err := s.Runtime.State(ctx, spec.ID)
	if err != nil {
		return "error", err.Error()
	}
	if state == "exited" && spec.DesiredState == "running" {
		s.captureCrash(ctx, spec, dir)
	}

	if state == "running" {
		s.saveRestartState(dir, restartState{})
	}
	if spec.DesiredState == "stopped" {
		switch state {
		case "running", "stopping":
			// Still shutting down: report it honestly.
		default:
			state = "stopped"
		}
	}
	switch state {
	case "missing", "paused":
		state = "unknown"
	}

	s.recordApplied(dir, spec.Revision, state)
	return state, ""
}

// Observe reports the current runtime state without changing it. It is what
// keeps the panel honest between mutations, and it captures crash artifacts
// when a container has exited.
func (s *Supervisor) Observe(ctx context.Context, spec wire.ServerSpec) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.Runtime.State(ctx, spec.ID)
	if err != nil {
		return "error", err.Error()
	}

	dir := filepath.Join(s.DataDir, "servers", spec.ID)
	if (state == "exited" || state == "error") && spec.DesiredState == "running" {
		s.captureCrash(ctx, spec, dir)
		if spec.RestartPolicy != "never" {
			if restarted, restartState := s.restartCrashed(ctx, spec, dir); restarted {
				return restartState, "restarted after the container exited"
			}
		}
	}
	if spec.DesiredState == "stopped" && (state == "exited" || state == "created" || state == "missing") {
		return "stopped", ""
	}

	switch state {
	case "stopping":
		return "stopping", ""
	case "missing", "paused":
		return "unknown", ""
	}
	return state, ""
}

// appliedRevision combines the local state file with the container label, so
// an agent restart neither restarts running servers nor re-downloads jars.
type restartState struct {
	Attempts int       `json:"attempts"`
	NextAt   time.Time `json:"next_at"`
}

func restartStatePath(dir string) string { return filepath.Join(dir, ".delphi-restart.json") }

// restartCrashed restarts a container after an unexpected exit. The first
// observation reports the crash; the next observation (after a short delay)
// starts it again. The panel-down path relies on this.
func (s *Supervisor) restartCrashed(ctx context.Context, spec wire.ServerSpec, dir string) (bool, string) {
	var state restartState
	if data, err := os.ReadFile(restartStatePath(dir)); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	delay := s.RestartDelay
	if delay <= 0 {
		delay = 2 * time.Second
	}
	now := time.Now()
	if state.NextAt.IsZero() {
		state.NextAt = now.Add(delay)
		s.saveRestartState(dir, state)
		return false, ""
	}
	if now.Before(state.NextAt) {
		return false, ""
	}
	state.Attempts++
	backoff := time.Duration(1<<min(state.Attempts, 5)) * 2 * time.Second
	if backoff > 60*time.Second {
		backoff = 60 * time.Second
	}
	state.NextAt = now.Add(backoff)
	s.saveRestartState(dir, state)
	if err := s.Runtime.Start(ctx, spec.ID); err != nil {
		s.Log.Warn("agent restart failed", "server", spec.ID, "err", err)
		return false, ""
	}
	state = restartState{}
	s.saveRestartState(dir, state)
	stateNow, err := s.Runtime.State(ctx, spec.ID)
	if err != nil {
		return true, "starting"
	}
	return true, stateNow
}

func (s *Supervisor) saveRestartState(dir string, state restartState) {
	data, _ := json.Marshal(state)
	_ = os.WriteFile(restartStatePath(dir), data, 0o644)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Supervisor) appliedRevision(ctx context.Context, id, dir string) int64 {
	var applied int64
	var local appliedState
	if data, err := os.ReadFile(stateFilePath(dir)); err == nil {
		if json.Unmarshal(data, &local) == nil {
			applied = local.AppliedRevision
		}
	}
	if fromLabel, err := s.Runtime.AppliedRevision(ctx, id); err == nil && fromLabel > applied {
		applied = fromLabel
	}
	return applied
}

func (s *Supervisor) recordApplied(dir string, revision int64, state string) {
	data, _ := json.Marshal(appliedState{AppliedRevision: revision, State: state})
	_ = os.WriteFile(stateFilePath(dir), data, 0o644)
}

func stateFilePath(dir string) string { return filepath.Join(dir, ".delphi-state.json") }

// prepare writes rendered template files, the Paper boot files when the base
// needs them, and the pinned jar. Existing files are never overwritten: a
// template applies on first boot, later edits are the operator's.
func (s *Supervisor) prepare(ctx context.Context, spec wire.ServerSpec, dir string) error {
	if err := writeTemplateFiles(dir, spec.Files); err != nil {
		return err
	}
	if spec.Base == "velocity" || spec.Base == "folia" {
		// The bridge is Paper-only: remove a stale copy for proxies
		// (Velocity) and for regionized Folia, where it is not supported.
		_ = os.Remove(filepath.Join(dir, "plugins", "DelphiBridge.jar"))
	} else if err := InstallBridge(dir); err != nil {
		return err
	}
	if spec.Base != "velocity" {
		if !spec.AcceptEULA {
			return errors.New("the Minecraft EULA was not accepted for this server")
		}
		if err := ensureFile(filepath.Join(dir, "eula.txt"), "eula=true\n"); err != nil {
			return err
		}
		properties := fmt.Sprintf(
			"server-port=%d\nserver-ip=\nonline-mode=true\nenable-rcon=false\nmotd=%s\nlevel-name=world\n",
			spec.Port, oneLine(spec.Name),
		)
		if err := ensureFile(filepath.Join(dir, "server.properties"), properties); err != nil {
			return err
		}
	}
	if err := s.ensureJar(ctx, spec, dir); err != nil {
		return err
	}
	return s.ensurePlugins(ctx, spec, dir)
}

// writeTemplateFiles creates parent directories and writes each file only if
// it does not exist yet.
func writeTemplateFiles(dir string, files map[string]string) error {
	for path, content := range files {
		if !safeRelPath(path) {
			return fmt.Errorf("template file %q is not a safe relative path", path)
		}
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(full), err)
		}
		if err := ensureFile(full, content); err != nil {
			return err
		}
	}
	return nil
}

func safeRelPath(path string) bool {
	if path == "" || len(path) > 255 || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func ensureFile(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (s *Supervisor) ensureJar(ctx context.Context, spec wire.ServerSpec, dir string) error {
	target := filepath.Join(dir, "paper.jar")
	if sum, err := fileSHA256(target); err == nil && sum == spec.JarSHA256 {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.JarURL, nil)
	if err != nil {
		return fmt.Errorf("jar request: %w", err)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("download jar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download jar: %s", resp.Status)
	}

	tmp := target + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hasher), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("download jar: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, closeErr)
	}

	if got := hex.EncodeToString(hasher.Sum(nil)); got != spec.JarSHA256 {
		_ = os.Remove(tmp)
		return fmt.Errorf("jar sha256 mismatch: got %s, want %s", got, spec.JarSHA256)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("install jar: %w", err)
	}
	s.Log.Info("jar installed", "server", spec.ID, "sha256", spec.JarSHA256)
	return nil
}

// ensurePlugins downloads managed plugin jars from the panel's artifact store
// into plugins/<name>, replacing a jar whose hash changed.
func (s *Supervisor) ensurePlugins(ctx context.Context, spec wire.ServerSpec, dir string) error {
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return fmt.Errorf("create plugins dir: %w", err)
	}
	if len(spec.Plugins) > 0 && s.BlobBase == "" {
		return errors.New("plugin downloads need a panel URL")
	}
	managed, err := readManagedPlugins(pluginDir)
	if err != nil {
		return fmt.Errorf("read plugin manifest: %w", err)
	}
	if len(managed) == 0 {
		for _, name := range spec.PreviousPlugins {
			managed = append(managed, managedPlugin{Name: name})
		}
	}
	current := make([]managedPlugin, 0, len(spec.Plugins))
	seen := make(map[string]bool, len(spec.Plugins))
	for _, p := range spec.Plugins {
		if !safePluginName(p.Name) || len(p.SHA256) != 64 {
			return fmt.Errorf("plugin %q has an invalid name or hash", p.Name)
		}
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		current = append(current, managedPlugin{Name: p.Name, ConfigDir: p.ConfigDir})
		active := filepath.Join(pluginDir, p.Name)
		disabled := active + ".d"
		target, other := active, disabled
		if p.Disabled {
			target, other = disabled, active
		}
		if !fileMatches(target, p.SHA256) {
			if fileMatches(other, p.SHA256) {
				if err := os.Rename(other, target); err != nil {
					return fmt.Errorf("toggle plugin %s: %w", p.Name, err)
				}
			} else if err := s.installPlugin(ctx, p, target); err != nil {
				return err
			}
		}
		if err := os.Remove(other); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove alternate plugin %s: %w", p.Name, err)
		}
		s.Log.Info("plugin staged", "server", spec.ID, "name", p.Name, "disabled", p.Disabled)
	}
	if err := writeManagedPlugins(pluginDir, current); err != nil {
		return fmt.Errorf("write plugin manifest: %w", err)
	}
	for _, old := range managed {
		if seen[old.Name] || !safePluginName(old.Name) {
			continue
		}
		for _, name := range []string{old.Name, old.Name + ".d"} {
			if err := os.Remove(filepath.Join(pluginDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove old plugin %s: %w", name, err)
			}
		}
		configName := old.ConfigDir
		if configName == "" {
			configName = strings.TrimSuffix(old.Name, ".jar")
		}
		if configName != "" {
			configDir, err := safePath(pluginDir, configName)
			if err != nil {
				return fmt.Errorf("remove configs for %s: %w", old.Name, err)
			}
			if err := os.RemoveAll(configDir); err != nil {
				return fmt.Errorf("remove configs for %s: %w", old.Name, err)
			}
			s.Log.Info("plugin configs removed", "server", spec.ID, "plugin", old.Name, "dir", configName)
		}
		s.Log.Info("plugin removed", "server", spec.ID, "name", old.Name)
	}
	return nil
}

func safePluginName(name string) bool {
	return name != "" && len(name) <= 128 && strings.HasSuffix(name, ".jar") &&
		!strings.ContainsAny(name, "/\\") && !strings.HasPrefix(name, ".")
}

func (s *Supervisor) installPlugin(ctx context.Context, p wire.PluginSpec, target string) error {
	url := strings.TrimRight(s.BlobBase, "/") + "/agent/blobs/" + p.SHA256
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("plugin request: %w", err)
	}
	client := s.BlobHTTP
	if client == nil {
		client = s.HTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download plugin %s: %w", p.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download plugin %s: %s", p.Name, resp.Status)
	}
	tmp := target + ".tmp"
	file, createErr := os.Create(tmp)
	if createErr != nil {
		return fmt.Errorf("create %s: %w", tmp, createErr)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hasher), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("download plugin %s: %w", p.Name, copyErr)
	}
	if closeErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, closeErr)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != p.SHA256 {
		os.Remove(tmp)
		return fmt.Errorf("plugin %s sha256 mismatch: got %s, want %s", p.Name, got, p.SHA256)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("install plugin %s: %w", p.Name, err)
	}
	return nil
}

func fileMatches(path, want string) bool {
	sum, err := fileSHA256(path)
	return err == nil && sum == want
}

const managedPluginsFile = ".delphi-managed.json"

type managedPlugin struct {
	Name      string `json:"name"`
	ConfigDir string `json:"config_dir,omitempty"`
}

func readManagedPlugins(dir string) ([]managedPlugin, error) {
	data, err := os.ReadFile(filepath.Join(dir, managedPluginsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var current []managedPlugin
	if err := json.Unmarshal(data, &current); err == nil {
		return current, nil
	}
	var legacy []string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}
	out := make([]managedPlugin, 0, len(legacy))
	for _, name := range legacy {
		out = append(out, managedPlugin{Name: name})
	}
	return out, nil
}

func writeManagedPlugins(dir string, plugins []managedPlugin) error {
	data, err := json.Marshal(plugins)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, managedPluginsFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, managedPluginsFile))
}

func (s *Supervisor) captureCrash(ctx context.Context, spec wire.ServerSpec, dir string) {
	crashDir := filepath.Join(dir, "crash", fmt.Sprintf("rev-%d", spec.Revision))
	if _, err := os.Stat(crashDir); err == nil {
		return
	}
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		return
	}

	if logs, err := s.Runtime.Logs(ctx, spec.ID, 300); err == nil {
		_ = os.WriteFile(filepath.Join(crashDir, "container.log"), []byte(logs), 0o644)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "hs_err_pid*.log"))
	for _, match := range matches {
		if data, err := os.ReadFile(match); err == nil {
			_ = os.WriteFile(filepath.Join(crashDir, filepath.Base(match)), data, 0o644)
		}
	}
	s.Log.Warn("captured crash artifacts", "server", spec.ID, "dir", crashDir)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}
