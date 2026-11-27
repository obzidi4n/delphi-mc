// Package capability is the only mutation surface. Handlers are thin adapters;
// this package authorizes, validates, audits, and notifies.
package capability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// Principal identifies the actor and what it may do.
type Principal struct {
	UserID string
	OrgID  string
	Via    string // "ui" | "cli" | "api" | "ai:<conversation-id>"
	Model  string
	Scopes []string
}

// Allows reports whether the principal holds a scope.
func (p Principal) Allows(scope string) bool { return auth.Allows(p.Scopes, scope) }

// Capability errors.
var (
	ErrInvalid      = errors.New("invalid request")
	ErrForbidden    = errors.New("forbidden")
	ErrUnauthorized = errors.New("unauthenticated")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func forbidden(scope string) error {
	return fmt.Errorf("%w: missing scope %q", ErrForbidden, scope)
}

// ServerStore is the persistence the server capability needs.
type ServerStore interface {
	CreateServer(ctx context.Context, s store.Server) (store.Server, error)
	GetServer(ctx context.Context, id string) (store.Server, error)
	ListServers(ctx context.Context, limit int, after string) ([]store.Server, string, error)
	AllocatePort(ctx context.Context, nodeID string) (int, error)
	ListServersByNode(ctx context.Context, nodeID string) ([]store.Server, error)
	SetDesiredState(ctx context.Context, id, desired string, kill bool) (store.Server, error)
	RenameServer(ctx context.Context, id, name string) (store.Server, error)
	SoftDeleteServer(ctx context.Context, id string) (store.Server, error)
	ReportServerState(ctx context.Context, nodeID, id, state string, revision int64, detail string) error
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// Notifier pushes a node's desired set over its live channel.
type Notifier interface {
	Sync(ctx context.Context, nodeID string) error
}

// Servers is the server-lifecycle capability.
type Servers struct {
	Store  ServerStore
	Notify Notifier
}

// CreateRequest is a server creation request.
type CreateRequest struct {
	Name            string
	NodeID          string
	Image           string
	MemoryMB        int
	CPUCores        float64
	Port            int
	JarURL          string
	JarSHA256       string
	AcceptEULA      bool
	GroupID         string
	DesiredState    string
	Base            string
	TemplateID      string
	TemplateVersion int
	JavaVersion     int
	Variables       map[string]string
	Files           map[string]string
	MCVersion       string
	Plugins         []store.ServerPlugin
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Create validates and persists a server, then syncs its node.
func (c *Servers) Create(ctx context.Context, p Principal, req CreateRequest) (store.Server, error) {
	if err := c.require(p, orgScope(p, "server:create")); err != nil {
		return store.Server{}, err
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		return store.Server{}, invalid("name must be 1-64 characters")
	}
	if req.DesiredState == "" {
		req.DesiredState = "stopped"
	}
	if req.DesiredState != "stopped" && req.DesiredState != "running" {
		return store.Server{}, invalid("desired_state must be stopped or running")
	}
	if req.NodeID == "" {
		return store.Server{}, invalid("node_id is required")
	}
	if req.Image == "" {
		return store.Server{}, invalid("image is required")
	}
	if req.MemoryMB < 512 || req.MemoryMB > 32768 {
		return store.Server{}, invalid("memory_mb must be between 512 and 32768")
	}
	if req.CPUCores == 0 {
		req.CPUCores = 1
	}
	if req.CPUCores < 0.1 || req.CPUCores > 16 {
		return store.Server{}, invalid("cpu_cores must be between 0.1 and 16")
	}
	if req.Port != 0 && (req.Port < 1024 || req.Port > 65535) {
		return store.Server{}, invalid("port must be between 1024 and 65535")
	}
	u, err := url.Parse(req.JarURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return store.Server{}, invalid("jar_url must be an absolute https URL")
	}
	if !sha256Hex.MatchString(strings.ToLower(req.JarSHA256)) {
		return store.Server{}, invalid("jar_sha256 must be 64 lowercase hex characters")
	}
	if req.Base == "" {
		req.Base = "paper"
	}
	if req.Base != "paper" && req.Base != "purpur" && req.Base != "folia" && req.Base != "velocity" {
		return store.Server{}, invalid("base must be paper, purpur, folia, or velocity")
	}
	if req.Base != "velocity" && !req.AcceptEULA {
		return store.Server{}, invalid("accept_eula must be true: the operator accepts the Minecraft EULA")
	}
	if req.JavaVersion == 0 {
		req.JavaVersion = 21
	}
	if req.Variables == nil {
		req.Variables = map[string]string{}
	}
	if req.Files == nil {
		req.Files = map[string]string{}
	}

	created, err := c.Store.CreateServer(ctx, store.Server{
		ID:              newServerID(),
		NodeID:          req.NodeID,
		Name:            req.Name,
		DesiredState:    req.DesiredState,
		AcceptEULA:      req.AcceptEULA,
		Image:           req.Image,
		JavaVersion:     req.JavaVersion,
		MemoryMB:        req.MemoryMB,
		CPUCores:        req.CPUCores,
		Port:            req.Port,
		JarURL:          req.JarURL,
		JarSHA256:       strings.ToLower(req.JarSHA256),
		Base:            req.Base,
		TemplateID:      req.TemplateID,
		TemplateVersion: req.TemplateVersion,
		Variables:       req.Variables,
		Files:           req.Files,
		MCVersion:       req.MCVersion,
		Plugins:         req.Plugins,
	})
	if err != nil {
		return store.Server{}, err
	}
	diff := map[string]any{
		"name": created.Name, "node_id": created.NodeID, "port": created.Port,
	}
	if created.TemplateID != "" {
		diff["template"] = created.TemplateID
		diff["template_version"] = created.TemplateVersion
	}
	if err := c.audit(ctx, p, "server.create", created.ID, diff); err != nil {
		return store.Server{}, err
	}

	c.sync(ctx, created.NodeID)
	return created, nil
}

// Power applies start/stop/restart/kill as a desired-state transition.
func (c *Servers) Power(ctx context.Context, p Principal, id, action string) (store.Server, error) {
	if _, err := c.Store.GetServer(ctx, id); err != nil {
		return store.Server{}, err
	}
	if err := c.require(p, serverScope(id, "power")); err != nil {
		return store.Server{}, err
	}

	var desired string
	var kill bool
	switch action {
	case "start", "restart":
		desired = "running"
	case "stop":
		desired = "stopped"
	case "kill":
		desired, kill = "stopped", true
	default:
		return store.Server{}, invalid("action must be one of start, stop, restart, kill")
	}

	updated, err := c.Store.SetDesiredState(ctx, id, desired, kill)
	if err != nil {
		return store.Server{}, err
	}
	if err := c.audit(ctx, p, "server.power", id, map[string]any{"action": action}); err != nil {
		return store.Server{}, err
	}
	c.sync(ctx, updated.NodeID)
	return updated, nil
}

// Rename changes a server's display name.
func (c *Servers) Rename(ctx context.Context, p Principal, id, name string) (store.Server, error) {
	if _, err := c.Store.GetServer(ctx, id); err != nil {
		return store.Server{}, err
	}
	if err := c.require(p, serverScope(id, "update")); err != nil {
		return store.Server{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return store.Server{}, invalid("name must be 1-64 characters")
	}

	updated, err := c.Store.RenameServer(ctx, id, name)
	if err != nil {
		return store.Server{}, err
	}
	if err := c.audit(ctx, p, "server.rename", id, map[string]any{"name": name}); err != nil {
		return store.Server{}, err
	}
	return updated, nil
}

// Delete stops the server, tombstones it, and lets the node remove the container.
func (c *Servers) Delete(ctx context.Context, p Principal, id string) (store.Server, error) {
	srv, err := c.Store.GetServer(ctx, id)
	if err != nil {
		return store.Server{}, err
	}
	if err := c.require(p, serverScope(id, "update")); err != nil {
		return store.Server{}, err
	}

	updated, err := c.Store.SoftDeleteServer(ctx, id)
	if err != nil {
		return store.Server{}, err
	}
	if err := c.audit(ctx, p, "server.delete", id, map[string]any{"name": srv.Name}); err != nil {
		return store.Server{}, err
	}
	c.sync(ctx, updated.NodeID)
	return updated, nil
}

// List returns the fleet.
func (c *Servers) List(ctx context.Context, p Principal, limit int, after string) ([]store.Server, string, error) {
	if err := c.require(p, orgScope(p, "read")); err != nil {
		return nil, "", err
	}
	return c.Store.ListServers(ctx, limit, after)
}

// Get returns one server.
func (c *Servers) Get(ctx context.Context, p Principal, id string) (store.Server, error) {
	if err := c.require(p, serverScope(id, "read")); err != nil {
		return store.Server{}, err
	}
	return c.Store.GetServer(ctx, id)
}

// SpecsForNode builds the desired frame for a node. It is internal: the hub
// calls it for the node's own authenticated channel, not on a user's behalf.
func (c *Servers) SpecsForNode(ctx context.Context, nodeID string) ([]wire.ServerSpec, error) {
	servers, err := c.Store.ListServersByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]wire.ServerSpec, 0, len(servers))
	for _, s := range servers {
		out = append(out, wire.ServerSpec{
			ID:            s.ID,
			Name:          s.Name,
			Revision:      s.DesiredRevision,
			DesiredState:  s.DesiredState,
			KillRequested: s.KillRequested,
			AcceptEULA:    s.AcceptEULA,
			Image:         s.Image,
			JavaVersion:   s.JavaVersion,
			MemoryMB:      s.MemoryMB,
			CPUCores:      s.CPUCores,
			Port:          s.Port,
			JarURL:        s.JarURL,
			JarSHA256:     s.JarSHA256,
			Base:          s.Base,
			Files:         s.Files,
			MCVersion:     s.MCVersion,
			RestartPolicy: s.RestartPolicy,
			NetworkName:   networkName(s.NetworkID),
			NetworkRole:   s.NetworkRole,
			NetworkAlias:  networkAlias(s.ID),
			Plugins:       pluginSpecs(s.Plugins),
			Deleted:       s.DeletedAt != nil,
		})
	}
	return out, nil
}

func networkName(id string) string {
	if id == "" {
		return ""
	}
	return "delphi-net-" + id
}

func networkAlias(id string) string {
	return "delphi-" + strings.ReplaceAll(id, "_", "-")
}

func pluginSpecs(in []store.ServerPlugin) []wire.PluginSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]wire.PluginSpec, 0, len(in))
	for _, p := range in {
		out = append(out, wire.PluginSpec{Name: p.Name, SHA256: p.SHA256})
	}
	return out
}

