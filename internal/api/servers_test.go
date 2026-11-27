package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

type fakeServerStore struct {
	mu        sync.Mutex
	servers   []store.Server
	next      string
	lastLimit int
	lastAfter string
}

func (f *fakeServerStore) CreateServer(_ context.Context, s store.Server) (store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s.OrgID = "default"
	s.DesiredRevision = 1
	f.servers = append(f.servers, s)
	return s, nil
}

func (f *fakeServerStore) GetServer(_ context.Context, id string) (store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.servers {
		if s.ID == id {
			return s, nil
		}
	}
	return store.Server{}, store.ErrNoServer
}

func (f *fakeServerStore) ListServers(_ context.Context, limit int, after string) ([]store.Server, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastLimit, f.lastAfter = limit, after
	return f.servers, f.next, nil
}

func (f *fakeServerStore) ListServersByNode(_ context.Context, nodeID string) ([]store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Server
	for _, s := range f.servers {
		if s.NodeID == nodeID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeServerStore) SetDesiredState(_ context.Context, id, desired string, kill bool) (store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].ID == id {
			f.servers[i].DesiredState = desired
			f.servers[i].KillRequested = kill
			f.servers[i].DesiredRevision++
			return f.servers[i], nil
		}
	}
	return store.Server{}, store.ErrNoServer
}

func (f *fakeServerStore) ReportServerState(context.Context, string, string, string, int64, string) error {
	return nil
}

func (f *fakeServerStore) RenameServer(_ context.Context, id, name string) (store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].ID == id {
			f.servers[i].Name = name
			return f.servers[i], nil
		}
	}
	return store.Server{}, store.ErrNoServer
}

func (f *fakeServerStore) SoftDeleteServer(_ context.Context, id string) (store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.servers {
		if f.servers[i].ID == id {
			now := time.Now()
			f.servers[i].DeletedAt = &now
			f.servers[i].DesiredState = "stopped"
			f.servers[i].DesiredRevision++
			return f.servers[i], nil
		}
	}
	return store.Server{}, store.ErrNoServer
}

func (f *fakeServerStore) Audit(context.Context, string, string, string, string, string, any) error {
	return nil
}

func (f *fakeServerStore) AllocatePort(context.Context, string) (int, error) { return 25570, nil }

func newServerAPI(t *testing.T) (http.Handler, *fakeServerStore) {
	t.Helper()
	fs := &fakeServerStore{}
	svc := &capability.Servers{Store: fs}
	h, err := api.New(api.Options{
		BasePath:   "/",
		Version:    "test",
		AdminToken: "secret",
		Services:   &capability.Services{Servers: svc},
		Now:        time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, fs
}

func serverRequest(h http.Handler, method, path string, body any, auth bool) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if auth {
		req.Header.Set("Authorization", "Bearer secret")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func validServerBody() map[string]any {
	return map[string]any{
		"name":        "survival-1",
		"node_id":     "n_1",
		"image":       "delphi/java:21-temurin",
		"memory_mb":   1024,
		"jar_url":     "https://example.invalid/paper.jar",
		"jar_sha256":  strings.Repeat("a", 64),
		"accept_eula": true,
	}
}

func TestCreateServer(t *testing.T) {
	h, fs := newServerAPI(t)

	rec := serverRequest(h, http.MethodPost, "/api/v1/servers", validServerBody(), true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "survival-1" || out["desired_state"] != "stopped" {
		t.Fatalf("unexpected body: %v", out)
	}
	if len(fs.servers) != 1 {
		t.Fatalf("stored %d servers", len(fs.servers))
	}
}

func TestCreateServerRequiresAuth(t *testing.T) {
	h, _ := newServerAPI(t)
	if rec := serverRequest(h, http.MethodPost, "/api/v1/servers", validServerBody(), false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateServerRejectsMissingEULA(t *testing.T) {
	h, _ := newServerAPI(t)
	body := validServerBody()
	body["accept_eula"] = false
	if rec := serverRequest(h, http.MethodPost, "/api/v1/servers", body, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPowerServer(t *testing.T) {
	h, fs := newServerAPI(t)
	rec := serverRequest(h, http.MethodPost, "/api/v1/servers", validServerBody(), true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id, _ := created["id"].(string)

	rec = serverRequest(h, http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "start"}, true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("power status = %d: %s", rec.Code, rec.Body.String())
	}
	if fs.servers[0].DesiredState != "running" || fs.servers[0].DesiredRevision != 2 {
		t.Fatalf("desired=%q rev=%d", fs.servers[0].DesiredState, fs.servers[0].DesiredRevision)
	}

	if rec := serverRequest(h, http.MethodPost, "/api/v1/servers/"+id+"/power", map[string]string{"action": "explode"}, true); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid action status = %d, want 400", rec.Code)
	}
	if rec := serverRequest(h, http.MethodPost, "/api/v1/servers/nope/power", map[string]string{"action": "start"}, true); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown server status = %d, want 404", rec.Code)
	}
}

func TestListServers(t *testing.T) {
	h, _ := newServerAPI(t)
	_ = serverRequest(h, http.MethodPost, "/api/v1/servers", validServerBody(), true)

	rec := serverRequest(h, http.MethodGet, "/api/v1/servers", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0]["name"] != "survival-1" {
		t.Fatalf("items = %v", out.Items)
	}
}

func TestListServersForwardsPagination(t *testing.T) {
	fs := &fakeServerStore{
		servers: []store.Server{{ID: "s_1", Name: "web", OrgID: "default"}},
		next:    "cursor-2",
	}
	h, err := api.New(api.Options{
		AdminToken: "secret",
		Services:   &capability.Services{Servers: &capability.Servers{Store: fs}},
		Now:        time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers?limit=7&after=cursor-1", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.NextCursor == nil || *out.NextCursor != "cursor-2" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if fs.lastLimit != 7 || fs.lastAfter != "cursor-1" {
		t.Fatalf("store got limit=%d after=%q", fs.lastLimit, fs.lastAfter)
	}
}
