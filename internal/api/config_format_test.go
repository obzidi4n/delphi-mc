package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/apitypes"
)

func postConfigFormat(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/format", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestConfigFormatJSON(t *testing.T) {
	h, err := api.New(api.Options{AdminToken: "secret", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	rec := postConfigFormat(t, h, `{"path":"server.json","content":"{\"a\":1}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got apitypes.ConfigFormatDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Valid || got.Format != "json" || !strings.Contains(got.Formatted, "\n  \"a\": 1\n") {
		t.Fatalf("result = %+v", got)
	}
}

func TestConfigFormatInvalidYAML(t *testing.T) {
	h, err := api.New(api.Options{AdminToken: "secret", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	rec := postConfigFormat(t, h, `{"path":"server.yml","content":"a: [unterminated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got apitypes.ConfigFormatDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Valid || got.Error == "" || got.Format != "yaml" {
		t.Fatalf("result = %+v", got)
	}
}

func TestConfigFormatRejectsUnknownExtension(t *testing.T) {
	h, err := api.New(api.Options{AdminToken: "secret", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	rec := postConfigFormat(t, h, `{"path":"server.ini","content":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConfigFormatServerPropertiesUsesVersion(t *testing.T) {
	h, err := api.New(api.Options{AdminToken: "secret", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	rec := postConfigFormat(t, h, `{"path":"server.properties","content":"pvp=true\nview-distance=99\n","version":"1.21.11"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got apitypes.ConfigFormatDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Valid || got.Format != "properties" || got.Error == "" {
		t.Fatalf("result = %+v", got)
	}
	if got.Warning == nil || !strings.Contains(*got.Warning, "removed") {
		t.Fatalf("warning = %v", got.Warning)
	}
}

func TestConfigFormatRequiresAuth(t *testing.T) {
	h, err := api.New(api.Options{AdminToken: "secret", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/config/format", strings.NewReader(`{"path":"a.json","content":"{}"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}
