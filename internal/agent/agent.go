// Package agent is the node daemon. At M0 it establishes liveness against the
// panel; enrollment, the mTLS WebSocket channel, and supervision land at M1-M2.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Config is the resolved agent configuration.
type Config struct {
	PanelURL string
	Interval time.Duration
}

// NewClient returns the HTTP client used for panel probes.
func NewClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// Probe asks the panel whether it is ready and returns the panel version.
func Probe(ctx context.Context, client *http.Client, panelURL string) (string, error) {
	endpoint := strings.TrimRight(panelURL, "/") + "/readyz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("probe %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("panel not ready: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return payload.Version, nil
}

// Run probes the panel until ctx is cancelled, or once when once is true.
func Run(ctx context.Context, cfg Config, log *slog.Logger, once bool) error {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	client := NewClient()

	for {
		panelVersion, err := Probe(ctx, client, cfg.PanelURL)
		switch {
		case err != nil:
			log.Warn("panel unavailable", "panel", cfg.PanelURL, "err", err)
			if once {
				return err
			}
		default:
			log.Info("panel ready", "panel", cfg.PanelURL, "panel_version", panelVersion)
			if once {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}
