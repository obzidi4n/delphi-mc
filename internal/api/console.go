package api

import (
	"fmt"
	"sync"

	"delphi.dev/delphi/internal/wire"
)

const consoleRing = 5000

// ConsoleHub keeps a per-server ring of console lines and fans them out to
// browser subscribers. The agent pushes; the hub never asks it to replay, so
// the panel ring is the join buffer.
type ConsoleHub struct {
	mu     sync.Mutex
	rooms  map[string]*consoleRoom
	metric *MetricsStore
}

type consoleRoom struct {
	lines []wire.ConsoleLine
	last  uint64
	subs  map[*ConsoleSub]struct{}
}

// ConsoleSub is one browser subscription. A slow subscriber is closed rather
// than allowed to block the fan-out; the UI reconnects and gets the ring.
type ConsoleSub struct {
	mu      sync.Mutex
	console chan []wire.ConsoleLine
	stats   chan wire.MetricsFrame
	closed  bool
}

// NewConsoleHub returns an empty hub.
func NewConsoleHub() *ConsoleHub {
	return &ConsoleHub{rooms: map[string]*consoleRoom{}, metric: NewMetricsStore()}
}

// Metrics exposes the rollup store.
func (h *ConsoleHub) Metrics() *MetricsStore { return h.metric }

// IngestFrame decodes and ingests one agent console frame.
func (h *ConsoleHub) IngestFrame(frame []byte) {
	serverID, seq, payload, err := wire.DecodeConsole(frame)
	if err != nil || serverID == "" {
		return
	}
	h.Ingest(serverID, seq, payload)
}

// Ingest appends a batch and fans it out. A sequence gap is announced with a
// synthetic line so the console never silently skips.
func (h *ConsoleHub) Ingest(serverID string, seq uint64, payload []byte) {
	text := string(payload)
	lines := splitLines(text)
	if len(lines) == 0 {
		return
	}
	h.mu.Lock()
	room := h.rooms[serverID]
	if room == nil {
		room = &consoleRoom{subs: map[*ConsoleSub]struct{}{}}
		h.rooms[serverID] = room
	}
	var gap *wire.ConsoleLine
	if room.last > 0 && seq > room.last+1 {
		missing := seq - room.last - 1
		gap = &wire.ConsoleLine{Seq: room.last + 1, Text: fmt.Sprintf("[delphi] %d line(s) missing", missing)}
	}
	batch := make([]wire.ConsoleLine, 0, len(lines))
	for i, text := range lines {
		batch = append(batch, wire.ConsoleLine{Seq: seq + uint64(i), Text: text})
	}
	for _, line := range batch {
		room.lines = append(room.lines, line)
	}
	if len(room.lines) > consoleRing {
		room.lines = room.lines[len(room.lines)-consoleRing:]
	}
	room.last = seq + uint64(len(lines)) - 1
	subs := make([]*ConsoleSub, 0, len(room.subs))
	for sub := range room.subs {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	if gap != nil {
		h.broadcastConsole(serverID, []wire.ConsoleLine{*gap})
	}
	h.broadcastConsole(serverID, batch)
}

func (h *ConsoleHub) broadcastConsole(serverID string, lines []wire.ConsoleLine) {
	h.mu.Lock()
	room := h.rooms[serverID]
	var drop []*ConsoleSub
	if room != nil {
		for sub := range room.subs {
			if !sub.pushConsole(lines) {
				drop = append(drop, sub)
			}
		}
	}
	h.mu.Unlock()
	for _, sub := range drop {
		h.Unsubscribe(serverID, sub)
	}
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	out := make([]string, 0, 16)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			out = append(out, text[start:i])
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// PublishStats stores a sample and pushes it to stats subscribers.
func (h *ConsoleHub) PublishStats(f wire.MetricsFrame) {
	if f.ServerID == "" {
		return
	}
	point := MetricPoint{At: f.At, CPUPercent: f.CPUPercent, MemoryBytes: f.MemoryBytes}
	h.metric.Ingest(f.ServerID, point)

	h.mu.Lock()
	room := h.rooms[f.ServerID]
	var drop []*ConsoleSub
	if room != nil {
		for sub := range room.subs {
			if !sub.pushStats(f) {
				drop = append(drop, sub)
			}
		}
	}
	h.mu.Unlock()
	for _, sub := range drop {
		h.Unsubscribe(f.ServerID, sub)
	}
}

// Subscribe registers a browser and returns the current ring.
func (h *ConsoleHub) Subscribe(serverID string) (*ConsoleSub, []wire.ConsoleLine) {
	sub := &ConsoleSub{
		console: make(chan []wire.ConsoleLine, 128),
		stats:   make(chan wire.MetricsFrame, 8),
	}
	h.mu.Lock()
	room := h.rooms[serverID]
	if room == nil {
		room = &consoleRoom{subs: map[*ConsoleSub]struct{}{}}
		h.rooms[serverID] = room
	}
	room.subs[sub] = struct{}{}
	backlog := make([]wire.ConsoleLine, len(room.lines))
	copy(backlog, room.lines)
	h.mu.Unlock()
	return sub, backlog
}

// Unsubscribe removes and closes a subscription.
func (h *ConsoleHub) Unsubscribe(serverID string, sub *ConsoleSub) {
	h.mu.Lock()
	if room := h.rooms[serverID]; room != nil {
		delete(room.subs, sub)
	}
	h.mu.Unlock()
	sub.close()
}

func (s *ConsoleSub) pushConsole(lines []wire.ConsoleLine) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.console <- lines:
		return true
	default:
		return false
	}
}

func (s *ConsoleSub) pushStats(f wire.MetricsFrame) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.stats <- f:
		return true
	default:
		return false
	}
}

func (s *ConsoleSub) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.console)
	close(s.stats)
}
