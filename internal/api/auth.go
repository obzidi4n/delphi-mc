package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

const (
	sessionCookie = "delphi_session"
	sessionTTL    = 30 * 24 * time.Hour
	defaultOrg    = "default"
)

// IdentityStore resolves users, sessions, and API keys.
type IdentityStore interface {
	UserByEmail(ctx context.Context, orgID, email string) (store.User, error)
	CreateSession(ctx context.Context, userID string, ttl time.Duration) (string, time.Time, error)
	UserBySession(ctx context.Context, token string) (store.User, error)
	DeleteSession(ctx context.Context, token string) error
	PrincipalByAPIKey(ctx context.Context, token string) (userID, orgID string, scopes []string, err error)
	ScopesForUser(ctx context.Context, userID string) ([]string, error)
}

// principalFrom resolves a bearer key/token or a session cookie.
func (s *server) principalFrom(r *http.Request) (capability.Principal, bool) {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		token := strings.TrimPrefix(header, "Bearer ")
		if s.opts.AdminToken != "" && constantTimeEqual(token, s.opts.AdminToken) {
			return capability.Principal{
				OrgID: defaultOrg, Via: "api", Scopes: []string{"*"},
			}, true
		}
		if s.opts.Identity != nil {
			userID, orgID, scopes, err := s.opts.Identity.PrincipalByAPIKey(r.Context(), token)
			if err == nil {
				return capability.Principal{UserID: userID, OrgID: orgID, Via: "api", Scopes: scopes}, true
			}
		}
		return capability.Principal{}, false
	}

	if cookie, err := r.Cookie(sessionCookie); err == nil && s.opts.Identity != nil {
		user, err := s.opts.Identity.UserBySession(r.Context(), cookie.Value)
		if err == nil {
			scopes, _ := s.opts.Identity.ScopesForUser(r.Context(), user.ID)
			return capability.Principal{UserID: user.ID, OrgID: user.OrgID, Via: "ui", Scopes: scopes}, true
		}
	}
	return capability.Principal{}, false
}

// authenticate requires a principal, writing 401 when absent.
func (s *server) authenticate(w http.ResponseWriter, r *http.Request) (capability.Principal, bool) {
	p, ok := s.principalFrom(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return capability.Principal{}, false
	}
	if p.Via == "ui" && r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		writeProblem(w, http.StatusForbidden, "csrf", "cross-site request refused")
		return capability.Principal{}, false
	}
	return p, true
}

func sameOrigin(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.Host == r.Host
	}
	return r.Header.Get("Sec-Fetch-Site") != "cross-site"
}

// currentUser returns the session user for page rendering.
func (s *server) currentUser(r *http.Request) (store.User, bool) {
	if s.opts.Identity == nil {
		return store.User{}, false
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, false
	}
	user, err := s.opts.Identity.UserBySession(r.Context(), cookie.Value)
	if err != nil {
		return store.User{}, false
	}
	return user, true
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.opts.Identity == nil {
		http.Error(w, "authentication is not configured", http.StatusNotImplemented)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, r, "Invalid form.", "")
		return
	}
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")

	user, err := s.opts.Identity.UserByEmail(r.Context(), defaultOrg, email)
	if err != nil || !auth.VerifyPassword(user.PasswordHash, password) {
		s.renderLogin(w, r, "Wrong email or password.", email)
		return
	}
	token, _, err := s.opts.Identity.CreateSession(r.Context(), user.ID, sessionTTL)
	if err != nil {
		http.Error(w, "could not create a session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     s.base,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL / time.Second),
	})
	http.Redirect(w, r, s.base, http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if s.opts.Identity != nil {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			_ = s.opts.Identity.DeleteSession(r.Context(), cookie.Value)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     s.base,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, s.base, http.StatusSeeOther)
}

func (s *server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.Services == nil || s.opts.Services.Keys == nil {
		writeProblem(w, http.StatusNotImplemented, "keys_disabled", "the key service is not configured")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	token, key, err := s.opts.Services.Keys.Create(r.Context(), principal, body.Name)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apitypes.APIKeyCreatedDTO{
		ID: key.ID, Name: key.Name, Token: token, Scopes: key.Scopes,
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(v)
}
