package store

import (
	"errors"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	c := encodeCursor("survival", "s_1")
	parts, err := decodeCursor(c, 2)
	if err != nil || parts[0] != "survival" || parts[1] != "s_1" {
		t.Fatalf("parts = %v, err = %v", parts, err)
	}
	if parts, err := decodeCursor("", 2); parts != nil || err != nil {
		t.Fatalf("empty cursor = %v, %v", parts, err)
	}
	for _, bad := range []string{"not base64!", encodeCursor("only-one")} {
		if _, err := decodeCursor(bad, 2); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("%q: err = %v, want ErrInvalidCursor", bad, err)
		}
	}
}

func TestSeqCursorRejectsGarbage(t *testing.T) {
	if _, err := decodeSeqCursor("abc"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("err = %v", err)
	}
	seq, err := decodeSeqCursor(encodeSeqCursor(42))
	if err != nil || seq != 42 {
		t.Fatalf("seq = %d, err = %v", seq, err)
	}
}

func TestPageLimitBounds(t *testing.T) {
	cases := map[int]int{0: DefaultPageLimit, -5: DefaultPageLimit, 10: 10, 500: MaxPageLimit}
	for in, want := range cases {
		if got := pageLimit(in); got != want {
			t.Fatalf("pageLimit(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestPageServersTrimsAndSetsCursor(t *testing.T) {
	all := []Server{{ID: "s_1", Name: "a"}, {ID: "s_2", Name: "b"}, {ID: "s_3", Name: "c"}}
	kept, next := pageServers(all, 2)
	if len(kept) != 2 || next == "" {
		t.Fatalf("kept %d, next %q", len(kept), next)
	}
	parts, err := decodeCursor(next, 2)
	if err != nil || parts[0] != "b" || parts[1] != "s_2" {
		t.Fatalf("cursor = %q (%v), err = %v", next, parts, err)
	}
}