// ReportState records a node's observation, scoped to that node.
func (c *Servers) ReportState(ctx context.Context, nodeID string, frame wire.ServerStateFrame) error {
	return c.Store.ReportServerState(ctx, nodeID, frame.ServerID, frame.State, frame.Revision, frame.Detail)
}

func (c *Servers) require(p Principal, scope string) error {
	if p.Via == "" {
		return fmt.Errorf("%w: principal is missing Via", ErrUnauthorized)
	}
	if p.UserID == "" && p.Via != "api" {
		return fmt.Errorf("%w: principal is missing a user", ErrUnauthorized)
	}
	if !p.Allows(scope) {
		return forbidden(scope)
	}
	return nil
}

func (c *Servers) audit(ctx context.Context, p Principal, action, target string, diff any) error {
	actor := p.UserID
	if actor == "" {
		actor = "bootstrap"
	}
	return c.Store.Audit(ctx, p.OrgID, actor, p.Via, action, target, diff)
}

func (c *Servers) sync(ctx context.Context, nodeID string) {
	if c.Notify != nil {
		_ = c.Notify.Sync(ctx, nodeID)
	}
}

func orgScope(p Principal, verb string) string {
	return "org:" + p.OrgID + ":" + verb
}

func serverScope(id, verb string) string {
	return "server:" + id + ":" + verb
}

func newServerID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic("capability: crypto/rand failed: " + err.Error())
	}
	return "s_" + hex.EncodeToString(raw)
}
