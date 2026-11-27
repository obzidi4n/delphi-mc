package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"delphi.dev/delphi/internal/wire"
)

// RunMetrics samples all containers every interval and reports each server.
// One process per tick, not one per server.
func RunMetrics(ctx context.Context, rt Runtime, interval time.Duration,
	send func(wire.MetricsFrame), log *slog.Logger) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			stats, err := rt.Stats(ctx)
			if err != nil {
				if log != nil && ctx.Err() == nil {
					log.Debug("metrics sample failed", "err", err)
				}
				continue
			}
			for _, stat := range stats {
				id := strings.TrimPrefix(stat.Container, "delphi-")
				if id == "" || id == stat.Container {
					continue
				}
				send(wire.MetricsFrame{
					Type: wire.TypeMetrics, ServerID: id,
					CPUPercent: stat.CPUPercent, MemoryBytes: stat.MemoryBytes,
					At: now.UTC(),
				})
			}
		}
	}
}
