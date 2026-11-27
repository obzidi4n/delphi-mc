package capability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/store"
)

type fakeUsers struct {
	users  []store.UserWithRole
	owners int
	audits []string
}

func (f *fakeUsers) ListUsers(context.Context, string) ([]store.UserWithRole, error) {
	return f.users, nil
}

func (f *fakeUsers) UserByID(_ context.Context, _, id string) (store.UserWithRole, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return store.UserWithRole{}, store.ErrNoUser
}

func (f *fakeUsers) CreateUser(_ context.Context, orgID, email, hash string) (store.User, error) {
	u := store.User{ID: "u_2", OrgID: orgID, Email: email, PasswordHash: hash, CreatedAt: time.Now()}
	f.users = append(f.users, store.UserWithRole{User: u})
	return u, nil
}

func (f *fakeUsers) SetOrgRole(_ context.Context, id, _, role string) error {
	for i := range f.users {
		if f.users[i].ID == id {
			f.users[i].Role = role
			return nil
		}
	}
	return store.ErrNoUser
}

func (f *fakeUsers) CountOwners(context.Context, string) (int, error) { return f.owners, nil }

func (f *fakeUsers) Audit(_ context.Context, _, _, _, action, target string, _ any) error {
	f.audits = append(f.audits, action+" "+target)
	return nil
}

func TestCreateUserNeedsUserAdmin(t *testing.T) {
	viewer := Principal{UserID: "u_1", OrgID: "default", Via: "ui",
		Scopes: []string{"org:default:read", "server:*:read"}}
	_, _, err := (&Users{Store: &fakeUsers{}}).Create(context.Background(), viewer,
		"new@delphi.local", "viewer")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestAdminCannotCreateOwner(t *testing.T) {
	admin := Principal{UserID: "u_1", OrgID: "default", Via: "ui",
		Scopes: []string{"org:default:user:admin"}}
	_, _, err := (&Users{Store: &fakeUsers{}}).Create(context.Background(), admin,
		"new@delphi.local", "owner")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden for owner creation", err)
	}
}

func TestCreateUserGeneratesPasswordAndAudits(t *testing.T) {
	fs := &fakeUsers{}
	owner := Principal{UserID: "u_1", OrgID: "default", Via: "ui", Scopes: []string{"*"}}
	user, password, err := (&Users{Store: fs}).Create(context.Background(), owner,
		"  Builder@Delphi.Local ", "developer")
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "builder@delphi.local" || user.Role != "developer" {
		t.Fatalf("user = %+v", user)
	}
	if len(password) < 20 || !auth.VerifyPassword(user.PasswordHash, password) {
		t.Fatal("generated password does not verify against the stored hash")
	}
	if len(fs.audits) != 1 || !strings.HasPrefix(fs.audits[0], "user.create u_2") {
		t.Fatalf("audits = %v", fs.audits)
	}
}

func TestCannotDemoteLastOwner(t *testing.T) {
	fs := &fakeUsers{
		users:  []store.UserWithRole{{User: store.User{ID: "u_owner"}, Role: "owner"}},
		owners: 1,
	}
	owner := Principal{UserID: "u_owner", OrgID: "default", Via: "ui", Scopes: []string{"*"}}
	err := (&Users{Store: fs}).SetRole(context.Background(), owner, "u_owner", "admin")
	if !errors.Is(err, ErrLastOwner) {
		t.Fatalf("err = %v, want ErrLastOwner", err)
	}
}
