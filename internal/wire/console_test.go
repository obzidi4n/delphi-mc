package wire

import (
	"bytes"
	"errors"
	"testing"
)

func TestConsoleFrameRoundTrip(t *testing.T) {
	payload := []byte("first line\nsecond line")
	frame := EncodeConsole("s_abc", 42, payload)
	id, seq, got, err := DecodeConsole(frame)
	if err != nil {
		t.Fatal(err)
	}
	if id != "s_abc" || seq != 42 || !bytes.Equal(got, payload) {
		t.Fatalf("decoded %q %d %q", id, seq, got)
	}
}

func TestConsoleFrameRejectsTruncated(t *testing.T) {
	frame := EncodeConsole("s_abc", 1, []byte("x"))
	for _, cut := range []int{0, 3, 10, len(frame) - 1} {
		if _, _, _, err := DecodeConsole(frame[:cut]); !errors.Is(err, ErrBadConsoleFrame) {
			t.Fatalf("len %d: err = %v", cut, err)
		}
	}
}
