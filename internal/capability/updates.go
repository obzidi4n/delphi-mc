package capability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"delphi.dev/delphi/internal/artifact"
	"delphi.dev/delphi/internal/plugins"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/template"
)

// ErrNoUpdates is returned when detection found nothing to apply.
var ErrNoUpdates = errors.New("no updates available")

// UpdateStore is persistence for managed plugins and the artifact registry.
type UpdateStore interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	SetArtifacts(ctx context.Context, id, mcVersion, jarURL, jarSHA string, plugins []store.ServerPlugin, desired string) (store.Server, error)
	SetPlugins(ctx context.Context, id string, plugins []store.ServerPlugin) (store.Server, error)
	RecordArtifact(ctx context.Context, a store.Artifact) error
	ListArtifacts(ctx context.Context) ([]store.Artifact, error)
	DeleteArtifact(ctx context.Context, sha string) error
	ArtifactRefs(ctx context.Context) (map[string]struct{}, error)
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// Updates installs plugins from templates and applies plugin updates with a
// boot check that rolls back on failure.
type Updates struct {
	Store     UpdateStore
	Resolver  *plugins.Resolver
	Artifacts *artifact.Store
	HTTP      *http.Client
	Notify    Notifier
	Log       *slog.Logger
	// BootTimeout and BootPoll tune the post-update health window.
	BootTimeout time.Duration
	BootPoll    time.Duration
}

// PluginUpdate is one available change.
type PluginUpdate struct {
	Project  string `json:"project"`
	Provider string `json:"provider"`
	Current  string `json:"current"`
	Latest   string `json:"latest"`
	Error    string `json:"error,omitempty"`
}

// ApplyResult reports what was changed.
type ApplyResult struct {
	Applied   []PluginUpdate `json:"applied"`
	Revision  int64          `json:"revision"`
	BootCheck string         `json:"boot_check"` // pending | skipped
}

// GCResult reports artifact garbage collection.
type GCResult struct {
	Removed   int   `json:"removed"`
	FreedByte int64 `json:"freed_bytes"`
}

const maxArtifactBytes = 100 << 20

// Install resolves and downloads every template plugin, returning the managed
// set to store on the server.
func (u *Updates) Install(ctx context.Context, mcVersion string, wanted []template.Plugin) ([]store.ServerPlugin, error) {
	out := make([]store.ServerPlugin, 0, len(wanted))
	names := map[string]bool{}
	for _, want := range wanted {
		res, err := u.resolve(ctx, want.Provider, want.Project, mcVersion, want.Version, want.Range)
		if err != nil {
			return nil, err
		}
		art, err := u.fetch(ctx, res)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", want.Project, err)
		}
		name := plugins.JarName(want.Project)
		if names[name] {
			return nil, invalid("duplicate plugin %q", want.Project)
		}
		names[name] = true
		policy := "latest"
		switch {
		case want.Version != "":
			policy = "pinned"
		case want.Range != "":
			policy = "range"
		}
		out = append(out, store.ServerPlugin{
			Project: want.Project, Provider: want.Provider, Version: res.Version,
			Policy: policy, Range: want.Range, Name: name,
			SHA256: art.SHA256, Size: art.Size,
		})
	}
	return out, nil
}

func (u *Updates) resolve(ctx context.Context, provider, project, mcVersion, pin, rng string) (plugins.Resolved, error) {
	if u.Resolver == nil {
		return plugins.Resolved{}, errors.New("plugin resolver is not configured")
	}
	return u.Resolver.Resolve(ctx, provider, project, mcVersion, pin, rng)
}

