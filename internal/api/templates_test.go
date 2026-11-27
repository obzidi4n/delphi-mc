package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/mc"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/template"
)

type fakeTemplateStore struct {
	rows map[string]store.Template
}

func (f *fakeTemplateStore) ListTemplates(context.Context, string, int, string) ([]store.Template, string, error) {
	out := make([]store.Template, 0, len(f.rows))
	for _, row := range f.rows {
		out = append(out, row)
	}
	return out, "", nil
}

func (f *fakeTemplateStore) TemplateByID(_ context.Context, _, id string) (store.Template, error) {
	if row, ok := f.rows[id]; ok {
		return row, nil
	}
	return store.Template{}, store.ErrNoTemplate
}

func (f *fakeTemplateStore) ImportTemplate(_ context.Context, orgID, name string, body []byte) (store.Template, error) {
	row := store.Template{ID: "t_1", OrgID: orgID, Name: name, Revision: 1, Body: body}
	f.rows["t_1"] = row
	return row, nil
}

func (f *fakeTemplateStore) UpdateTemplate(_ context.Context, orgID, id, name string, body []byte) (store.Template, error) {
	row, ok := f.rows[id]
	if !ok || row.Builtin {
		return store.Template{}, store.ErrNoTemplate
	}
	row.OrgID, row.Name, row.Body = orgID, name, body
	row.Revision++
	f.rows[id] = row
	return row, nil
}

func (f *fakeTemplateStore) Audit(context.Context, string, string, string, string, string, any) error {
	return nil
}

type fakeArtifactResolver struct{}

func (fakeArtifactResolver) Resolve(context.Context, string, string) (mc.Artifact, error) {
	return mc.Artifact{
		Project: "paper", Version: "1.21.4", Build: 100,
		JarURL: "https://example.invalid/paper.jar", JarSHA256: strings.Repeat("a", 64),
	}, nil
}

func survivalTemplateRow(t *testing.T) store.Template {
	t.Helper()
	all, err := template.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	for _, tpl := range all {
		if tpl.ID == "builtin-survival" {
			body, err := json.Marshal(tpl)
			if err != nil {
				t.Fatal(err)
			}
			return store.Template{ID: tpl.ID, Name: tpl.Name, Revision: 2, Body: body, Builtin: true}
		}
	}
	t.Fatal("no survival template")
	return store.Template{}
}

func newTemplateHandler(t *testing.T) (http.Handler, *fakeServerStore, *fakeTemplateStore) {
	t.Helper()
	row := survivalTemplateRow(t)
	templates := &fakeTemplateStore{rows: map[string]store.Template{row.ID: row}}
	servers := &fakeServerStore{}
	svc := &capability.Services{
		Servers: &capability.Servers{Store: servers},
		Templates: &capability.Templates{
			Store: templates, Servers: &capability.Servers{Store: servers},
			Resolver: fakeArtifactResolver{},
		},
	}
	h, err := api.New(api.Options{AdminToken: "secret", Services: svc, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	return h, servers, templates
}

func TestListAndGetTemplates(t *testing.T) {
	h, _, _ := newTemplateHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/templates", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "builtin-survival") {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/templates/builtin-survival", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Base  string            `json:"base"`
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Base != "paper" || !strings.Contains(got.Files["server.properties"], "motd={{server.name}}") {
		t.Fatalf("template = %+v", got)
	}
}

func TestCreateServerFromTemplate(t *testing.T) {
	h, servers, _ := newTemplateHandler(t)
	body := `{"name":"SMP One","node_id":"node-c","template":"builtin-survival","accept_eula":true,"overrides":{"port":"25601","memory_mb":"2048"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Base            string `json:"base"`
		TemplateID      string `json:"template_id"`
		TemplateVersion int    `json:"template_version"`
		Port            int    `json:"port"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Base != "paper" || got.TemplateID != "builtin-survival" || got.Port != 25601 {
		t.Fatalf("server = %+v", got)
	}
	if len(servers.servers) != 1 || servers.servers[0].Files["server.properties"] == "" {
		t.Fatalf("stored server = %+v", servers.servers)
	}
	if !strings.Contains(servers.servers[0].Files["server.properties"], "motd=SMP One") {
		t.Fatalf("files not rendered: %v", servers.servers[0].Files)
	}
}

func TestUpdateTemplateAndBuiltinReadOnly(t *testing.T) {
	h, _, templates := newTemplateHandler(t)
	body := `{"schema_version":1,"name":"Custom","base":"paper","mc_version":"1.21.4","java":21,"defaults":{"memory_mb":1024}}`
	templates.rows["t_custom"] = store.Template{ID: "t_custom", OrgID: "default", Name: "Custom", Revision: 1, Body: []byte(body)}

	updated := `{"schema_version":1,"name":"Custom Two","base":"paper","mc_version":"1.21.4","java":21,"defaults":{"memory_mb":2048}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/templates/t_custom", strings.NewReader(updated))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revision":2`) {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/templates/builtin-survival", strings.NewReader(updated))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "read-only") {
		t.Fatalf("builtin update: %d %s", rec.Code, rec.Body.String())
	}
}

func TestImportModuleRejectsInvalidTemplate(t *testing.T) {
	h, _, _ := newTemplateHandler(t)
	bad := `{"schema_version":1,"name":"Bad","base":"forge","mc_version":"latest","java":21}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/templates", strings.NewReader(bad))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	good := `{"schema_version":1,"name":"Custom","base":"paper","mc_version":"latest","java":21,"defaults":{"memory_mb":1024},"files":{"a.txt":"{{server.name}}"}}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/templates", strings.NewReader(good))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"revision":1`) {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
}
