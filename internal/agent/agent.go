// Package agent is the node daemon: it enrolls once, then keeps one mTLS
// WebSocket open to the panel, reporting a heartbeat on the configured interval.
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
	"time"

	"github.com/coder/websocket"
)

// ConnectOptions configures the channel.
type ConnectOptions struct {
	PanelURL     string
	StateDir     string
	AgentVersion string
	Interval     time.Duration
}

// Connect keeps the agent channel open until ctx is cancelled, reconnecting
// with exponential backoff.
func Connect(ctx context.Context, opts ConnectOptions, log *slog.Logger) error {
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
		err := session(ctx, client, endpoint, opts.AgentVersion, interval, log)
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

func session(ctx context.Context, client *http.Client, endpoint, agentVersion string, interval time.Duration, log *slog.Logger) error {
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()

	hello, _ := json.Marshal(map[string]any{
		"type":          "hello",
		"agent_version": agentVersion,
		"fs_capability": "copy",
		"capacity":      map[string]any{},
	})
	if err := writeFrame(ctx, conn, hello); err != nil {
		return fmt.Errorf("hello: %w", err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_, ack, err := conn.Read(readCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("await hello.ack: %w", err)
	}
	log.Info("agent channel open", "panel", endpoint, "ack", string(ack))

	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				readErr <- err
				return
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
			heartbeat, _ := json.Marshal(map[string]any{"type": "heartbeat", "at": time.Now().UTC()})
			if err := writeFrame(ctx, conn, heartbeat); err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		}
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, data []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
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
