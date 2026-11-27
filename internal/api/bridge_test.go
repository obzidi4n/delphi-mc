package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

func TestGetServerBridgeReturnsSnapshot(t *testing.T) {
	console := api.NewConsoleHub()
	console.PublishBridgeState(wire.BridgeStateFrame{
		ServerID: "s_1", TPS: 19.75,
		Players:  []wire.BridgePlayer{{Name: "Steve", UUID: "u1"}},
		Commands: []string{"help", "say"},
		Updated:  time.Now().UTC(),
	})
	h, err := api.New(api.Options{
		AdminToken: "secret",
		Console:    console,
		Services: &capability.Services{Servers: &capability.Servers{Store: &fakeServerStore{
			servers: []store.Server{{ID: "s_1", Name: "SMP", NodeID: "node-c"}},
		}}},
		Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/servers/s_1/bridge", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		TPS      float64 `json:"tps"`
		Players  []struct{ Name string }
		Commands []string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.TPS != 19.75 || len(body.Players) != 1 || body.Players[0].Name != "Steve" || len(body.Commands) != 2 {
		t.Fatalf("body = %+v", body)
	}
}

func TestModerationSendsBridgeCommand(t *testing.T) {
	var sent *wire.BridgeCommandFrame
	h, err := api.New(api.Options{
		AdminToken: "secret",
		Services: &capability.Services{Servers: &capability.Servers{Store: &fakeServerStore{
			servers: []store.Server{{ID: "s_1", Name: "SMP", NodeID: "node-c"}},
		}}},
		BridgeSender: func(_ context.Context, nodeID string, frame wire.BridgeCommandFrame) error {
			if nodeID != "node-c" {
				t.Fatalf("nodeID = %q", nodeID)
			}
			sent = &frame
			return nil
		},
		Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"action":"ban","player":"Griefer","reason":"griefing"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/s_1/moderation", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || sent == nil {
		t.Fatalf("status = %d, sent = %+v: %s", rec.Code, sent, rec.Body.String())
	}
	if sent.Action != "ban" || sent.Player != "Griefer" || sent.Reason != "griefing" || sent.ID == "" {
		t.Fatalf("frame = %+v", sent)
	}
}

func TestCommandSendsBridgeCommand(t *testing.T) {
	console := api.NewConsoleHub()
	console.PublishBridgeState(wire.BridgeStateFrame{ServerID: "s_1", Updated: time.Now()})
	var sent *wire.BridgeCommandFrame
	h, err := api.New(api.Options{
		AdminToken: "secret",
		Console:    console,
		Services: &capability.Services{Servers: &capability.Servers{Store: &fakeServerStore{
			servers: []store.Server{{ID: "s_1", Name: "SMP", NodeID: "node-c"}},
		}}},
		BridgeSender: func(_ context.Context, nodeID string, frame wire.BridgeCommandFrame) error {
			if nodeID != "node-c" {
				t.Fatalf("nodeID = %q", nodeID)
			}
			sent = &frame
			return nil
		},
		Now: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/s_1/command", strings.NewReader(`{"command":"say hello"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || sent == nil {
		t.Fatalf("status = %d, sent = %+v: %s", rec.Code, sent, rec.Body.String())
	}
	if sent.Action != "command" || sent.Command != "say hello" || sent.ServerID != "s_1" || sent.ID == "" {
		t.Fatalf("frame = %+v", sent)
	}
}

func TestModerationNeedsScopeAndOnlineNode(t *testing.T) {
	identity := newFakeIdentity(t)
	identity.scopes = []string{"server:s_1:read"}
	h, err := api.New(api.Options{
		Identity: identity,
		Services: &capability.Services{Servers: &capability.Servers{Store: &fakeServerStore{
			servers: []store.Server{{ID: "s_1", Name: "SMP", NodeID: "node-c"}},
		}}},
		BridgeSender: func(context.Context, string, wire.BridgeCommandFrame) error { return api.ErrNodeOffline },
		Now:          time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	login := url.Values{"email": {"owner@delphi.local"}, "password": {"correct horse"}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(login.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login failed")
	}

	body := `{"action":"kick","player":"Steve"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/servers/s_1/moderation", strings.NewReader(body))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d: %s", rec.Code, rec.Body.String())
	}

	identity.scopes = []string{"server:s_1:read", "server:s_1:moderate"}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/servers/s_1/moderation", strings.NewReader(body))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("offline status = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConsoleWebSocketDeliversBridgeState(t *testing.T) {
	console := api.NewConsoleHub()
	console.PublishBridgeState(wire.BridgeStateFrame{
		ServerID: "s_1", TPS: 20, Commands: []string{"help"},
		Players: []wire.BridgePlayer{{Name: "Steve", UUID: "u1"}},
		Updated: time.Now().UTC(),
	})
	h, err := api.New(api.Options{AdminToken: "secret", Console: console, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/servers/s_1",
		&websocket.DialOptions{HTTPHeader: map[string][]string{"Authorization": {"Bearer secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for {
		readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
		typ, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			t.Fatal(err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		if frame["type"] == "bridge" && frame["tps"] == 20.0 {
			return
		}
	}
}
