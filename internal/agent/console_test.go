package agent

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"delphi.dev/delphi/internal/wire"
)

type batch struct {
	server  string
	seq     uint64
	payload string
}

func TestConsoleBatchesWithMonotonicSeq(t *testing.T) {
	pr, pw := io.Pipe()
	rt := &fakeRuntime{follow: pr}
	c := NewConsole(rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.Interval = 5 * time.Millisecond
	var mu sync.Mutex
	var got []batch
	c.Send = func(serverID string, seq uint64, payload []byte) {
		mu.Lock()
		got = append(got, batch{serverID, seq, string(payload)})
		mu.Unlock()
	}

	c.Start("s_1")
	// Start is idempotent: only one tailer and one flusher.
	c.Start("s_1")
	for _, line := range []string{"line one\n", "line two\n", "line three\n"} {
		if _, err := pw.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no console batch arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.Stop("s_1")
	_ = pw.Close()
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	all := strings.Builder{}
	var firstSeq uint64
	for _, b := range got {
		if b.server != "s_1" {
			t.Fatalf("server = %q", b.server)
		}
		if firstSeq == 0 {
			firstSeq = b.seq
		}
		all.WriteString(b.payload)
		all.WriteString("\n")
	}
	if firstSeq != 1 {
		t.Fatalf("first seq = %d, want 1", firstSeq)
	}
	for _, want := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(all.String(), want) {
			t.Fatalf("missing %q in %q", want, all.String())
		}
	}
}

func TestConsoleSyncStopsOnStop(t *testing.T) {
	rt := &fakeRuntime{}
	c := NewConsole(rt, slog.New(slog.NewTextHandler(io.Discard, nil)))
	c.Interval = 5 * time.Millisecond
	c.Send = func(string, uint64, []byte) {}
	c.Sync("s_1", "running")
	c.mu.Lock()
	_, running := c.tails["s_1"]
	c.mu.Unlock()
	if !running {
		t.Fatal("running did not start a tailer")
	}
	c.Sync("s_1", "stopped")
	c.mu.Lock()
	_, still := c.tails["s_1"]
	c.mu.Unlock()
	if still {
		t.Fatal("stopped did not stop the tailer")
	}
}

func TestRunMetricsMapsContainerNames(t *testing.T) {
	rt := &fakeRuntime{stats: []ContainerStat{
		{Container: "delphi-s_1", CPUPercent: 12.5, MemoryBytes: 1024},
		{Container: "other", CPUPercent: 1, MemoryBytes: 1},
	}}
	cases := make(chan wire.MetricsFrame, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunMetrics(ctx, rt, 5*time.Millisecond, func(f wire.MetricsFrame) { cases <- f },
			slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	select {
	case f := <-cases:
		if f.ServerID != "s_1" || f.CPUPercent != 12.5 || f.MemoryBytes != 1024 {
			t.Fatalf("frame = %+v", f)
		}
		if f.Type != wire.TypeMetrics {
			t.Fatalf("type = %q", f.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("no metrics frame")
	}
	cancel()
	<-done
}
