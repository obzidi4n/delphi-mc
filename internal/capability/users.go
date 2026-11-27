package capability

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/store"
)

// ErrLastOwner refuses a change that would leave the org without an owner.
var ErrLastOwner = errors.New("the last owner cannot be demoted")

// UsersStore is account persistence.
type UsersStore interface {
	ListUsers(ctx context.Context, orgID string, limit int, after string) ([]store.UserWithRole, string, error)
	UserByID(ctx context.Context, orgID, id string) (store.UserWithRole, error)
	CreateUser(ctx context.Context, orgID, email, passwordHash string) (store.User, error)
	SetOrgRole(ctx context.Context, userID, orgID, role string) error
	CountOwners(ctx context.Context, orgID string) (int, error)
	Audit(ctx context.Context, orgID, actor, via, action, target string, diff any) error
}

// Users manages accounts and their org roles.
type Users struct{ Store UsersStore }

// List returns the org's accounts.
func (u *Users) List(ctx context.Context, p Principal, limit int, after string) ([]store.UserWithRole, string, error) {
	if !p.Allows(orgScope(p, "user:read")) {
		return nil, "", forbidden(orgScope(p, "user:read"))
	}
	return u.Store.ListUsers(ctx, p.OrgID, limit, after)
}

// Create adds an account with a generated password. The password is returned
// once and never stored in plaintext.
func (u *Users) Create(ctx context.Context, p Principal, email, role string) (store.UserWithRole, string, error) {
	if !p.Allows(orgScope(p, "user:admin")) {
		return store.UserWithRole{}, "", forbidden(orgScope(p, "user:admin"))
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := mail.ParseAddress(email); err != nil || len(email) > 254 {
		return store.UserWithRole{}, "", invalid("a valid email is required")
	}
	if role == "" {
		role = "viewer"
	}
	if !auth.ValidRole(role) {
		return store.UserWithRole{}, "", invalid("role must be owner, admin, developer, or viewer")
	}
	if role == "owner" && !p.Allows("*") {
		return store.UserWithRole{}, "", forbidden("only an owner can create another owner")
	}

	password := auth.NewSecret()
	hash, err := auth.HashPassword(password)
	if err != nil {
		return store.UserWithRole{}, "", err
	}
	user, err := u.Store.CreateUser(ctx, p.OrgID, email, hash)
	if err != nil {
		return store.UserWithRole{}, "", err
	}
	if err := u.Store.SetOrgRole(ctx, user.ID, p.OrgID, role); err != nil {
		return store.UserWithRole{}, "", err
	}
	if err := u.Store.Audit(ctx, p.OrgID, actor(p), p.Via, "user.create", user.ID,
		map[string]string{"email": email, "role": role}); err != nil {
		return store.UserWithRole{}, "", err
	}
	return store.UserWithRole{User: user, Role: role}, password, nil
}

// SetRole changes an account's org role.
func (u *Users) SetRole(ctx context.Context, p Principal, userID, role string) error {
	if !p.Allows(orgScope(p, "user:admin")) {
		return forbidden(orgScope(p, "user:admin"))
	}
	if !auth.ValidRole(role) {
		return invalid("role must be owner, admin, developer, or viewer")
	}
	target, err := u.Store.UserByID(ctx, p.OrgID, userID)
	if err != nil {
		return err
	}
	if role == "owner" && !p.Allows("*") {
		return forbidden("only an owner can grant the owner role")
	}
	if target.Role == "owner" && role != "owner" {
		owners, err := u.Store.CountOwners(ctx, p.OrgID)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	if err := u.Store.SetOrgRole(ctx, userID, p.OrgID, role); err != nil {
		return err
	}
	return u.Store.Audit(ctx, p.OrgID, actor(p), p.Via, "user.role", userID,
		map[string]string{"from": target.Role, "to": role})
}
