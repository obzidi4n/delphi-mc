package capability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"

	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// DevStore is the persistence the dev-server capability reads.
type DevStore interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	ListDevServers(ctx context.Context, sourceID string) ([]store.Server, error)
	ExpiredDevServers(ctx context.Context, now time.Time) ([]store.Server, error)
}

// DevFiles is the node file surface used to compare and promote settings.
type DevFiles interface {
	List(ctx context.Context, srv store.Server, path string) ([]wire.FileEntry, error)
	Read(ctx context.Context, srv store.Server, path string) (string, string, int64, error)
	Write(ctx context.Context, p Principal, srv store.Server, path, content, expected, auditAction string) (string, int, error)
}

// PromotionPluginChange is one plugin difference between a dev clone and
// production.
type PromotionPluginChange struct {
	Project  string `json:"project"`
	Provider string `json:"provider"`
	Current  string `json:"current,omitempty"`
	Next     string `json:"next,omitempty"`
	Change   string `json:"change"` // added | removed | changed | activated | deactivated
}

// PromotionFileChange is one settings/config difference.
type PromotionFileChange struct {
	Path          string `json:"path"`
	Status        string `json:"status"` // added | changed
	ProductionSHA string `json:"production_sha,omitempty"`
	DevSHA        string `json:"dev_sha,omitempty"`
	Size          int64  `json:"size,omitempty"`
	Diff          string `json:"diff,omitempty"`
}

// PromotionPreview is a safe review of dev changes before promotion.
type PromotionPreview struct {
	DevID      string                  `json:"dev_id"`
	TargetID   string                  `json:"target_id"`
	TargetName string                  `json:"target_name"`
	Plugins    []PromotionPluginChange `json:"plugins"`
	Files      []PromotionFileChange   `json:"files"`
}

// PromotionResult reports the staged production changes.
type PromotionResult struct {
	Plugins int  `json:"plugins"`
	Files   int  `json:"files"`
	Pending bool `json:"pending"`
}

// Dev creates temporary same-node clones for testing settings and plugins.
type Dev struct {
	Store      DevStore
	Servers    *Servers
	Updates    *Updates
	Files      DevFiles
	Hub        NodeCaller
	HTTP       *http.Client
	MojangBase string
	Log        *slog.Logger
}

// DevCreateResult is the created clone.
type DevCreateResult struct {
	Server store.Server
	Owner  string
	UUID   string
}

// Create clones a server's current on-disk state into a minimal, whitelisted
// dev server. The production server remains running.
func (d *Dev) Create(ctx context.Context, p Principal, sourceID, ownerName string, freshWorld bool) (DevCreateResult, error) {
	ownerName = strings.TrimSpace(ownerName)
	if ownerName == "" || len(ownerName) > 32 || strings.ContainsAny(ownerName, " /\\") {
		return DevCreateResult{}, invalid("a Minecraft username is required")
	}
	if !p.Allows(serverScope(sourceID, "update")) {
		return DevCreateResult{}, forbidden(serverScope(sourceID, "update"))
	}
	if d.Store == nil || d.Servers == nil || d.Hub == nil {
		return DevCreateResult{}, errors.New("dev environments are not configured")
	}
	source, err := d.Store.GetServer(ctx, sourceID)
	if err != nil {
		return DevCreateResult{}, err
	}
	if source.OrgID != p.OrgID {
		return DevCreateResult{}, forbidden(serverScope(sourceID, "update"))
	}
	if source.DevOf != "" {
		return DevCreateResult{}, invalid("cannot create a dev clone of another dev clone")
	}
	if source.Base == "velocity" {
		return DevCreateResult{}, invalid("dev clones are for game servers, not proxies")
	}
	uuid, err := d.lookupUUID(ctx, ownerName)
	if err != nil {
		return DevCreateResult{}, err
	}
	memory := 1024
	if source.MemoryMB > 0 && source.MemoryMB < memory {
		memory = source.MemoryMB
	}
	name := source.Name + " dev"
	if len(name) > 64 {
		name = name[:64]
	}
	ttl := 24 * time.Hour
	expires := time.Now().Add(ttl)
	target, err := d.Servers.Create(ctx, p, CreateRequest{
		Name: name, NodeID: source.NodeID, Image: source.Image,
		MemoryMB: memory, CPUCores: 1, Port: 0,
		JarURL: source.JarURL, JarSHA256: source.JarSHA256,
		JavaVersion: source.JavaVersion,
		AcceptEULA:  source.AcceptEULA, Base: source.Base,
		DesiredState: "stopped", MCVersion: source.MCVersion,
		Plugins: append([]store.ServerPlugin(nil), source.Plugins...),
		DevOf:   source.ID, DevWhitelist: []string{ownerName}, DevExpiresAt: &expires,
	})
	if err != nil {
		return DevCreateResult{}, err
	}
	cleanup := func() {
		_, _ = d.Servers.Delete(ctx, p, target.ID)
	}
	raw, err := d.Hub.Call(ctx, source.NodeID, func(requestID string) any {
		return wire.DevCloneRequest{
			Type: wire.TypeDevClone, RequestID: requestID,
			SourceServerID: source.ID, TargetServerID: target.ID, Port: target.Port,
			Whitelist:  []wire.WhitelistEntry{{Name: ownerName, UUID: uuid}},
			FreshWorld: freshWorld,
		}
	}, 30*time.Minute)
	if err != nil {
		cleanup()
		return DevCreateResult{}, fmt.Errorf("clone: %w", err)
	}
	var result wire.DevCloneResult
	if err := json.Unmarshal(raw, &result); err != nil {
		cleanup()
		return DevCreateResult{}, err
	}
	if !result.OK {
		cleanup()
		return DevCreateResult{}, fmt.Errorf("clone failed: %s", result.Error)
	}
	started, err := d.Servers.Power(ctx, p, target.ID, "start")
	if err != nil {
		cleanup()
		return DevCreateResult{}, err
	}
	target = started
	d.log().Info("dev server created", "source", source.ID, "target", target.ID, "bytes", result.Bytes, "method", result.Method)
	return DevCreateResult{Server: target, Owner: ownerName, UUID: uuid}, nil
}

