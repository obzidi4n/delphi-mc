package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	mu      sync.Mutex
	conns   map[string]*sink
	pending map[string]chan json.RawMessage
}

type sink struct {
	conn *websocket.Conn
	mu   sync.Mutex // one writer at a time; the read loop runs concurrently
}

// NewHub returns an empty hub.
func NewHub() *Hub {
	return &Hub{conns: make(map[string]*sink), pending: make(map[string]chan json.RawMessage)}
}

// Call writes a request with a generated request id and waits for the node's
// matching result frame.
func (h *Hub) Call(ctx context.Context, nodeID string, build func(requestID string) any, timeout time.Duration) (json.RawMessage, error) {
	h.mu.Lock()
	s := h.conns[nodeID]
	if s == nil {
		h.mu.Unlock()
		return nil, ErrNodeOffline
	}
	id := "req_" + randomID()
	ch := make(chan json.RawMessage, 1)
	h.pending[id] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()
	if err := s.write(ctx, build(id)); err != nil {
		return nil, err
	}
	select {
	case data := <-ch:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, errors.New("the node did not answer in time")
	}
}

// Resolve delivers a result frame to the waiting call.
func (h *Hub) Resolve(requestID string, data json.RawMessage) {
	h.mu.Lock()
	ch := h.pending[requestID]
	h.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- data:
	default:
	}
}

func randomID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic("api: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(raw)
}

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
