// Package capability is the only mutation surface. Handlers are thin adapters;
// this package validates, authorizes (scopes arrive at M3a), and notifies.
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

	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// Principal identifies the actor. Scopes land at M3a; the struct exists now so
// no caller learns to mutate without one.
type Principal struct {
	UserID string
	OrgID  string
	Via    string // "api" | "ui" | "cli" | "ai:<conversation-id>"
}

// ErrInvalid marks a validation failure (HTTP 400).
var ErrInvalid = errors.New("invalid request")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ServerStore is the persistence the capability needs.
type ServerStore interface {
	CreateServer(ctx context.Context, s store.Server) (store.Server, error)
	GetServer(ctx context.Context, id string) (store.Server, error)
	ListServers(ctx context.Context) ([]store.Server, error)
	ListServersByNode(ctx context.Context, nodeID string) ([]store.Server, error)
	SetDesiredState(ctx context.Context, id, desired string, kill bool) (store.Server, error)
	ReportServerState(ctx context.Context, nodeID, id, state string, revision int64, detail string) error
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
	Name       string
	NodeID     string
	Image      string
	MemoryMB   int
	CPUCores   float64
	Port       int
	JarURL     string
	JarSHA256  string
	AcceptEULA bool
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Create validates and persists a server, then syncs its node.
func (c *Servers) Create(ctx context.Context, p Principal, req CreateRequest) (store.Server, error) {
	if p.Via == "" {
		return store.Server{}, errors.New("capability: principal is missing Via")
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		return store.Server{}, invalid("name must be 1-64 characters")
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
	if !req.AcceptEULA {
		return store.Server{}, invalid("accept_eula must be true: the operator accepts the Minecraft EULA")
	}
	javaVersion := 21

	created, err := c.Store.CreateServer(ctx, store.Server{
		ID:           newServerID(),
		NodeID:       req.NodeID,
		Name:         req.Name,
		DesiredState: "stopped",
		AcceptEULA:   req.AcceptEULA,
		Image:        req.Image,
		JavaVersion:  javaVersion,
		MemoryMB:     req.MemoryMB,
		CPUCores:     req.CPUCores,
		Port:         req.Port,
		JarURL:       req.JarURL,
		JarSHA256:    strings.ToLower(req.JarSHA256),
	})
	if err != nil {
		return store.Server{}, err
	}

	c.sync(ctx, created.NodeID)
	return created, nil
}

// Power applies start/stop/restart/kill as a desired-state transition.
func (c *Servers) Power(ctx context.Context, p Principal, id, action string) (store.Server, error) {
	if p.Via == "" {
		return store.Server{}, errors.New("capability: principal is missing Via")
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
	c.sync(ctx, updated.NodeID)
	return updated, nil
}

// List returns the fleet.
func (c *Servers) List(ctx context.Context, _ Principal) ([]store.Server, error) {
	return c.Store.ListServers(ctx)
}

// Get returns one server.
func (c *Servers) Get(ctx context.Context, _ Principal, id string) (store.Server, error) {
	return c.Store.GetServer(ctx, id)
}

// SpecsForNode builds the desired frame for a node.
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
		})
	}
	return out, nil
}

// ReportState records a node's observation, scoped to that node.
func (c *Servers) ReportState(ctx context.Context, nodeID string, frame wire.ServerStateFrame) error {
	return c.Store.ReportServerState(ctx, nodeID, frame.ServerID, frame.State, frame.Revision, frame.Detail)
}

func (c *Servers) sync(ctx context.Context, nodeID string) {
	if c.Notify != nil {
		_ = c.Notify.Sync(ctx, nodeID)
	}
}

func newServerID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic("capability: crypto/rand failed: " + err.Error())
	}
	return "s_" + hex.EncodeToString(raw)
}