// List returns active dev clones for a production server.
func (d *Dev) List(ctx context.Context, p Principal, sourceID string) ([]store.Server, error) {
	if !p.Allows(serverScope(sourceID, "read")) {
		return nil, forbidden(serverScope(sourceID, "read"))
	}
	if d.Store == nil {
		return nil, errors.New("dev environments are not configured")
	}
	return d.Store.ListDevServers(ctx, sourceID)
}

// Preview compares a dev clone with its production source.
func (d *Dev) Preview(ctx context.Context, p Principal, devID string) (PromotionPreview, error) {
	dev, target, err := d.devPair(ctx, p, devID)
	if err != nil {
		return PromotionPreview{}, err
	}
	files, err := d.promotionFiles(ctx, dev, target)
	if err != nil {
		return PromotionPreview{}, err
	}
	return PromotionPreview{
		DevID: dev.ID, TargetID: target.ID, TargetName: target.Name,
		Plugins: pluginDiff(dev.Plugins, target.Plugins), Files: files,
	}, nil
}

// Promote stages selected dev changes on production. It does not restart
// production; plugin/file mutations mark the production server pending.
func (d *Dev) Promote(ctx context.Context, p Principal, devID string, includePlugins bool, selected []string) (PromotionResult, error) {
	dev, target, err := d.devPair(ctx, p, devID)
	if err != nil {
		return PromotionResult{}, err
	}
	preview, err := d.Preview(ctx, p, devID)
	if err != nil {
		return PromotionResult{}, err
	}
	result := PromotionResult{Pending: true}
	if includePlugins && len(preview.Plugins) > 0 {
		if d.Updates == nil {
			return PromotionResult{}, errors.New("plugin promotion is not configured")
		}
		if _, err := d.Updates.PromotePlugins(ctx, p, target.ID, dev.Plugins); err != nil {
			return PromotionResult{}, err
		}
		result.Plugins = len(preview.Plugins)
	}
	want := map[string]bool{}
	for _, path := range selected {
		want[path] = true
	}
	for _, change := range preview.Files {
		if !want[change.Path] {
			continue
		}
		content, _, _, err := d.Files.Read(ctx, dev, change.Path)
		if err != nil {
			return PromotionResult{}, fmt.Errorf("read dev %s: %w", change.Path, err)
		}
		production, productionSHA, _, readErr := d.Files.Read(ctx, target, change.Path)
		if readErr != nil {
			production, productionSHA = "", ""
		}
		if change.Path == "server.properties" {
			content = promoteProperties(content, production)
		}
		if _, _, err := d.Files.Write(ctx, p, target, change.Path, content, productionSHA, "dev.promote"); err != nil {
			return PromotionResult{}, fmt.Errorf("write production %s: %w", change.Path, err)
		}
		result.Files++
	}
	d.StoreAudit(ctx, p, "dev.promote", dev.ID, map[string]any{
		"target_id": target.ID, "plugins": result.Plugins, "files": result.Files,
	})
	return result, nil
}

