package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"delphi.dev/delphi/internal/api"
)

func newHandler(t *testing.T, base string, ready func(context.Context) error) http.Handler {
	t.Helper()
	h, err := api.New(api.Options{BasePath: base, Version: "test", Ready: ready})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthz(t *testing.T) {
	rec := get(t, newHandler(t, "/", nil), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] != "test" {
		t.Fatalf("unexpected body: %v", body)
	}
}

func TestReadyzReflectsDatabase(t *testing.T) {
	down := func(context.Context) error { return errors.New("nope") }
	rec := get(t, newHandler(t, "/", down), "/readyz")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	up := func(context.Context) error { return nil }
	rec = get(t, newHandler(t, "/", up), "/readyz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestIndexServedAtBasePath(t *testing.T) {
	rec := get(t, newHandler(t, "/", nil), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "DelphiMC") {
		t.Fatalf("body does not look like the page: %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

func TestSubpathRouting(t *testing.T) {
	h := newHandler(t, "/delphi/", nil)

	if rec := get(t, h, "/delphi/"); rec.Code != http.StatusOK {
		t.Fatalf("base: status = %d, want 200", rec.Code)
	}
	if rec := get(t, h, "/delphi"); rec.Code < 300 || rec.Code >= 400 {
		t.Fatalf("bare base: status = %d, want a redirect", rec.Code)
	}
	if loc := get(t, h, "/delphi").Header().Get("Location"); loc != "/delphi/" {
		t.Fatalf("redirect to %q, want /delphi/", loc)
	}
	if rec := get(t, h, "/delphi/other"); rec.Code != http.StatusNotFound {
		t.Fatalf("subtree: status = %d, want 404", rec.Code)
	}
	if rec := get(t, h, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("health under subpath: status = %d, want 200", rec.Code)
	}
	if rec := get(t, h, "/delphi/api/v1/nodes"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api under subpath: status = %d, want 401", rec.Code)
	}
	if rec := get(t, h, "/api/v1/nodes"); rec.Code != http.StatusNotFound {
		t.Fatalf("unprefixed api: status = %d, want 404", rec.Code)
	}
}

func TestUnknownPathIsNotFound(t *testing.T) {
	if rec := get(t, newHandler(t, "/", nil), "/nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