func (u *Updates) fetch(ctx context.Context, res plugins.Resolved) (store.Artifact, error) {
	if u.Artifacts == nil || u.Store == nil {
		return store.Artifact{}, errors.New("artifact store is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, res.URL, nil)
	if err != nil {
		return store.Artifact{}, err
	}
	req.Header.Set("User-Agent", "delphi/1.0 (minecraft control plane)")
	client := u.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return store.Artifact{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return store.Artifact{}, fmt.Errorf("download: %s", resp.Status)
	}
	sha, size, err := u.Artifacts.Put(resp.Body, res.SHA256, maxArtifactBytes)
	if err != nil {
		return store.Artifact{}, err
	}
	art := store.Artifact{
		SHA256: sha, Filename: res.Filename, Size: size,
		Kind: "plugin", Source: res.Provider + ":" + res.Project + "@" + res.Version,
	}
	if err := u.Store.RecordArtifact(ctx, art); err != nil {
		return store.Artifact{}, err
	}
	return art, nil
}

// Catalog searches Modrinth or Hangar for installable plugin projects.
func (u *Updates) Catalog(ctx context.Context, p Principal, provider, query, mcVersion string, limit int) ([]plugins.CatalogEntry, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, forbidden(orgScope(p, "read"))
	}
	if u.Resolver == nil {
		return nil, errors.New("plugin resolver is not configured")
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "modrinth" && provider != "hangar" {
		return nil, invalid("provider must be modrinth or hangar")
	}
	return u.Resolver.Search(ctx, provider, query, strings.TrimSpace(mcVersion), limit)
}

// PluginRequest asks to install or replace one managed plugin.
type PluginRequest struct {
	Provider string `json:"provider"`
	Project  string `json:"project"`
	Name     string `json:"name,omitempty"`
	Version  string `json:"version,omitempty"`
	Range    string `json:"range,omitempty"`
}

// PluginAddResult reports the installed plugin and resulting revision.
type PluginAddResult struct {
	Plugin   store.ServerPlugin `json:"plugin"`
	Revision int64              `json:"revision"`
}

// Add resolves, downloads, and stages one plugin on a server. Reinstalling
// the same project updates its resolved version instead of creating a duplicate.
// A running server is not restarted; the owner restarts when ready to load it.
func (u *Updates) Add(ctx context.Context, p Principal, id string, req PluginRequest) (PluginAddResult, error) {
	if !p.Allows(serverScope(id, "update")) {
		return PluginAddResult{}, forbidden(serverScope(id, "update"))
	}
	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	req.Project = strings.TrimSpace(req.Project)
	if req.Provider != "modrinth" && req.Provider != "hangar" && req.Provider != "github" && req.Provider != "jenkins" {
		return PluginAddResult{}, invalid("provider must be modrinth, hangar, github, or jenkins")
	}
	if req.Project == "" || len(req.Project) > 200 {
		return PluginAddResult{}, invalid("project must be 1-200 characters")
	}
	if req.Version != "" && req.Range != "" {
		return PluginAddResult{}, invalid("version and range cannot both be set")
	}
	project, err := plugins.NormalizeProject(req.Provider, req.Project)
	if err != nil {
		return PluginAddResult{}, invalid("%v", err)
	}
	req.Project = project
	srv, err := u.Store.GetServer(ctx, id)
	if err != nil {
		return PluginAddResult{}, err
	}
	if srv.MCVersion == "" {
		return PluginAddResult{}, invalid("server has no recorded Minecraft version")
	}
	res, err := u.resolve(ctx, req.Provider, req.Project, srv.MCVersion, req.Version, req.Range)
	if err != nil {
		return PluginAddResult{}, err
	}
	art, err := u.fetch(ctx, res)
	if err != nil {
		return PluginAddResult{}, fmt.Errorf("plugin %s: %w", req.Project, err)
	}
	policy := "latest"
	switch {
	case req.Version != "":
		policy = "pinned"
	case req.Range != "":
		policy = "range"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = req.Project
	}
	if len(name) > 100 {
		return PluginAddResult{}, invalid("plugin name must be 1-100 characters")
	}
	plugin := store.ServerPlugin{
		Project: name, Provider: req.Provider, Version: res.Version,
		Policy: policy, Range: req.Range, Name: plugins.JarName(name),
		Source: req.Project, SHA256: art.SHA256, Size: art.Size,
	}
	next := append([]store.ServerPlugin(nil), srv.Plugins...)
	replaced := false
	for i, installed := range next {
		if strings.EqualFold(installed.Project, name) {
			next[i] = plugin
			replaced = true
			break
		}
	}
	if !replaced {
		next = append(next, plugin)
	}
	updated, err := u.Store.SetPlugins(ctx, srv.ID, next)
	if err != nil {
		return PluginAddResult{}, err
	}
	u.sync(ctx, srv.NodeID)
	action := "server.plugin.add"
	if replaced {
		action = "server.plugin.update"
	}
	_ = u.audit(ctx, srv, p, action, map[string]any{
		"project": plugin.Project, "provider": plugin.Provider,
		"version": plugin.Version, "policy": plugin.Policy, "revision": updated.DesiredRevision,
	})
	return PluginAddResult{Plugin: plugin, Revision: updated.DesiredRevision}, nil
}

// Upload installs a manually supplied jar, such as a SpigotMC resource that
// has no public API. Uploaded plugins are pinned to the supplied version and
// excluded from remote update detection.
func (u *Updates) Upload(ctx context.Context, p Principal, id, project, version string, r io.Reader) (PluginAddResult, error) {
	if !p.Allows(serverScope(id, "update")) {
		return PluginAddResult{}, forbidden(serverScope(id, "update"))
	}
	project = strings.TrimSpace(project)
	version = strings.TrimSpace(version)
	if project == "" || len(project) > 200 {
		return PluginAddResult{}, invalid("project must be 1-200 characters")
	}
	if version == "" {
		version = "uploaded"
	}
	if len(version) > 100 {
		return PluginAddResult{}, invalid("version must be 1-100 characters")
	}
	if u.Artifacts == nil || u.Store == nil {
		return PluginAddResult{}, errors.New("artifact store is not configured")
	}
	srv, err := u.Store.GetServer(ctx, id)
	if err != nil {
		return PluginAddResult{}, err
	}
	sha, size, err := u.Artifacts.Put(r, "", maxArtifactBytes)
	if err != nil {
		return PluginAddResult{}, invalid("plugin upload: %v", err)
	}
	// A plugin jar is a zip; reject an accidentally uploaded text/config file.
	path, err := u.Artifacts.Path(sha)
	if err != nil {
		return PluginAddResult{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return PluginAddResult{}, err
	}
	head := make([]byte, 4)
	_, readErr := io.ReadFull(file, head)
	_ = file.Close()
	if readErr != nil || string(head) != "PK\x03\x04" {
		return PluginAddResult{}, invalid("uploaded file is not a jar")
	}
	art := store.Artifact{
		SHA256: sha, Filename: plugins.JarName(project), Size: size,
		Kind: "plugin", Source: "spigot:" + project + "@" + version,
	}
	if err := u.Store.RecordArtifact(ctx, art); err != nil {
		return PluginAddResult{}, err
	}
	plugin := store.ServerPlugin{
		Project: project, Provider: "spigot", Version: version,
		Policy: "manual", Name: plugins.JarName(project), SHA256: sha, Size: size,
	}
	next := append([]store.ServerPlugin(nil), srv.Plugins...)
	replaced := false
	for i, installed := range next {
		if strings.EqualFold(installed.Project, project) {
			next[i] = plugin
			replaced = true
			break
		}
	}
	if !replaced {
		next = append(next, plugin)
	}
	updated, err := u.Store.SetPlugins(ctx, srv.ID, next)
	if err != nil {
		return PluginAddResult{}, err
	}
	u.sync(ctx, srv.NodeID)
	action := "server.plugin.upload"
	if replaced {
		action = "server.plugin.upload_replace"
	}
	_ = u.audit(ctx, srv, p, action, map[string]any{
		"project": plugin.Project, "version": plugin.Version,
		"sha256": plugin.SHA256, "revision": updated.DesiredRevision,
	})
	return PluginAddResult{Plugin: plugin, Revision: updated.DesiredRevision}, nil
}

// PromotePlugins replaces the managed plugin set on a production server with
// the dev server's set. It stages files and does not restart production.
func (u *Updates) PromotePlugins(ctx context.Context, p Principal, targetID string, plugins []store.ServerPlugin) (store.Server, error) {
	if !p.Allows(serverScope(targetID, "update")) {
		return store.Server{}, forbidden(serverScope(targetID, "update"))
	}
	srv, err := u.Store.GetServer(ctx, targetID)
	if err != nil {
		return store.Server{}, err
	}
	updated, err := u.Store.SetPlugins(ctx, srv.ID, plugins)
	if err != nil {
		return store.Server{}, err
	}
	u.sync(ctx, srv.NodeID)
	_ = u.audit(ctx, srv, p, "server.plugin.promote", map[string]any{
		"plugins": len(plugins), "revision": updated.DesiredRevision,
	})
	return updated, nil
}

// Remove deletes one managed plugin and syncs the server. A running server is
// not restarted; the owner restarts when ready to unload it.
func (u *Updates) Remove(ctx context.Context, p Principal, id, project string) (int64, error) {
	if !p.Allows(serverScope(id, "update")) {
		return 0, forbidden(serverScope(id, "update"))
	}
	project = strings.TrimSpace(project)
	if project == "" || len(project) > 200 {
		return 0, invalid("project must be 1-200 characters")
	}
	srv, err := u.Store.GetServer(ctx, id)
	if err != nil {
		return 0, err
	}
	next := make([]store.ServerPlugin, 0, len(srv.Plugins))
	removed := false
	for _, installed := range srv.Plugins {
		if strings.EqualFold(installed.Project, project) {
			removed = true
			continue
		}
		next = append(next, installed)
	}
	if !removed {
		return 0, invalid("plugin %q is not installed", project)
	}
	updated, err := u.Store.SetPlugins(ctx, srv.ID, next)
	if err != nil {
		return 0, err
	}
	u.sync(ctx, srv.NodeID)
	_ = u.audit(ctx, srv, p, "server.plugin.remove", map[string]any{
		"project": project, "revision": updated.DesiredRevision,
	})
	return updated.DesiredRevision, nil
}

// List returns a server's managed plugins.
func (u *Updates) List(ctx context.Context, p Principal, id string) ([]store.ServerPlugin, error) {
	if !p.Allows(serverScope(id, "read")) {
		return nil, forbidden(serverScope(id, "read"))
	}
	srv, err := u.Store.GetServer(ctx, id)
	if err != nil {
		return nil, err
	}
	return srv.Plugins, nil
}

// Detect compares installed plugins with the newest compatible versions.
func (u *Updates) Detect(ctx context.Context, p Principal, id string) ([]PluginUpdate, int64, error) {
	if !p.Allows(serverScope(id, "read")) {
		return nil, 0, forbidden(serverScope(id, "read"))
	}
	srv, err := u.Store.GetServer(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	out := make([]PluginUpdate, 0, len(srv.Plugins))
	for _, installed := range srv.Plugins {
		if strings.EqualFold(installed.Provider, "spigot") || strings.EqualFold(installed.Policy, "manual") {
			continue
		}
		source := installed.Source
		if source == "" {
			source = installed.Project
		}
		project, normalizeErr := plugins.NormalizeProject(installed.Provider, source)
		if normalizeErr != nil {
			out = append(out, PluginUpdate{
				Project: installed.Project, Provider: installed.Provider,
				Current: installed.Version, Error: normalizeErr.Error(),
			})
			continue
		}
		res, err := u.resolve(ctx, installed.Provider, project, srv.MCVersion, "", installed.Range)
		if err != nil {
			out = append(out, PluginUpdate{
				Project: installed.Project, Provider: installed.Provider,
				Current: installed.Version, Error: err.Error(),
			})
			continue
		}
		if res.Version == installed.Version && (res.SHA256 == "" || res.SHA256 == installed.SHA256) {
			continue
		}
		out = append(out, PluginUpdate{
			Project: installed.Project, Provider: installed.Provider,
			Current: installed.Version, Latest: res.Version,
		})
	}
	return out, srv.DesiredRevision, nil
}

// Apply resolves and installs the newest plugin versions, restarts a running
// server, and rolls back if the restart does not reach running.
func (u *Updates) Apply(ctx context.Context, p Principal, id string) (ApplyResult, error) {
	if !p.Allows(serverScope(id, "update")) {
		return ApplyResult{}, forbidden(serverScope(id, "update"))
	}
	srv, err := u.Store.GetServer(ctx, id)
	if err != nil {
		return ApplyResult{}, err
	}
	if srv.MCVersion == "" {
		return ApplyResult{}, invalid("server has no recorded Minecraft version")
	}

	updates, _, err := u.Detect(ctx, p, id)
	if err != nil {
		return ApplyResult{}, err
	}
	applied := make([]PluginUpdate, 0, len(updates))
	next := append([]store.ServerPlugin(nil), srv.Plugins...)
	for _, update := range updates {
		if update.Error != "" || update.Latest == "" {
			continue
		}
		installed, idx := findPlugin(next, update.Project)
		if idx < 0 {
			continue
		}
		source := installed.Source
		if source == "" {
			source = installed.Project
		}
		res, err := u.resolve(ctx, installed.Provider, source, srv.MCVersion, "", installed.Range)
		if err != nil {
			return ApplyResult{}, err
		}
		art, err := u.fetch(ctx, res)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("plugin %s: %w", installed.Project, err)
		}
		next[idx].Version = res.Version
		next[idx].SHA256 = art.SHA256
		next[idx].Size = art.Size
		applied = append(applied, update)
	}
	if len(applied) == 0 {
		return ApplyResult{}, ErrNoUpdates
	}

	desired := srv.DesiredState
	updated, err := u.Store.SetArtifacts(ctx, srv.ID, srv.MCVersion, srv.JarURL, srv.JarSHA256, next, desired)
	if err != nil {
		return ApplyResult{}, err
	}
	u.sync(ctx, srv.NodeID)
	_ = u.audit(ctx, srv, p, "server.update", map[string]any{"updates": applied, "revision": updated.DesiredRevision})

	result := ApplyResult{Applied: applied, Revision: updated.DesiredRevision, BootCheck: "skipped"}
	if desired == "running" {
		result.BootCheck = "pending"
		go u.bootCheck(srv, p, updated.DesiredRevision)
	}
	return result, nil
}

func findPlugin(list []store.ServerPlugin, project string) (store.ServerPlugin, int) {
	for i, p := range list {
		if strings.EqualFold(p.Project, project) {
			return p, i
		}
	}
	return store.ServerPlugin{}, -1
}

// bootCheck watches the new revision; a failure restores the previous jar and
// plugin set and restarts that.
func (u *Updates) bootCheck(old store.Server, p Principal, revision int64) {
	timeout := u.BootTimeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	poll := u.BootPoll
	if poll <= 0 {
		poll = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	outcome := "timeout"
	failures := 0
	for {
		select {
		case <-ctx.Done():
			rctx, cancelRollback := context.WithTimeout(context.Background(), 15*time.Second)
			u.rollback(rctx, old, p, outcome)
			cancelRollback()
			return
		case <-ticker.C:
		}
		srv, err := u.Store.GetServer(ctx, old.ID)
		if err != nil {
			continue
		}
		if srv.AppliedRevision < revision {
			continue
		}
		switch srv.ObservedState {
		case "running":
			_ = u.audit(ctx, srv, p, "server.update.verified", map[string]any{"revision": revision})
			return
		case "exited", "error":
			// A restart can briefly observe the old container exiting; only a
			// failure that persists across three polls is a boot failure.
			failures++
			if failures < 3 {
				continue
			}
			outcome = "boot failed: " + srv.LastError
			rctx, cancelRollback := context.WithTimeout(context.Background(), 15*time.Second)
			u.rollback(rctx, old, p, outcome)
			cancelRollback()
			return
		default:
			failures = 0
		}
	}
}

func (u *Updates) rollback(ctx context.Context, old store.Server, p Principal, reason string) {
	desired := "running"
	if old.DesiredState != "running" {
		desired = old.DesiredState
	}
	restored, err := u.Store.SetArtifacts(ctx, old.ID, old.MCVersion, old.JarURL, old.JarSHA256, old.Plugins, desired)
	if err != nil {
		u.log().Error("update rollback failed", "server", old.ID, "err", err)
		return
	}
	u.sync(ctx, old.NodeID)
	_ = u.audit(ctx, restored, p, "server.update.rollback", map[string]any{"reason": reason, "revision": restored.DesiredRevision})
}

func (u *Updates) audit(ctx context.Context, srv store.Server, p Principal, action string, diff any) error {
	return u.Store.Audit(ctx, srv.OrgID, p.UserID, p.Via, action, srv.ID, diff)
}

func (u *Updates) sync(ctx context.Context, nodeID string) {
	if u.Notify != nil {
		_ = u.Notify.Sync(ctx, nodeID)
	}
}

func (u *Updates) log() *slog.Logger {
	if u.Log != nil {
		return u.Log
	}
	return slog.Default()
}

// GC removes unreferenced artifacts older than minAge.
func (u *Updates) GC(ctx context.Context, p Principal, minAge time.Duration) (GCResult, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return GCResult{}, forbidden(orgScope(p, "admin"))
	}
	items, err := u.Store.ListArtifacts(ctx)
	if err != nil {
		return GCResult{}, err
	}
	refs, err := u.Store.ArtifactRefs(ctx)
	if err != nil {
		return GCResult{}, err
	}
	cutoff := time.Now().Add(-minAge)
	var result GCResult
	for _, art := range items {
		if _, used := refs[art.SHA256]; used || art.CreatedAt.After(cutoff) {
			continue
		}
		if u.Artifacts != nil {
			if err := u.Artifacts.Remove(art.SHA256); err != nil {
				continue
			}
		}
		if err := u.Store.DeleteArtifact(ctx, art.SHA256); err != nil {
			continue
		}
		result.Removed++
		result.FreedByte += art.Size
	}
	// A crash between writing a blob and recording it leaves an orphan with no
	// row; collect those too.
	if u.Artifacts != nil {
		if shas, err := u.Artifacts.List(); err == nil {
			for _, sha := range shas {
				if _, known := refs[sha]; known {
					continue
				}
				recorded := false
				for _, art := range items {
					if art.SHA256 == sha {
						recorded = true
						break
					}
				}
				if recorded {
					continue
				}
				path, err := u.Artifacts.Path(sha)
				if err != nil {
					continue
				}
				info, err := os.Stat(path)
				if err != nil || info.ModTime().After(cutoff) {
					continue
				}
				if err := u.Artifacts.Remove(sha); err == nil {
					result.Removed++
					result.FreedByte += info.Size()
				}
			}
		}
	}
	_ = u.Store.Audit(ctx, p.OrgID, p.UserID, p.Via, "artifact.gc", "artifacts", map[string]any{
		"removed": result.Removed, "freed_bytes": result.FreedByte,
	})
	return result, nil
}