func (d *Dev) devPair(ctx context.Context, p Principal, devID string) (store.Server, store.Server, error) {
	if d.Store == nil {
		return store.Server{}, store.Server{}, errors.New("dev environments are not configured")
	}
	dev, err := d.Store.GetServer(ctx, devID)
	if err != nil {
		return store.Server{}, store.Server{}, err
	}
	if dev.DevOf == "" {
		return store.Server{}, store.Server{}, invalid("server is not a dev clone")
	}
	target, err := d.Store.GetServer(ctx, dev.DevOf)
	if err != nil {
		return store.Server{}, store.Server{}, err
	}
	if target.OrgID != p.OrgID || dev.OrgID != p.OrgID {
		return store.Server{}, store.Server{}, forbidden(serverScope(devID, "update"))
	}
	if !p.Allows(serverScope(target.ID, "update")) {
		return store.Server{}, store.Server{}, forbidden(serverScope(target.ID, "update"))
	}
	return dev, target, nil
}

func pluginDiff(dev, target []store.ServerPlugin) []PromotionPluginChange {
	byProject := make(map[string]store.ServerPlugin, len(target))
	for _, p := range target {
		byProject[strings.ToLower(p.Project)] = p
	}
	seen := map[string]bool{}
	var out []PromotionPluginChange
	for _, p := range dev {
		key := strings.ToLower(p.Project)
		seen[key] = true
		old, ok := byProject[key]
		switch {
		case !ok:
			out = append(out, PromotionPluginChange{Project: p.Project, Provider: p.Provider, Next: p.Version, Change: "added"})
		case old.Version != p.Version || old.SHA256 != p.SHA256 || old.Provider != p.Provider:
			out = append(out, PromotionPluginChange{Project: p.Project, Provider: p.Provider, Current: old.Version, Next: p.Version, Change: "changed"})
		case old.Disabled != p.Disabled:
			change := "activated"
			if p.Disabled {
				change = "deactivated"
			}
			out = append(out, PromotionPluginChange{Project: p.Project, Provider: p.Provider, Current: old.Version, Next: p.Version, Change: change})
		}
	}
	for _, p := range target {
		if !seen[strings.ToLower(p.Project)] {
			out = append(out, PromotionPluginChange{Project: p.Project, Provider: p.Provider, Current: p.Version, Change: "removed"})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project == out[j].Project {
			return out[i].Change < out[j].Change
		}
		return out[i].Project < out[j].Project
	})
	return out
}

var promotionRoots = []string{
	"server.properties", "bukkit.yml", "spigot.yml", "paper-global.yml",
	"paper-world-defaults.yml", "commands.yml", "help.yml", "permissions.yml", "purpur.yml",
}

var promotionSkipDirs = map[string]bool{
	"world": true, "world_nether": true, "world_the_end": true,
	"logs": true, "cache": true, "crash": true, "backups": true,
	"libraries": true, "versions": true, "playerdata": true, "data": true,
	"sessions": true, "stats": true, "advancements": true,
}

func (d *Dev) promotionFiles(ctx context.Context, dev, target store.Server) ([]PromotionFileChange, error) {
	if d.Files == nil {
		return nil, errors.New("file promotion is not configured")
	}
	paths := map[string]bool{}
	for _, p := range promotionRoots {
		paths[p] = true
	}
	for _, root := range []string{"config", "plugins"} {
		d.walkPromotionFiles(ctx, dev, root, paths)
	}
	list := make([]string, 0, len(paths))
	for p := range paths {
		list = append(list, p)
	}
	sort.Strings(list)
	var out []PromotionFileChange
	for _, p := range list {
		devContent, _, size, err := d.Files.Read(ctx, dev, p)
		if err != nil {
			continue
		}
		production, productionSHA, _, prodErr := d.Files.Read(ctx, target, p)
		if p == "server.properties" {
			devContent = promoteProperties(devContent, production)
			size = int64(len(devContent))
		}
		devSHA := contentSHA(devContent)
		if prodErr == nil && devSHA == productionSHA {
			continue
		}
		status := "changed"
		if prodErr != nil {
			status = "added"
			productionSHA = ""
		}
		out = append(out, PromotionFileChange{
			Path: p, Status: status, ProductionSHA: productionSHA, DevSHA: devSHA, Size: size,
			Diff: promotionDiff(p, production, devContent),
		})
	}
	return out, nil
}

