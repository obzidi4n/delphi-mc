package capability

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"delphi.dev/delphi/internal/mc"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/template"
)

type fakeTemplateStore struct {
	rows     map[string]store.Template
	imported []byte
	audits   []string
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
	f.imported = body
	row := store.Template{ID: "t_new", OrgID: orgID, Name: name, Revision: 1, Body: body}
	f.rows["t_new"] = row
	return row, nil
}

func (f *fakeTemplateStore) Audit(_ context.Context, _, _, _, action, target string, _ any) error {
	f.audits = append(f.audits, action+" "+target)
	return nil
}

type fakeResolver struct {
	artifact mc.Artifact
	err      error
}

func (f fakeResolver) Resolve(context.Context, string, string) (mc.Artifact, error) {
	return f.artifact, f.err
}

type fakeCreator struct {
	req   CreateRequest
	calls int
}

func (f *fakeCreator) Create(_ context.Context, _ Principal, req CreateRequest) (store.Server, error) {
	f.calls++
	f.req = req
	return store.Server{ID: "s_new", Name: req.Name, Base: req.Base, Files: req.Files}, nil
}

type fakePorts struct{ port int }

func (f fakePorts) AllocatePort(context.Context, string) (int, error) { return f.port, nil }

func survivalRow(t *testing.T, revision int) store.Template {
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
			return store.Template{ID: tpl.ID, Name: tpl.Name, Revision: revision, Body: body, Builtin: true}
		}
	}
	t.Fatal("no survival builtin")
	return store.Template{}
}

func TestInstantiateRendersTemplate(t *testing.T) {
	row := survivalRow(t, 3)
	creator := &fakeCreator{}
	svc := &Templates{
		Store:   &fakeTemplateStore{rows: map[string]store.Template{row.ID: row}},
		Servers: creator,
		Ports:   fakePorts{port: 25570},
		Resolver: fakeResolver{artifact: mc.Artifact{
			Project: "paper", Version: "1.21.4", Build: 100,
			JarURL: "https://example.invalid/paper.jar", JarSHA256: strings.Repeat("a", 64),
		}},
	}
	owner := Principal{UserID: "u_1", OrgID: "default", Via: "api", Scopes: []string{"*"}}
	_, err := svc.CreateServer(context.Background(), owner, InstantiateRequest{
		Name: "SMP One", NodeID: "node-c", TemplateID: row.ID,
		Overrides:  map[string]string{"memory_mb": "2048", "port": "25601"},
		AcceptEULA: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := creator.req
	if req.Base != "paper" || req.TemplateID != "builtin-survival" || req.TemplateVersion != 3 {
		t.Fatalf("request = %+v", req)
	}
	if req.MemoryMB != 2048 || req.Port != 25601 || req.JavaVersion != 21 {
		t.Fatalf("sizing = %+v", req)
	}
	if req.Image != "docker.io/library/eclipse-temurin:21-jre" {
		t.Fatalf("image = %q", req.Image)
	}
	if !strings.Contains(req.Files["server.properties"], "motd=SMP One") ||
		!strings.Contains(req.Files["server.properties"], "server-port=25601") {
		t.Fatalf("files = %v", req.Files)
	}
	if req.JarURL != "https://example.invalid/paper.jar" || req.Variables["template.mc_version"] != "1.21.4" {
		t.Fatalf("artifact facts = %+v / %v", req, req.Variables)
	}
}

func TestInstantiateAllocatesPort(t *testing.T) {
	row := survivalRow(t, 1)
	creator := &fakeCreator{}
	svc := &Templates{
		Store:    &fakeTemplateStore{rows: map[string]store.Template{row.ID: row}},
		Servers:  creator,
		Ports:    fakePorts{port: 25571},
		Resolver: fakeResolver{artifact: mc.Artifact{Version: "1.21.4", JarURL: "https://x/y.jar", JarSHA256: strings.Repeat("b", 64)}},
	}
	principal := Principal{UserID: "u", OrgID: "default", Via: "api",
		Scopes: []string{"org:default:read", "org:default:server:create"}}
	if _, err := svc.CreateServer(context.Background(), principal, InstantiateRequest{
		Name: "auto", NodeID: "node-c", TemplateID: row.ID, AcceptEULA: true,
	}); err != nil {
		t.Fatal(err)
	}
	if creator.req.Port != 25571 || !strings.Contains(creator.req.Files["server.properties"], "server-port=25571") {
		t.Fatalf("port not allocated: %+v", creator.req)
	}
}

func TestInstantiateRejectsUnknownOverrideAndBadScope(t *testing.T) {
	row := survivalRow(t, 1)
	svc := &Templates{
		Store:    &fakeTemplateStore{rows: map[string]store.Template{row.ID: row}},
		Servers:  &fakeCreator{},
		Resolver: fakeResolver{artifact: mc.Artifact{Version: "1.21.4"}},
	}
	owner := Principal{OrgID: "default", Via: "api", Scopes: []string{"*"}}
	if _, err := svc.CreateServer(context.Background(), owner, InstantiateRequest{
		Name: "x", NodeID: "n", TemplateID: row.ID, Overrides: map[string]string{"nope": "1"},
	}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	reader := Principal{OrgID: "default", Via: "api", Scopes: []string{"org:default:read"}}
	if _, err := svc.CreateServer(context.Background(), reader, InstantiateRequest{
		Name: "x", NodeID: "n", TemplateID: row.ID,
	}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func TestImportValidatesAndAudits(t *testing.T) {
	storeFake := &fakeTemplateStore{rows: map[string]store.Template{}}
	svc := &Templates{Store: storeFake}
	admin := Principal{UserID: "u", OrgID: "default", Via: "ui", Scopes: []string{"org:default:admin"}}
	body := []byte(`{"schema_version":1,"name":"Custom","base":"paper","mc_version":"latest","java":21,"defaults":{"memory_mb":1024}}`)
	if _, _, err := svc.Import(context.Background(), admin, body); err != nil {
		t.Fatal(err)
	}
	if len(storeFake.imported) == 0 || len(storeFake.audits) != 1 || !strings.HasPrefix(storeFake.audits[0], "template.import") {
		t.Fatalf("imported=%d audits=%v", len(storeFake.imported), storeFake.audits)
	}
	if _, _, err := svc.Import(context.Background(), admin, []byte(`{"schema_version":1,"name":"Bad","base":"forge","mc_version":"latest","java":21}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	viewer := Principal{OrgID: "default", Via: "ui", Scopes: []string{"org:default:read"}}
	if _, _, err := svc.Import(context.Background(), viewer, body); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}
