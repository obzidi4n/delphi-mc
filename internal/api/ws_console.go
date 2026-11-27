package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/wire"
)

// consoleSocket streams one server's console and stats to a browser.
func (s *server) consoleSocket(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.principalFrom(r)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if s.opts.Console == nil {
		writeProblem(w, http.StatusNotImplemented, "console_disabled", "the console is not configured")
		return
	}
	serverID := r.PathValue("id")
	if !auth.Allows(principal.Scopes, "server:"+serverID+":read") {
		writeProblem(w, http.StatusForbidden, "forbidden", "missing scope server:"+serverID+":read")
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	sub, backlog := s.opts.Console.Subscribe(serverID)
	defer s.opts.Console.Unsubscribe(serverID, sub)

	send := func(v any) error {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return conn.Write(ctx, websocket.MessageText, data)
	}
	writeConsole := func(lines []wire.ConsoleLine) error {
		if len(lines) == 0 {
			return nil
		}
		if len(lines) > 2000 {
			lines = lines[len(lines)-2000:]
		}
		texts := make([]string, len(lines))
		for i, line := range lines {
			texts[i] = line.Text
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		return conn.Write(ctx, websocket.MessageBinary,
			wire.EncodeConsole(serverID, lines[0].Seq, []byte(strings.Join(texts, "\n"))))
	}

	if err := send(map[string]any{"type": "hello", "server_id": serverID}); err != nil {
		return
	}
	if point, ok := s.opts.Console.Metrics().Latest(serverID); ok {
		_ = send(statsFrame(serverID, point))
	}
	if err := writeConsole(backlog); err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				cancel()
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case lines, ok := <-sub.console:
			if !ok {
				return
			}
			if err := writeConsole(lines); err != nil {
				return
			}
		case frame, ok := <-sub.stats:
			if !ok {
				return
			}
			point := MetricPoint{At: frame.At, CPUPercent: frame.CPUPercent, MemoryBytes: frame.MemoryBytes}
			if err := send(statsFrame(serverID, point)); err != nil {
				return
			}
		}
	}
}

func statsFrame(serverID string, point MetricPoint) map[string]any {
	return map[string]any{
		"type":         "stats",
		"server_id":    serverID,
		"at":           point.At,
		"cpu_percent":  point.CPUPercent,
		"memory_bytes": point.MemoryBytes,
	}
}

// getServerMetrics returns the in-memory rollups for one server.
func (s *server) getServerMetrics(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}
	id := r.PathValue("id")
	if _, err := service.Get(r.Context(), principal, id); err != nil {
		writeCapabilityError(w, err)
		return
	}
	if s.opts.Console == nil {
		writeProblem(w, http.StatusNotImplemented, "metrics_disabled", "metrics are not configured")
		return
	}
	resolution := r.URL.Query().Get("resolution")
	switch resolution {
	case "", "1s", "15s", "15m", "5m":
	default:
		writeProblem(w, http.StatusBadRequest, "invalid_resolution", "resolution must be 1s, 15s, or 5m")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	points := s.opts.Console.Metrics().Query(id, resolution, limit)
	items := make([]apitypes.MetricPointDTO, 0, len(points))
	for _, p := range points {
		items = append(items, apitypes.MetricPointDTO{
			At: p.At, CPUPercent: p.CPUPercent, MemoryBytes: p.MemoryBytes,
		})
	}
	writeJSON(w, http.StatusOK, apitypes.ServerMetricsDTO{Resolution: resolution, Items: items})
}
