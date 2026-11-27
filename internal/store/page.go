package store

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// ErrInvalidCursor reports a cursor this API did not issue or one whose
// position no longer exists.
var ErrInvalidCursor = errors.New("invalid cursor")

// Page limits.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

func pageLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultPageLimit
	case limit > MaxPageLimit:
		return MaxPageLimit
	default:
		return limit
	}
}

// pageQuerySize fetches one extra row to detect a next page.
func pageQuerySize(limit int) int { return pageLimit(limit) + 1 }

// encodeCursor joins keyset parts with a separator no id or name contains.
func encodeCursor(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x1f")))
}

// decodeCursor returns nil parts for an empty cursor.
func decodeCursor(cursor string, n int) ([]string, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	parts := strings.Split(string(raw), "\x1f")
	if len(parts) != n {
		return nil, ErrInvalidCursor
	}
	return parts, nil
}

func decodeSeqCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, ErrInvalidCursor
	}
	seq, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || seq < 0 {
		return 0, ErrInvalidCursor
	}
	return seq, nil
}

func encodeSeqCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}

// pageServers trims to the limit and returns the cursor of the last kept row.
func pageServers(all []Server, limit int) ([]Server, string) {
	if len(all) <= pageLimit(limit) {
		return all, ""
	}
	kept := all[:pageLimit(limit)]
	last := kept[len(kept)-1]
	return kept, encodeCursor(last.Name, last.ID)
}
