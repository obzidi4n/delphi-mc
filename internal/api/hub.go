package api

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/wire"
)

// Hub tracks one live channel per node and pushes its desired server set.
// A node that is offline needs nothing pushed: state is durable in Postgres and
// re-synced on connect.
type Hub struct {
	// Build produces the desired set for a node.
	Build func(context.Context, string) ([]wire.ServerSpec, error)

	mu    sync.Mutex
	conns map[string]*sink
}

type sink struct {
	conn *websocket.Conn
	mu   sync.Mutex // one writer at a time; the read loop runs concurrently
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{conns: make(map[string]*sink)} }

// ErrNodeOffline reports that a node has no live channel.
var ErrNodeOffline = errors.New("node is offline")

// Send writes one frame to a node if it is connected.
func (h *Hub) Send(ctx context.Context, nodeID string, v any) error {
	h.mu.Lock()
	s := h.conns[nodeID]
	h.mu.Unlock()
	if s == nil {
		return ErrNodeOffline
	}
	return s.write(ctx, v)
}

// Register attaches a node's channel.
func (h *Hub) Register(nodeID string, conn *websocket.Conn) *sink {
	s := &sink{conn: conn}
	h.mu.Lock()
	h.conns[nodeID] = s
	h.mu.Unlock()
	return s
}

// Unregister detaches the channel if it is still the current one.
func (h *Hub) Unregister(nodeID string, s *sink) {
	h.mu.Lock()
	if h.conns[nodeID] == s {
		delete(h.conns, nodeID)
	}
	h.mu.Unlock()
}

// Sync sends the node's full desired set if it is connected.
func (h *Hub) Sync(ctx context.Context, nodeID string) error {
	if h == nil || h.Build == nil {
		return nil
	}
	items, err := h.Build(ctx, nodeID)
	if err != nil {
		return err
	}

	h.mu.Lock()
	s := h.conns[nodeID]
	h.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.write(ctx, wire.ServersFrame{Type: wire.TypeServers, Items: items})
}

func (s *sink) write(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(writeCtx, websocket.MessageText, data)
}