func (d *Dev) walkPromotionFiles(ctx context.Context, srv store.Server, dir string, out map[string]bool) {
	entries, err := d.Files.List(ctx, srv, dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		rel := path.Join(dir, entry.Name)
		if entry.Dir {
			if promotionSkipDirs[strings.ToLower(entry.Name)] {
				continue
			}
			d.walkPromotionFiles(ctx, srv, rel, out)
			continue
		}
		if isPromotionFile(rel) {
			out[rel] = true
		}
	}
}

func isPromotionFile(rel string) bool {
	base := strings.ToLower(path.Base(rel))
	ext := path.Ext(base)
	if ext != ".yml" && ext != ".yaml" && ext != ".json" && ext != ".properties" && ext != ".conf" && ext != ".toml" {
		return false
	}
	for _, bad := range []string{"whitelist.json", "ops.json", "usercache.json", "banned-ips.json", "banned-players.json"} {
		if base == bad {
			return false
		}
	}
	lower := strings.ToLower(rel)
	for _, bad := range []string{"/playerdata/", "/data/", "/sessions/", "/stats/", "/advancements/"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	return true
}

func promoteProperties(dev, production string) string {
	protected := []string{"server-port", "server-ip", "white-list", "enforce-whitelist", "level-name", "enable-rcon", "rcon.port"}
	out := dev
	for _, key := range protected {
		if value, ok := propertyValue(production, key); ok {
			out = setProperty(out, key, value)
		}
	}
	return out
}

func propertyValue(content, key string) (string, bool) {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

func setProperty(content, key, value string) string {
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if ok && strings.TrimSpace(k) == key {
			lines[i] = key + "=" + value
			found = true
		}
	}
	if !found {
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, key+"="+value)
	}
	return strings.Join(lines, "\n")
}

func promotionDiff(path, before, after string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(before),
		B:        difflib.SplitLines(after),
		FromFile: "production/" + path,
		ToFile:   "dev/" + path,
		Context:  3,
	})
	if err != nil {
		return ""
	}
	const max = 64 << 10
	if len(diff) > max {
		diff = diff[:max] + "\n... diff truncated ...\n"
	}
	return diff
}

func contentSHA(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// StoreAudit records promotion without requiring the full server store.
func (d *Dev) StoreAudit(ctx context.Context, p Principal, action, target string, diff any) {
	if d.Store == nil {
		return
	}
	if auditor, ok := d.Store.(interface {
		Audit(context.Context, string, string, string, string, string, any) error
	}); ok {
		_ = auditor.Audit(ctx, p.OrgID, p.UserID, p.Via, action, target, diff)
	}
}

// Cleanup deletes dev clones whose TTL elapsed. It uses a system principal
// scoped to the owning org so the normal delete/sync/audit path still runs.
func (d *Dev) Cleanup(ctx context.Context, now time.Time) int {
	if d.Store == nil || d.Servers == nil {
		return 0
	}
	items, err := d.Store.ExpiredDevServers(ctx, now)
	if err != nil {
		d.log().Warn("dev cleanup list", "err", err)
		return 0
	}
	n := 0
	for _, srv := range items {
		p := Principal{
			UserID: "dev-cleanup", OrgID: srv.OrgID, Via: "system",
			Scopes: []string{"server:*:update"},
		}
		if _, err := d.Servers.Delete(ctx, p, srv.ID); err != nil {
			d.log().Warn("dev cleanup delete", "server", srv.ID, "err", err)
			continue
		}
		n++
	}
	return n
}

func (d *Dev) lookupUUID(ctx context.Context, name string) (string, error) {
	base := strings.TrimRight(d.MojangBase, "/")
	if base == "" {
		base = "https://api.mojang.com"
	}
	client := d.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/users/profiles/minecraft/"+url.PathEscape(name), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "delphi/1.0 (minecraft control plane)")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Minecraft profile lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return "", invalid("Minecraft user %q was not found", name)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Minecraft profile lookup: %s", resp.Status)
	}
	var profile struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return "", err
	}
	if len(profile.ID) != 32 {
		return "", errors.New("Minecraft profile lookup returned an invalid UUID")
	}
	return formatUUID(profile.ID), nil
}

func formatUUID(raw string) string {
	if len(raw) != 32 {
		return raw
	}
	return raw[0:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}

func (d *Dev) log() *slog.Logger {
	if d.Log != nil {
		return d.Log
	}
	return slog.Default()
}
