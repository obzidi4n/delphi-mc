package capability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

type fakeDevCaller struct {
	node string
	req  wire.DevCloneRequest
}

func (f *fakeDevCaller) Call(_ context.Context, nodeID string, build func(requestID string) any, _ time.Duration) (json.RawMessage, error) {
	f.node = nodeID
	f.req = build("req_dev").(wire.DevCloneRequest)
	return json.Marshal(wire.DevCloneResult{Type: wire.TypeDevCloneResult, RequestID: "req_dev", OK: true})
}

func TestCreateDevClonesAndWhitelistsOwner(t *testing.T) {
	mojang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/profiles/minecraft/owner" {
			t.Errorf("unexpected mojang path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"11111111111111111111111111111111","name":"owner"}`))
	}))
	defer mojang.Close()
	fs := &fakeStore{servers: []store.Server{{
		ID: "s_prod", OrgID: "default", NodeID: "n_1", Name: "survival",
		Image: "delphi/java:21", Base: "paper", AcceptEULA: true, MemoryMB: 4096, Port: 25565,
		JarURL: "https://example.invalid/paper.jar", JarSHA256: strings.Repeat("a", 64),
		MCVersion: "1.21.11", Plugins: []store.ServerPlugin{{Project: "LuckPerms", Provider: "modrinth", Name: "luckperms.jar"}},
	}}}
	caller := &fakeDevCaller{}
	dev := &Dev{Store: fs, Servers: &Servers{Store: fs}, Hub: caller, HTTP: mojang.Client(), MojangBase: mojang.URL}
	result, err := dev.Create(context.Background(), testPrincipal, "s_prod", "owner", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Server.DevOf != "s_prod" || result.Server.Name != "survival dev" || result.Server.MemoryMB != 1024 {
		t.Fatalf("dev server = %+v", result.Server)
	}
	if result.Server.DesiredState != "running" || len(result.Server.Plugins) != 1 {
		t.Fatalf("dev lifecycle/plugins = %+v", result.Server)
	}
	if caller.node != "n_1" || caller.req.SourceServerID != "s_prod" || caller.req.TargetServerID != result.Server.ID {
		t.Fatalf("clone request = %+v (node %s)", caller.req, caller.node)
	}
	if len(caller.req.Whitelist) != 1 || caller.req.Whitelist[0].Name != "owner" ||
		caller.req.Whitelist[0].UUID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("whitelist = %+v", caller.req.Whitelist)
	}
}
