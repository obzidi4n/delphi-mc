package agent

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"delphi.dev/delphi/internal/wire"
)

//go:embed bridge/DelphiBridge.jar
var bridgeJar []byte

const bridgeDirName = ".delphi-bridge"

// InstallBridge writes the embedded plugin jar when it changed. It is called
// before every start, so a server gains or updates the bridge on boot.
func InstallBridge(serverDir string) error {
	plugins := filepath.Join(serverDir, "plugins")
	if err := os.MkdirAll(plugins, 0o755); err != nil {
		return fmt.Errorf("create plugins dir: %w", err)
	}
	target := filepath.Join(plugins, "DelphiBridge.jar")
	existing, err := os.ReadFile(target)
	if err == nil && bytes.Equal(existing, bridgeJar) {
		return nil
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, bridgeJar, 0o644); err != nil {
		return fmt.Errorf("write bridge jar: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("install bridge jar: %w", err)
	}
	return nil
}

// Bridge polls the plugin's state file and forwards events, and writes
// moderation commands for the plugin to consume.
type Bridge struct {
	DataDir  string
	Log      *slog.Logger
	Send     func(any)
	Interval time.Duration

	mu    sync.Mutex
	polls map[string]*bridgePoller
}

type bridgePoller struct {
	id      string
	dir     string
	cancel  context.CancelFunc
	lastSeq int64
	lastKey string
}

// NewBridge returns a bridge manager rooted at the agent's data dir.
func NewBridge(dataDir string, send func(any), log *slog.Logger) *Bridge {
	return &Bridge{DataDir: dataDir, Send: send, Log: log, Interval: 500 * time.Millisecond, polls: map[string]*bridgePoller{}}
}

// Sync starts or stops polling from an observed server state.
func (b *Bridge) Sync(serverID, state string) {
	if state == "running" {
		b.Start(serverID)
		return
	}
	b.Stop(serverID)
}

// Start begins polling one server's bridge state.
func (b *Bridge) Start(serverID string) {
	if b == nil || serverID == "" {
		return
	}
	b.mu.Lock()
	if _, ok := b.polls[serverID]; ok {
		b.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &bridgePoller{
		id: serverID, dir: filepath.Join(b.DataDir, "servers", serverID),
		cancel: cancel,
	}
	b.polls[serverID] = p
	b.mu.Unlock()
	go b.poll(ctx, p)
}

// Stop ends polling.
func (b *Bridge) Stop(serverID string) {
	b.mu.Lock()
	p := b.polls[serverID]
	delete(b.polls, serverID)
	b.mu.Unlock()
	if p != nil {
		p.cancel()
	}
}

// Close stops every poller.
func (b *Bridge) Close() {
	b.mu.Lock()
	polls := b.polls
	b.polls = map[string]*bridgePoller{}
	b.mu.Unlock()
	for _, p := range polls {
		p.cancel()
	}
}

// Command writes a moderation command for the plugin.
func (b *Bridge) Command(cmd wire.BridgeCommandFrame) error {
	if cmd.ServerID == "" || cmd.ID == "" {
		return errors.New("bridge command needs a server and id")
	}
	switch cmd.Action {
	case "kick", "ban":
		if strings.TrimSpace(cmd.Player) == "" || len(cmd.Player) > 64 {
			return errors.New("player must be 1-64 characters")
		}
	case "command":
		if strings.TrimSpace(cmd.Command) == "" || len(cmd.Command) > 512 {
			return errors.New("command must be 1-512 characters")
		}
	default:
		return fmt.Errorf("unsupported bridge action %q", cmd.Action)
	}
	if len(cmd.Reason) > 256 {
		return errors.New("reason must be at most 256 characters")
	}
	b.mu.Lock()
	_, live := b.polls[cmd.ServerID]
	b.mu.Unlock()
	if !live {
		return errors.New("the server is not running")
	}
	dir := filepath.Join(b.DataDir, "servers", cmd.ServerID, bridgeDirName, "cmd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{
		"id": cmd.ID, "action": cmd.Action, "player": cmd.Player,
		"reason": cmd.Reason, "command": cmd.Command,
	})
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "cmd-"+cmd.ID+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

func (b *Bridge) poll(ctx context.Context, p *bridgePoller) {
	ticker := time.NewTicker(b.Interval)
	defer ticker.Stop()
	for {
		b.readState(p)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (b *Bridge) readState(p *bridgePoller) {
	path := filepath.Join(p.dir, bridgeDirName, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state struct {
		Seq      int64               `json:"seq"`
		TPS      float64             `json:"tps"`
		Players  []wire.BridgePlayer `json:"players"`
		Commands []string            `json:"commands"`
		Events   []struct {
			Seq     int64  `json:"seq"`
			Type    string `json:"type"`
			Player  string `json:"player"`
			Message string `json:"message"`
			Action  string `json:"action"`
			OK      bool   `json:"ok"`
			Error   string `json:"error"`
			ID      string `json:"id"`
		} `json:"events"`
		At int64 `json:"at"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		if b.Log != nil {
			b.Log.Debug("bridge state unreadable", "server", p.id, "err", err)
		}
		return
	}
	key := fmt.Sprintf("%.2f|%d|%d", state.TPS, len(state.Players), len(state.Commands))
	for _, pl := range state.Players {
		key += "|" + pl.Name
	}
	if key != p.lastKey && b.Send != nil {
		p.lastKey = key
		updated := time.UnixMilli(state.At)
		if state.At == 0 {
			updated = time.Now().UTC()
		}
		b.Send(wire.BridgeStateFrame{
			Type: wire.TypeBridgeState, ServerID: p.id, TPS: state.TPS,
			Players: state.Players, Commands: state.Commands, Updated: updated.UTC(),
		})
	}
	for _, event := range state.Events {
		if event.Seq <= p.lastSeq {
			continue
		}
		if p.lastSeq == 0 {
			// First read: adopt the plugin's history without replaying it.
			p.lastSeq = event.Seq
			continue
		}
		p.lastSeq = event.Seq
		if b.Send == nil {
			continue
		}
		b.Send(wire.BridgeEventFrame{
			Type: wire.TypeBridgeEvent, ServerID: p.id,
			Event: wire.BridgeEvent{
				Seq: event.Seq, Kind: event.Type, Player: event.Player,
				Message: event.Message, Action: event.Action, OK: event.OK,
				Error: event.Error, ID: event.ID,
			},
		})
	}
}
