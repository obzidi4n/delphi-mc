package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			t.Errorf("path = %q, want /readyz", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ready","database":"ok","version":"1.2.3"}`)
	}))
	defer srv.Close()

	got, err := Probe(context.Background(), NewClient(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", got)
	}
}

func TestProbeUnready(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	if _, err := Probe(context.Background(), NewClient(), srv.URL); err == nil {
		t.Fatal("expected an error for a 503")
	}
}

func TestProbeInvalidURL(t *testing.T) {
	if _, err := Probe(context.Background(), NewClient(), "not-a-url"); err == nil {
		t.Fatal("expected an error for an invalid URL")
	}
}

func TestProbeUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	if _, err := Probe(context.Background(), NewClient(), url); err == nil {
		t.Fatal("expected an error for a closed server")
	}
}
