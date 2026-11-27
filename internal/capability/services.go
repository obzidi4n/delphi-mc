package capability

import (
	"context"
	"strings"
	"time"

	"delphi.dev/delphi/internal/store"
)

// Services bundles the capabilities the API exposes.
type Services struct {
	Servers   *Servers
	Fleet     *Fleet
	Groups    *Groups
	Audit     *Audit
	Keys      *Keys
	Users     *Users
	Templates *Templates
	Updates   *Updates
	Backups   *Backups
	Schedules *Schedules
	Alerts    *Alerts
	Networks  *Networks
}

// FleetStore is node and enrollment persistence.
type FleetStore interface {
	ListNodes(ctx context.Context, limit int, after string) ([]store.Node, string, error)
	CreateEnrollmentToken(ctx context.Context, orgID string, ttl time.Duration) (string, time.Time, error)
	RevokeNode(ctx context.Context, id string) error
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// GroupsStore is group persistence.
type GroupsStore interface {
	CreateGroup(ctx context.Context, orgID, name, description string) (store.Group, error)
	ListGroups(ctx context.Context, orgID string, limit int, after string) ([]store.Group, string, error)
	AddGroupMember(ctx context.Context, orgID, groupID, serverID string) error
	RemoveGroupMember(ctx context.Context, orgID, groupID, serverID string) error
	ListGroupMembers(ctx context.Context, orgID, groupID string, limit int, after string) ([]string, string, error)
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// AuditStore is the audit reader.
type AuditStore interface {
	ListAudit(ctx context.Context, orgID string, limit int, after string) ([]store.AuditEntry, string, error)
}

// KeyStore mints API keys with the caller's scopes.
type KeyStore interface {
	CreateAPIKey(ctx context.Context, orgID, userID, name string, scopes []string) (string, store.APIKey, error)
}

// Fleet manages nodes and enrollment tokens.
type Fleet struct{ Store FleetStore }

// Groups manages targeting groups.
type Groups struct{ Store GroupsStore }

// Audit reads the audit log.
type Audit struct{ Store AuditStore }

// Keys mints bearer tokens.
type Keys struct{ Store KeyStore }

// ListNodes returns the fleet's nodes.
func (f *Fleet) ListNodes(ctx context.Context, p Principal, limit int, after string) ([]store.Node, string, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, "", forbidden(orgScope(p, "read"))
	}
	return f.Store.ListNodes(ctx, limit, after)
}

// CreateToken issues a single-use enrollment token.
func (f *Fleet) CreateToken(ctx context.Context, p Principal, ttl time.Duration) (string, time.Time, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return "", time.Time{}, forbidden(orgScope(p, "admin"))
	}
	token, expires, err := f.Store.CreateEnrollmentToken(ctx, p.OrgID, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	if err := f.audit(ctx, p, "node.token", p.OrgID, nil); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// RevokeNode revokes a node certificate.
func (f *Fleet) RevokeNode(ctx context.Context, p Principal, id string) error {
	if !p.Allows(orgScope(p, "admin")) {
		return forbidden(orgScope(p, "admin"))
	}
	if err := f.Store.RevokeNode(ctx, id); err != nil {
		return err
	}
	return f.audit(ctx, p, "node.revoke", id, nil)
}

func (f *Fleet) audit(ctx context.Context, p Principal, action, target string, diff any) error {
	return f.Store.Audit(ctx, p.OrgID, actor(p), p.Via, action, target, diff)
}

// Create adds a group.
func (g *Groups) Create(ctx context.Context, p Principal, name, description string) (store.Group, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return store.Group{}, forbidden(orgScope(p, "admin"))
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return store.Group{}, invalid("group name must be 1-64 characters")
	}
	group, err := g.Store.CreateGroup(ctx, p.OrgID, name, description)
	if err != nil {
		return store.Group{}, err
	}
	if err := g.Store.Audit(ctx, p.OrgID, actor(p), p.Via, "group.create", group.ID, map[string]any{"name": name}); err != nil {
		return store.Group{}, err
	}
	return group, nil
}

// List returns groups.
func (g *Groups) List(ctx context.Context, p Principal, limit int, after string) ([]store.Group, string, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, "", forbidden(orgScope(p, "read"))
	}
	return g.Store.ListGroups(ctx, p.OrgID, limit, after)
}

// AddMember attaches a server to a group.
func (g *Groups) AddMember(ctx context.Context, p Principal, groupID, serverID string) error {
	if !p.Allows(orgScope(p, "admin")) {
		return forbidden(orgScope(p, "admin"))
	}
	if err := g.Store.AddGroupMember(ctx, p.OrgID, groupID, serverID); err != nil {
		return err
	}
	return g.Store.Audit(ctx, p.OrgID, actor(p), p.Via, "group.add_member", groupID, map[string]any{"server_id": serverID})
}

// RemoveMember detaches a server from a group.
func (g *Groups) RemoveMember(ctx context.Context, p Principal, groupID, serverID string) error {
	if !p.Allows(orgScope(p, "admin")) {
		return forbidden(orgScope(p, "admin"))
	}
	if err := g.Store.RemoveGroupMember(ctx, p.OrgID, groupID, serverID); err != nil {
		return err
	}
	return g.Store.Audit(ctx, p.OrgID, actor(p), p.Via, "group.remove_member", groupID, map[string]any{"server_id": serverID})
}

// Members lists a group's server ids.
func (g *Groups) Members(ctx context.Context, p Principal, groupID string, limit int, after string) ([]string, string, error) {
	if !p.Allows(orgScope(p, "read")) {
		return nil, "", forbidden(orgScope(p, "read"))
	}
	return g.Store.ListGroupMembers(ctx, p.OrgID, groupID, limit, after)
}

// List returns audit entries, newest first.
func (a *Audit) List(ctx context.Context, p Principal, limit int, after string) ([]store.AuditEntry, string, error) {
	if !p.Allows(orgScope(p, "admin")) {
		return nil, "", forbidden(orgScope(p, "admin"))
	}
	return a.Store.ListAudit(ctx, p.OrgID, limit, after)
}

// Create mints an API key with the caller's scopes; it can never exceed them.
func (k *Keys) Create(ctx context.Context, p Principal, name string) (string, store.APIKey, error) {
	if p.UserID == "" {
		return "", store.APIKey{}, forbidden("a user principal is required to mint a key")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return "", store.APIKey{}, invalid("key name must be 1-64 characters")
	}
	if len(p.Scopes) == 0 {
		return "", store.APIKey{}, forbidden("the principal has no scopes to delegate")
	}
	return k.Store.CreateAPIKey(ctx, p.OrgID, p.UserID, name, p.Scopes)
}

func actor(p Principal) string {
	if p.UserID != "" {
		return p.UserID
	}
	return "bootstrap"
}
