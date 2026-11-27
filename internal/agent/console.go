package agent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Console tails container output per running server, keeps a ring of the last
// lines, and emits newline-delimited batches every Interval. It never blocks a
// log reader on the network: the batch sender runs on its own goroutine.
type Console struct {
	Runtime  Runtime
	Log      *slog.Logger
	Send     func(serverID string, seq uint64, payload []byte)
	Interval time.Duration
	RingSize int

	mu    sync.Mutex
	tails map[string]*tailer
}

type tailer struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	interval time.Duration
	ringSize int
	send     func(string, uint64, []byte)

	mu         sync.Mutex
	ring       [][]byte
	base       uint64 // seq of ring[0]
	next       uint64 // seq of the next line (starts at 1)
	pending    [][]byte
	pendingSeq uint64
}

// NewConsole returns a console manager.
func NewConsole(rt Runtime, log *slog.Logger) *Console {
	return &Console{
		Runtime: rt, Log: log,
		Interval: 16 * time.Millisecond, RingSize: 5000,
		tails: map[string]*tailer{},
	}
}

// Sync starts or stops the tailer from an observed server state.
func (c *Console) Sync(serverID, state string) {
	if state == "running" {
		c.Start(serverID)
		return
	}
	c.Stop(serverID)
}

// Start begins tailing a server once; it is idempotent.
func (c *Console) Start(serverID string) {
	if c == nil || serverID == "" {
		return
	}
	c.mu.Lock()
	if _, ok := c.tails[serverID]; ok {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &tailer{
		id: serverID, ctx: ctx, cancel: cancel,
		interval: c.Interval, ringSize: c.RingSize, send: c.Send,
		next: 1,
	}
	c.tails[serverID] = t
	c.mu.Unlock()

	go t.flushLoop()
	go c.follow(ctx, t)
}

// Stop ends a tailer and drops its ring.
func (c *Console) Stop(serverID string) {
	c.mu.Lock()
	t := c.tails[serverID]
	delete(c.tails, serverID)
	c.mu.Unlock()
	if t != nil {
		t.cancel()
	}
}

// Close stops every tailer (agent shutdown).
func (c *Console) Close() {
	c.mu.Lock()
	tails := c.tails
	c.tails = map[string]*tailer{}
	c.mu.Unlock()
	for _, t := range tails {
		t.cancel()
	}
}

// follow reconnects the log stream for as long as the tailer lives; a container
// restart should resume output without a new desired-state frame.
func (c *Console) follow(ctx context.Context, t *tailer) {
	for {
		reader, err := c.Runtime.FollowLogs(ctx, t.id)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if c.Log != nil {
				c.Log.Warn("console follow failed", "server", t.id, "err", err)
			}
		} else {
			err = t.pump(ctx, reader)
			_ = reader.Close()
			if ctx.Err() != nil {
				return
			}
			if err != nil && c.Log != nil && !errors.Is(err, io.EOF) {
				c.Log.Debug("console stream ended", "server", t.id, "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (t *tailer) pump(ctx context.Context, reader io.Reader) error {
	buf := bufio.NewReaderSize(reader, 1<<20)
	for {
		line, err := buf.ReadString('\n')
		if len(line) > 0 {
			t.append(strings.TrimRight(line, "\r\n"))
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (t *tailer) append(text string) {
	t.mu.Lock()
	seq := t.next
	t.next++
	if t.ringSize > 0 {
		if len(t.ring) == 0 {
			t.base = seq
		}
		t.ring = append(t.ring, []byte(text))
		if len(t.ring) > t.ringSize {
			t.ring = t.ring[1:]
			t.base++
		}
	}
	if len(t.pending) == 0 {
		t.pendingSeq = seq
	}
	t.pending = append(t.pending, []byte(text))
	t.mu.Unlock()
}

func (t *tailer) flushLoop() {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			t.flush()
			return
		case <-ticker.C:
			t.flush()
		}
	}
}

func (t *tailer) flush() {
	t.mu.Lock()
	if len(t.pending) == 0 {
		t.mu.Unlock()
		return
	}
	seq := t.pendingSeq
	lines := t.pending
	t.pending = nil
	t.mu.Unlock()

	payload := make([]byte, 0, 256)
	for i, line := range lines {
		if i > 0 {
			payload = append(payload, '\n')
		}
		payload = append(payload, line...)
	}
	if t.send != nil {
		t.send(t.id, seq, payload)
	}
}
