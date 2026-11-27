package capability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// DevStore is the persistence the dev-server capability reads.
type DevStore interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	ExpiredDevServers(ctx context.Context, now time.Time) ([]store.Server, error)
}

// Dev creates temporary same-node clones for testing settings and plugins.
type Dev struct {
	Store      DevStore
	Servers    *Servers
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
