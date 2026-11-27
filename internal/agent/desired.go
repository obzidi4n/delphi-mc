package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"delphi.dev/delphi/internal/wire"
)

func desiredPath(dataDir string) string { return filepath.Join(dataDir, "desired.json") }

func saveDesired(dataDir string, items []wire.ServerSpec) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	tmp := desiredPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, desiredPath(dataDir))
}

func loadDesired(dataDir string) ([]wire.ServerSpec, error) {
	data, err := os.ReadFile(desiredPath(dataDir))
	if err != nil {
		return nil, err
	}
	var items []wire.ServerSpec
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// superviseOffline keeps crash recovery alive while the panel is unreachable,
// using the last desired set it sent. It does not create containers or fetch
// artifacts; that remains a panel-driven convergence.
func superviseOffline(ctx context.Context, sup *Supervisor, online *atomic.Bool, log *slog.Logger) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if online.Load() {
			continue
		}
		items, err := loadDesired(sup.DataDir)
		if err != nil {
			continue
		}
		for _, spec := range items {
			if spec.Deleted || spec.DesiredState != "running" {
				continue
			}
			state, detail := sup.Observe(ctx, spec)
			if detail != "" {
				log.Info("offline reconcile", "server", spec.ID, "state", state, "detail", detail)
			}
		}
	}
}
