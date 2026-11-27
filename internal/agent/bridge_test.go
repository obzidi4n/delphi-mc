package agent

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"delphi.dev/delphi/internal/wire"
)

func TestInstallBridgeWritesPlugin(t *testing.T) {
	dir := t.TempDir()
	if err := InstallBridge(dir); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "plugins", "DelphiBridge.jar")
	info, err := os.Stat(target)
	if err != nil || info.Size() == 0 {
		t.Fatalf("plugin not installed: %v", err)
	}
	if err := InstallBridge(dir); err != nil {
		t.Fatalf("second install: %v", err)
	}
}

func TestBridgeForwardsStateAndNewEvents(t *testing.T) {
	data := t.TempDir()
	serverID := "s_1"
	bridgeDir := filepath.Join(data, "servers", serverID, ".delphi-bridge")
	if err := os.MkdirAll(bridgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	frames := make(chan any, 16)
	b := NewBridge(data, func(v any) { frames <- v }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Interval = 10 * time.Millisecond
	b.Start(serverID)
	defer b.Stop(serverID)

	write := func(seq int, events ...map[string]any) {
		state := map[string]any{
			"version": 1, "seq": seq, "tps": 19.5,
			"players":  []map[string]string{{"name": "Steve", "uuid": "u"}},
			"commands": []string{"help", "say"},
			"events":   events,
			"at":       time.Now().UnixMilli(),
		}
		raw, _ := json.Marshal(state)
		tmp := filepath.Join(bridgeDir, "state.json.tmp")
		if err := os.WriteFile(tmp, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, filepath.Join(bridgeDir, "state.json")); err != nil {
			t.Fatal(err)
		}
	}
	write(1, map[string]any{"seq": 1, "type": "ready", "ok": true})

	deadline := time.After(2 * time.Second)
	var sawState bool
	for !sawState {
		select {
		case frame := <-frames:
			if st, ok := frame.(wire.BridgeStateFrame); ok {
				if st.TPS != 19.5 || len(st.Players) != 1 || st.Players[0].Name != "Steve" || len(st.Commands) != 2 {
					t.Fatalf("state = %+v", st)
				}
				sawState = true
			}
		case <-deadline:
			t.Fatal("no bridge state frame")
		}
	}

	write(2,
		map[string]any{"seq": 1, "type": "ready", "ok": true},
		map[string]any{"seq": 2, "type": "chat", "player": "Steve", "message": "hello"},
	)
	deadline = time.After(2 * time.Second)
	for {
		select {
		case frame := <-frames:
			ev, ok := frame.(wire.BridgeEventFrame)
			if !ok {
				continue
			}
			if ev.Event.Seq != 2 || ev.Event.Kind != "chat" || ev.Event.Message != "hello" {
				t.Fatalf("event = %+v", ev.Event)
			}
			return
		case <-deadline:
			t.Fatal("chat event was not forwarded")
		}
	}
}

func TestBridgeCommandWritesFile(t *testing.T) {
	data := t.TempDir()
	serverID := "s_1"
	if err := os.MkdirAll(filepath.Join(data, "servers", serverID), 0o755); err != nil {
		t.Fatal(err)
	}
	b := NewBridge(data, func(any) {}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.Start(serverID) // the poller must be live for commands
	defer b.Stop(serverID)

	err := b.Command(wire.BridgeCommandFrame{
		Type: wire.TypeBridgeCommand, ServerID: serverID, ID: "c_1",
		Action: "ban", Player: "Griefer", Reason: "griefing",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "servers", serverID, ".delphi-bridge", "cmd", "cmd-c_1.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cmd map[string]string
	if err := json.Unmarshal(raw, &cmd); err != nil {
		t.Fatal(err)
	}
	if cmd["action"] != "ban" || cmd["player"] != "Griefer" || cmd["reason"] != "griefing" {
		t.Fatalf("cmd = %v", cmd)
	}
	if err := b.Command(wire.BridgeCommandFrame{Type: wire.TypeBridgeCommand, ServerID: serverID, ID: "c_2", Action: "op", Player: "x"}); err == nil {
		t.Fatal("unsupported action was accepted")
	}
	b.Stop(serverID)
	if err := b.Command(wire.BridgeCommandFrame{Type: wire.TypeBridgeCommand, ServerID: serverID, ID: "c_3", Action: "kick", Player: "x"}); err == nil {
		t.Fatal("command accepted while stopped")
	}
}
