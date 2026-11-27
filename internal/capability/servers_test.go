package capability

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/store"
)

type fakeStore struct {
	created  store.Server
	servers  []store.Server
	err      error
	desired  string
	kill     bool
	syncNode string
	audits   []string
}

func (f *fakeStore) CreateServer(_ context.Context, s store.Server) (store.Server, error) {
	if f.err != nil {
		return store.Server{}, f.err
	}
	s.OrgID = "default"
	s.DesiredRevision = 1
	f.created = s
	f.servers = append(f.servers, s)
	return s, nil
}

func (f *fakeStore) GetServer(_ context.Context, id string) (store.Server, error) {
	for _, s := range f.servers {
		if s.ID == id {
			return s, nil
		}
	}
	return store.Server{}, store.ErrNoServer
}

func (f *fakeStore) ListServers(context.Context, int, string) ([]store.Server, string, error) {
	return f.servers, "", nil
}

func (f *fakeStore) ListServersByNode(_ context.Context, nodeID string) ([]store.Server, error) {
	var out []store.Server
	for _, s := range f.servers {
		if s.NodeID == nodeID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) SetDesiredState(_ context.Context, id, desired string, kill bool) (store.Server, error) {
	for i := range f.servers {
		if f.servers[i].ID == id {
			f.desired, f.kill = desired, kill
			f.servers[i].DesiredState = desired
			f.servers[i].KillRequested = kill
			f.servers[i].DesiredRevision++
			return f.servers[i], nil
		}
	}
	return store.Server{}, store.ErrNoServer
}

func (f *fakeStore) ReportServerState(context.Context, string, string, string, int64, string) error {
	return nil
}

func (f *fakeStore) RenameServer(_ context.Context, id, name string) (store.Server, error) {
	for i := range f.servers {
		if f.servers[i].ID == id {
			f.servers[i].Name = name
			return f.servers[i], nil
		}
	}
	return store.Server{}, store.ErrNoServer
}

func (f *fakeStore) SoftDeleteServer(_ context.Context, id string) (store.Server, error) {
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

func (f *fakeStore) Audit(_ context.Context, _, _, _, action, target string, _ any) error {
	f.audits = append(f.audits, action+" "+target)
	return nil
}

var testPrincipal = Principal{UserID: "u_1", OrgID: "default", Via: "api", Scopes: []string{"*"}}

type fakeNotifier struct{ synced []string }

func (f *fakeNotifier) Sync(_ context.Context, nodeID string) error {
	f.synced = append(f.synced, nodeID)
	return nil
}

func validRequest() CreateRequest {
	return CreateRequest{
		Name:       "survival-1",
		NodeID:     "n_1",
		Image:      "delphi/java:21-temurin",
		MemoryMB:   1024,
		CPUCores:   1,
		JarURL:     "https://example.invalid/paper.jar",
		JarSHA256:  strings.Repeat("a", 64),
		AcceptEULA: true,
	}
}

func TestCreateValidatesAndSyncs(t *testing.T) {
	fs := &fakeStore{}
	fn := &fakeNotifier{}
	svc := &Servers{Store: fs, Notify: fn}

	created, err := svc.Create(context.Background(), testPrincipal, validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, "s_") || created.DesiredState != "stopped" {
		t.Fatalf("created = %+v", created)
	}
	if len(fn.synced) != 1 || fn.synced[0] != "n_1" {
		t.Fatalf("sync = %v", fn.synced)
	}
}

func TestCreateRejectsInvalid(t *testing.T) {
	svc := &Servers{Store: &fakeStore{}}

	cases := map[string]func(*CreateRequest){
		"missing name":    func(r *CreateRequest) { r.Name = "  " },
		"missing node":    func(r *CreateRequest) { r.NodeID = "" },
		"missing image":   func(r *CreateRequest) { r.Image = "" },
		"small memory":    func(r *CreateRequest) { r.MemoryMB = 128 },
		"huge memory":     func(r *CreateRequest) { r.MemoryMB = 65536 },
		"http jar":        func(r *CreateRequest) { r.JarURL = "http://example.invalid/paper.jar" },
		"bad sha":         func(r *CreateRequest) { r.JarSHA256 = "abc" },
		"eula not agreed": func(r *CreateRequest) { r.AcceptEULA = false },
		"bad port":        func(r *CreateRequest) { r.Port = 80 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := validRequest()
			mutate(&req)
			_, err := svc.Create(context.Background(), testPrincipal, req)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestCreateRequiresPrincipalVia(t *testing.T) {
	if _, err := (&Servers{Store: &fakeStore{}}).Create(context.Background(), Principal{}, validRequest()); err == nil {
		t.Fatal("expected an error without Via")
	}
}

func TestCreateRequiresScope(t *testing.T) {
	reader := Principal{UserID: "u_1", OrgID: "default", Via: "api", Scopes: []string{"org:default:read"}}
	if _, err := (&Servers{Store: &fakeStore{}}).Create(context.Background(), reader, validRequest()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestAuditRecordsMutations(t *testing.T) {
	fs := &fakeStore{}
	svc := &Servers{Store: fs}
	created, err := svc.Create(context.Background(), testPrincipal, validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Power(context.Background(), testPrincipal, created.ID, "start"); err != nil {
		t.Fatal(err)
	}
	if len(fs.audits) != 2 ||
		fs.audits[0] != "server.create "+created.ID ||
		fs.audits[1] != "server.power "+created.ID {
		t.Fatalf("audits = %v", fs.audits)
	}
}

func TestPowerMapping(t *testing.T) {
	for _, tc := range []struct {
		action string
		want   string
		kill   bool
	}{
		{"start", "running", false},
		{"restart", "running", false},
		{"stop", "stopped", false},
		{"kill", "stopped", true},
	} {
		t.Run(tc.action, func(t *testing.T) {
			fs := &fakeStore{servers: []store.Server{{ID: "s_1", NodeID: "n_1"}}}
			svc := &Servers{Store: fs}
			if _, err := svc.Power(context.Background(), testPrincipal, "s_1", tc.action); err != nil {
				t.Fatal(err)
			}
			if fs.desired != tc.want || fs.kill != tc.kill {
				t.Fatalf("desired=%q kill=%v, want %q %v", fs.desired, fs.kill, tc.want, tc.kill)
			}
		})
	}
}

func TestPowerRejectsUnknownAction(t *testing.T) {
	svc := &Servers{Store: &fakeStore{servers: []store.Server{{ID: "s_1", NodeID: "n_1"}}}}
	if _, err := svc.Power(context.Background(), testPrincipal, "s_1", "explode"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}
