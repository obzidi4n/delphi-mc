package api_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"delphi.dev/delphi/internal/api"
	"delphi.dev/delphi/internal/wire"
)

func TestConsoleWebSocketServesTwentyViewers(t *testing.T) {
	console := api.NewConsoleHub()
	console.Ingest("s_1", 1, []byte("first\nsecond"))
	h, err := api.New(api.Options{AdminToken: "secret", Console: console, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/servers/s_1"
	var conns []*websocket.Conn
	defer func() {
		for _, c := range conns {
			c.CloseNow()
		}
	}()
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			HTTPHeader: map[string][]string{"Authorization": {"Bearer secret"}},
		})
		cancel()
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		conns = append(conns, conn)
	}

	// Every viewer first receives the backlog (2 lines).
	readBatch := func(c *websocket.Conn) ([]string, uint64) {
		t.Helper()
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			typ, data, err := c.Read(ctx)
			cancel()
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if typ != websocket.MessageBinary {
				continue // hello / stats precede the backlog
			}
			id, seq, payload, err := wire.DecodeConsole(data)
			if err != nil || id != "s_1" {
				t.Fatalf("decode: id=%q err=%v", id, err)
			}
			return strings.Split(string(payload), "\n"), seq
		}
	}
	for _, c := range conns {
		lines, seq := readBatch(c)
		if seq != 1 || len(lines) != 2 || lines[0] != "first" {
			t.Fatalf("backlog = %v seq=%d", lines, seq)
		}
	}

	start := time.Now()
	console.Ingest("s_1", 3, []byte("live"))
	for _, c := range conns {
		lines, seq := readBatch(c)
		if seq != 3 || lines[0] != "live" {
			t.Fatalf("live = %v seq=%d", lines, seq)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("20 viewers took %s", elapsed)
	}
}
