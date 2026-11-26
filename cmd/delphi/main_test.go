package main

import (
	"reflect"
	"testing"
)

func TestExtractPositional(t *testing.T) {
	valueFlags := map[string]bool{"panel": true, "token": true, "ca": true, "ttl": true}

	for _, tc := range []struct {
		name string
		args []string
		id   string
		rest []string
	}{
		{"id first", []string{"n_1", "--panel", "https://p"}, "n_1", []string{"--panel", "https://p"}},
		{"flags first", []string{"--panel", "https://p", "n_1"}, "n_1", []string{"--panel", "https://p"}},
		{"bool flag first", []string{"--json", "n_1"}, "n_1", []string{"--json"}},
		{"equals form", []string{"--panel=https://p", "n_1"}, "n_1", []string{"--panel=https://p"}},
		{"no positional", []string{"--panel", "https://p"}, "", []string{"--panel", "https://p"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, rest := extractPositional(tc.args, valueFlags)
			if id != tc.id {
				t.Fatalf("id = %q, want %q", id, tc.id)
			}
			if !reflect.DeepEqual(rest, tc.rest) {
				t.Fatalf("rest = %v, want %v", rest, tc.rest)
			}
		})
	}
}
