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
	"delphi.dev/delphi/internal/store"
)

type fakeIdentity struct {
	user     store.User
	sessions map[string]store.User
	scopes   []string
}

func newFakeIdentity(t *testing.T) *fakeIdentity {
	t.Helper()
	hash, err := auth.HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	return &fakeIdentity{
		user:     store.User{ID: "u_1", OrgID: "default", Email: "owner@delphi.local", PasswordHash: hash},
		sessions: map[string]store.User{},
		scopes:   []string{"*"},
	}
}

func (f *fakeIdentity) UserByEmail(_ context.Context, _, email string) (store.User, error) {
	if email != f.user.Email {
		return store.User{}, store.ErrNoUser
	}
	return f.user, nil
}

func (f *fakeIdentity) CreateSession(_ context.Context, _ string, _ time.Duration) (string, time.Time, error) {
	f.sessions["session-token"] = f.user
	return "session-token", time.Now().Add(time.Hour), nil
}

func (f *fakeIdentity) UserBySession(_ context.Context, token string) (store.User, error) {
	if u, ok := f.sessions[token]; ok {
		return u, nil
	}
	return store.User{}, store.ErrNoSession
}

func (f *fakeIdentity) DeleteSession(_ context.Context, token string) error {
	delete(f.sessions, token)
	return nil
}

func (f *fakeIdentity) PrincipalByAPIKey(context.Context, string) (string, string, []string, error) {
	return "", "", nil, store.ErrNoAPIKey
}

func (f *fakeIdentity) ScopesForUser(context.Context, string) ([]string, error) {
	return f.scopes, nil
}

func newAuthHandler(t *testing.T) (http.Handler, *fakeIdentity) {
	t.Helper()
	identity := newFakeIdentity(t)
	h, err := api.New(api.Options{
		BasePath: "/",
		Version:  "test",
		Identity: identity,
		Now:      time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, identity
}

func TestLoginSetsCookieAndRendersFleetPage(t *testing.T) {
	h, _ := newAuthHandler(t)

	form := url.Values{"email": {"owner@delphi.local"}, "password": {"correct horse"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "delphi_session" || !cookies[0].HttpOnly {
		t.Fatalf("unexpected cookie: %+v", cookies)
	}

	page := httptest.NewRequest(http.MethodGet, "/", nil)
	page.AddCookie(cookies[0])
	pageRec := httptest.NewRecorder()
	h.ServeHTTP(pageRec, page)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("page status = %d", pageRec.Code)
	}
	if !strings.Contains(pageRec.Body.String(), "owner@delphi.local") {
		t.Fatalf("page does not show the signed-in user: %s", pageRec.Body.String())
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	h, _ := newAuthHandler(t)
	form := url.Values{"email": {"owner@delphi.local"}, "password": {"wrong"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginRefusesCrossSite(t *testing.T) {
	h, _ := newAuthHandler(t)
	form := url.Values{"email": {"owner@delphi.local"}, "password": {"correct horse"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAnonymousPageShowsLogin(t *testing.T) {
	h, _ := newAuthHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), "Sign in") {
		t.Fatalf("anonymous page missing login: %s", rec.Body.String())
	}
}

func TestAPIKeyCreationRequiresUserPrincipal(t *testing.T) {
	h, _ := newAuthHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", strings.NewReader(`{"name":"cli"}`))
	req.Header.Set("Authorization", "Bearer unknown")
	h.ServeHTTP(httptest.NewRecorder(), req)
	// The request is unauthenticated: 401 before reaching the key service.
	rec := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", strings.NewReader(`{"name":"cli"}`))
	h.ServeHTTP(rec, req2)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
