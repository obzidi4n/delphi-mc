package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

type fakeUserStore struct {
	users  []store.UserWithRole
	audits []string
}

func (f *fakeUserStore) ListUsers(context.Context, string, int, string) ([]store.UserWithRole, string, error) {
	return f.users, "", nil
}

func (f *fakeUserStore) UserByID(_ context.Context, _, id string) (store.UserWithRole, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return store.UserWithRole{}, store.ErrNoUser
}

func (f *fakeUserStore) CreateUser(_ context.Context, orgID, email, hash string) (store.User, error) {
	u := store.User{ID: "u_new", OrgID: orgID, Email: email, PasswordHash: hash, CreatedAt: time.Now()}
	f.users = append(f.users, store.UserWithRole{User: u})
	return u, nil
}

func (f *fakeUserStore) SetOrgRole(_ context.Context, id, _, role string) error {
	for i := range f.users {
		if f.users[i].ID == id {
			f.users[i].Role = role
			return nil
		}
	}
	return store.ErrNoUser
}

func (f *fakeUserStore) CountOwners(context.Context, string) (int, error) {
	n := 0
	for _, u := range f.users {
		if u.Role == "owner" {
			n++
		}
	}
	return n, nil
}

func (f *fakeUserStore) Audit(_ context.Context, _, _, _, action, target string, _ any) error {
	f.audits = append(f.audits, action+" "+target)
	return nil
}

type fakeGroupStore struct {
	groups  []store.Group
	members map[string][]string
}

func (f *fakeGroupStore) ListGroups(context.Context, string, int, string) ([]store.Group, string, error) {
	return f.groups, "", nil
}

func (f *fakeGroupStore) CreateGroup(_ context.Context, orgID, name, description string) (store.Group, error) {
	g := store.Group{ID: "g_new", OrgID: orgID, Name: name, Description: description, CreatedAt: time.Now()}
	f.groups = append(f.groups, g)
	return g, nil
}

func (f *fakeGroupStore) AddGroupMember(_ context.Context, _, groupID, serverID string) error {
	f.members[groupID] = append(f.members[groupID], serverID)
	return nil
}

func (f *fakeGroupStore) RemoveGroupMember(_ context.Context, _, groupID, serverID string) error {
	var kept []string
	for _, id := range f.members[groupID] {
		if id != serverID {
			kept = append(kept, id)
		}
	}
	f.members[groupID] = kept
	return nil
}

func (f *fakeGroupStore) ListGroupMembers(_ context.Context, _, groupID string, _ int, _ string) ([]string, string, error) {
	return f.members[groupID], "", nil
}

func (f *fakeGroupStore) Audit(context.Context, string, string, string, string, string, any) error {
	return nil
}

func newAdminHandler(t *testing.T, scopes []string) (http.Handler, *fakeIdentity, *fakeUserStore, *fakeGroupStore) {
	t.Helper()
	identity := newFakeIdentity(t)
	identity.scopes = scopes
	users := &fakeUserStore{users: []store.UserWithRole{{
		User: identity.user, Role: "owner",
	}}}
	groups := &fakeGroupStore{
		groups:  []store.Group{{ID: "g_1", OrgID: "default", Name: "smp", Description: "survival"}},
		members: map[string][]string{"g_1": {"s_1"}},
	}
	servers := &fakeServerStore{servers: []store.Server{{
		ID: "s_1", OrgID: "default", Name: "survival-demo", ObservedState: "stopped",
	}}}
	h, err := api.New(api.Options{
		BasePath: "/",
		Version:  "test",
		Identity: identity,
		Services: &capability.Services{
			Users:   &capability.Users{Store: users},
			Groups:  &capability.Groups{Store: groups},
			Servers: &capability.Servers{Store: servers},
			Fleet:   &capability.Fleet{Store: &fakeNodes{}},
		},
		Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, identity, users, groups
}

func loginCookie(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	form := url.Values{"email": {"owner@delphi.local"}, "password": {"correct horse"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	return cookies[0]
}

func TestTeamPageListsAccounts(t *testing.T) {
	h, _, _, _ := newAdminHandler(t, []string{"*"})
	req := httptest.NewRequest(http.MethodGet, "/team", nil)
	req.AddCookie(loginCookie(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"owner@delphi.local", "Add account", ">owner<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("team page missing %q:\n%s", want, body)
		}
	}
}

func TestCreateUserShowsPasswordOnce(t *testing.T) {
	h, _, users, _ := newAdminHandler(t, []string{"*"})
	form := url.Values{"email": {"builder@delphi.local"}, "role": {"developer"}}
	req := httptest.NewRequest(http.MethodPost, "/team", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(loginCookie(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Password for builder@delphi.local") {
		t.Fatalf("password was not shown once: %d\n%s", rec.Code, body)
	}
	var created store.UserWithRole
	for _, u := range users.users {
		if u.Email == "builder@delphi.local" {
			created = u
		}
	}
	if created.Role != "developer" {
		t.Fatalf("role = %q, want developer", created.Role)
	}
	start := strings.Index(body, "<code>")
	end := strings.Index(body, "</code>")
	if start < 0 || end < start {
		t.Fatalf("no password in page: %s", body)
	}
	if !auth.VerifyPassword(created.PasswordHash, body[start+6:end]) {
		t.Fatal("displayed password does not match the stored hash")
	}
}

func TestHomePageGroupsServers(t *testing.T) {
	h, _, _, _ := newAdminHandler(t, []string{"*"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(loginCookie(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"Servers by group", "smp", "survival-demo", "Every server belongs to a group."} {
		if !strings.Contains(body, want) {
			t.Fatalf("home page missing %q:\n%s", want, body)
		}
	}
}

func TestWebOnboardingPages(t *testing.T) {
	h, _, _, _ := newAdminHandler(t, []string{"*"})
	for _, path := range []string{"/nodes", "/servers/new"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(loginCookie(t, h))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d: %s", path, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.AddCookie(loginCookie(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, want := range []string{"Add a node", "Generate enrollment token"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("nodes page missing %q", want)
		}
	}
}

func TestGroupsPageRendersMembers(t *testing.T) {
	h, _, _, _ := newAdminHandler(t, []string{"*"})
	req := httptest.NewRequest(http.MethodGet, "/groups", nil)
	req.AddCookie(loginCookie(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"smp", "survival", "survival-demo", "Remove", "add a server"} {
		if !strings.Contains(body, want) {
			t.Fatalf("groups page missing %q:\n%s", want, body)
		}
	}
}

func TestViewerCannotListTeam(t *testing.T) {
	h, _, _, _ := newAdminHandler(t, []string{"org:default:read", "server:*:read"})
	req := httptest.NewRequest(http.MethodGet, "/team", nil)
	req.AddCookie(loginCookie(t, h))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "missing scope") {
		t.Fatalf("viewer saw the team: %d\n%s", rec.Code, rec.Body.String())
	}
}
