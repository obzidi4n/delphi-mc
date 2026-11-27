// Package agent is the node daemon: it enrolls once, keeps one mTLS WebSocket
// open to the panel, and reconciles the desired server set it receives.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/wire"
)

// ConnectOptions configures the channel.
type ConnectOptions struct {
	PanelURL     string
	StateDir     string
	AgentVersion string
	Interval     time.Duration
	Supervisor   *Supervisor
	// FSCapability advertises reflink, zfs, or copy for this node.
	FSCapability string
}

// Connect keeps the agent channel open until ctx is cancelled, reconnecting
// with exponential backoff.
func Connect(ctx context.Context, opts ConnectOptions, log *slog.Logger) error {
	if opts.Supervisor == nil {
		return errors.New("agent: supervisor is required")
	}
	interval := opts.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	cert, roots, err := LoadIdentity(opts.StateDir)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: tlsClientConfig(cert, roots),
	}}

	endpoint, err := socketURL(opts.PanelURL)
	if err != nil {
		return err
	}

	backoff := time.Second
	for {
		started := time.Now()
		err := session(ctx, client, endpoint, opts, interval, log)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		log.Warn("agent channel closed", "err", err, "retry_in", backoff)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func session(ctx context.Context, client *http.Client, endpoint string, opts ConnectOptions, interval time.Duration, log *slog.Logger) error {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, _, err := websocket.Dial(sctx, endpoint, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()

	writer := &connWriter{conn: conn}

	fsCapability := opts.FSCapability
	if fsCapability == "" {
		fsCapability = "copy"
	}
	hello := map[string]any{
		"type":          wire.TypeHello,
		"agent_version": opts.AgentVersion,
		"fs_capability": fsCapability,
		"capacity":      map[string]any{},
	}
	if err := writer.write(sctx, hello); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	readCtx, cancelRead := context.WithTimeout(sctx, 10*time.Second)
	_, ack, err := conn.Read(readCtx)
	cancelRead()
	if err != nil {
		return fmt.Errorf("await hello.ack: %w", err)
	}
	log.Info("agent channel open", "panel", endpoint, "ack", string(ack))

	bridge := NewBridge(opts.Supervisor.DataDir, func(frame any) {
		_ = writer.write(sctx, frame)
	}, log)
	defer bridge.Close()

	console := NewConsole(opts.Supervisor.Runtime, log)
	console.Send = func(serverID string, seq uint64, payload []byte) {
		_ = writer.writeBinary(sctx, wire.EncodeConsole(serverID, seq, payload))
	}
	defer console.Close()

	reconciler := &reconciler{
		apply:   opts.Supervisor.Apply,
		observe: opts.Supervisor.Observe,
		send: func(ctx context.Context, frame wire.ServerStateFrame) error {
			console.Sync(frame.ServerID, frame.State)
			bridge.Sync(frame.ServerID, frame.State)
			return writer.write(ctx, frame)
		},
		reported: make(map[string]string),
		log:      log,
	}

	readErr := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(sctx)
			if err != nil {
				readErr <- err
				return
			}
			var envelope struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &envelope) != nil {
				continue
			}
			switch envelope.Type {
			case wire.TypeServers:
				var frame wire.ServersFrame
				if json.Unmarshal(data, &frame) == nil {
					reconciler.submit(sctx, frame)
				}
			case wire.TypeBridgeCommand:
				var frame wire.BridgeCommandFrame
				if json.Unmarshal(data, &frame) == nil {
					if err := bridge.Command(frame); err != nil {
						log.Warn("bridge command rejected", "server", frame.ServerID, "err", err)
						_ = writer.write(sctx, wire.BridgeEventFrame{
							Type: wire.TypeBridgeEvent, ServerID: frame.ServerID,
							Event: wire.BridgeEvent{Kind: "command", Action: frame.Action, ID: frame.ID,
								Error: err.Error()},
						})
					}
				}
			}
		}
	}()

	go RunMetrics(sctx, opts.Supervisor.Runtime, time.Second, func(frame wire.MetricsFrame) {
		_ = writer.write(sctx, frame)
	}, log)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusGoingAway, "shutdown")
			return nil
		case err := <-readErr:
			return err
		case <-ticker.C:
			if err := writer.write(sctx, map[string]any{
				"type": wire.TypeHeartbeat,
				"at":   time.Now().UTC(),
			}); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
			// Observe reports crashes and out-of-band changes without acting.
			reconciler.report(sctx)
		}
	}
}

// connWriter serializes writes: heartbeats and state reports share one socket.
type connWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *connWriter) write(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.conn.Write(writeCtx, websocket.MessageText, data)
}

func (w *connWriter) writeBinary(ctx context.Context, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return w.conn.Write(writeCtx, websocket.MessageBinary, payload)
}

// reconciler coalesces desired-state frames and applies the latest one.
type reconciler struct {
	mu       sync.Mutex
	pending  *wire.ServersFrame
	last     []wire.ServerSpec
	reported map[string]string
	busy     bool
	apply    func(context.Context, wire.ServerSpec) (string, string)
	observe  func(context.Context, wire.ServerSpec) (string, string)
	send     func(context.Context, wire.ServerStateFrame) error
	log      *slog.Logger
}

func (r *reconciler) submit(ctx context.Context, frame wire.ServersFrame) {
	r.mu.Lock()
	r.pending = &frame
	r.last = frame.Items
	if r.busy {
		r.mu.Unlock()
		return
	}
	r.busy = true
	r.mu.Unlock()
	go r.drain(ctx)
}

// report sends a state frame only when a server's observed state changed.
func (r *reconciler) report(ctx context.Context) {
	r.mu.Lock()
	specs := append([]wire.ServerSpec(nil), r.last...)
	r.mu.Unlock()
	if len(specs) == 0 {
		return
	}

	for _, spec := range specs {
		state, detail := r.observe(ctx, spec)

		r.mu.Lock()
		changed := r.reported[spec.ID] != state
		if changed {
			r.reported[spec.ID] = state
		}
		r.mu.Unlock()
		if !changed {
			continue
		}
		if err := r.send(ctx, wire.ServerStateFrame{
			Type:     wire.TypeServerState,
			ServerID: spec.ID,
			State:    state,
			Revision: spec.Revision,
			Detail:   detail,
		}); err != nil {
			return
		}
	}
}

func (r *reconciler) drain(ctx context.Context) {
	for {
		r.mu.Lock()
		frame := r.pending
		r.pending = nil
		if frame == nil {
			r.busy = false
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		for _, spec := range frame.Items {
			state, detail := r.apply(ctx, spec)
			if detail != "" {
				r.log.Warn("server apply failed", "server", spec.ID, "detail", detail)
			}
			r.mu.Lock()
			r.reported[spec.ID] = state
			r.mu.Unlock()
			err := r.send(ctx, wire.ServerStateFrame{
				Type:     wire.TypeServerState,
				ServerID: spec.ID,
				State:    state,
				Revision: spec.Revision,
				Detail:   detail,
			})
			if err != nil {
				return
			}
		}
	}
}

// socketURL turns the panel base URL into the agent channel endpoint.
func socketURL(panelURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(panelURL))
	if err != nil {
		return "", fmt.Errorf("panel_url: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "wss":
	default:
		return "", errors.New("panel_url must use https")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/agent/ws"
	return u.String(), nil
}
