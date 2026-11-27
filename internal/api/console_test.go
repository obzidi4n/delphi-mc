package api

import (
	"testing"
	"time"

	"delphi.dev/delphi/internal/wire"
)

func drainLines(t *testing.T, sub *ConsoleSub, want int) []wire.ConsoleLine {
	t.Helper()
	var out []wire.ConsoleLine
	deadline := time.After(50 * time.Millisecond)
	for len(out) < want {
		select {
		case lines, ok := <-sub.console:
			if !ok {
				t.Fatalf("subscription closed after %d lines", len(out))
			}
			out = append(out, lines...)
		case <-deadline:
			t.Fatalf("got %d lines, want %d", len(out), want)
		}
	}
	return out
}

func TestConsoleFanoutToTwentyViewersUnderFiftyMillis(t *testing.T) {
	h := NewConsoleHub()
	const viewers = 20
	subs := make([]*ConsoleSub, viewers)
	for i := range subs {
		subs[i], _ = h.Subscribe("s_1")
		defer h.Unsubscribe("s_1", subs[i])
	}
	start := time.Now()
	h.Ingest("s_1", 1, []byte("alpha\nbeta\ngamma"))
	for _, sub := range subs {
		lines := drainLines(t, sub, 3)
		if lines[0].Seq != 1 || lines[2].Text != "gamma" {
			t.Fatalf("lines = %v", lines)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("fan-out took %s", elapsed)
	}
}

func TestConsoleBacklogAndGapMarker(t *testing.T) {
	h := NewConsoleHub()
	h.Ingest("s_1", 1, []byte("one\ntwo"))
	sub, backlog := h.Subscribe("s_1")
	defer h.Unsubscribe("s_1", sub)
	if len(backlog) != 2 || backlog[1].Text != "two" {
		t.Fatalf("backlog = %v", backlog)
	}
	h.Ingest("s_1", 10, []byte("ten"))
	gap := drainLines(t, sub, 1)
	if len(gap) != 1 || gap[0].Seq != 3 {
		t.Fatalf("gap line = %v", gap)
	}
	if gap[0].Text == "" {
		t.Fatal("gap has no text")
	}
	next := drainLines(t, sub, 1)
	if next[0].Seq != 10 || next[0].Text != "ten" {
		t.Fatalf("next = %v", next)
	}
}

func TestConsoleSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	h := NewConsoleHub()
	slow, _ := h.Subscribe("s_1")
	fast, _ := h.Subscribe("s_1")
	defer h.Unsubscribe("s_1", fast)

	// Drain the fast subscriber while ingesting; the slow one never reads.
	got := make(chan int, 1)
	go func() {
		n := 0
		for lines := range fast.console {
			_ = lines
			n++
			if n == 200 {
				got <- n
				return
			}
		}
		got <- -1
	}()
	for i := 0; i < 200; i++ {
		h.Ingest("s_1", uint64(i*10+1), []byte("line"))
		// Let the fast reader drain; the slow one holds its buffer and is
		// dropped deterministically without racing the scheduler.
		time.Sleep(100 * time.Microsecond)
	}
	select {
	case n := <-got:
		if n != 200 {
			t.Fatalf("fast subscriber got %d batches", n)
		}
	case <-time.After(time.Second):
		t.Fatal("fast subscriber did not receive 200 batches")
	}
	slow.mu.Lock()
	closed := slow.closed
	slow.mu.Unlock()
	if !closed {
		t.Fatal("slow subscriber was not dropped")
	}
}
