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

	reconciler := &reconciler{
		apply:   opts.Supervisor.Apply,
		observe: opts.Supervisor.Observe,
		send: func(ctx context.Context, frame wire.ServerStateFrame) error {
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
			if envelope.Type != wire.TypeServers {
				continue
			}
			var frame wire.ServersFrame
			if json.Unmarshal(data, &frame) == nil {
				reconciler.submit(sctx, frame)
			}
		}
	}()

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
