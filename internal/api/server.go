// Package api is the panel's HTTP surface. Handlers stay thin: at M0 there is
// health, readiness, and the embedded page; the capability layer arrives at M3a.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"delphi.dev/delphi/internal/config"
	"delphi.dev/delphi/web"
)

// Options configures the handler.
type Options struct {
	// BasePath is the prefix the UI is served under; normalized on use.
	BasePath string
	// Version is reported by health and readiness.
	Version string
	// Ready is called by /readyz; nil means always ready.
	Ready func(context.Context) error
}

// New builds the panel handler.
func New(opts Options) (http.Handler, error) {
	index, err := web.Index()
	if err != nil {
		return nil, err
	}

	base := config.NormalizeBasePath(opts.BasePath)
	ready := opts.Ready
	if ready == nil {
		ready = func(context.Context) error { return nil }
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, statusBody{Status: "ok", Version: opts.Version})
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			// The reason is logged by the caller; the body stays generic so a
			// probe never leaks a host, DSN, or driver error.
			writeJSON(w, http.StatusServiceUnavailable, statusBody{
				Status: "unready", Database: "unreachable", Version: opts.Version,
			})
			return
		}
		writeJSON(w, http.StatusOK, statusBody{
			Status: "ready", Database: "ok", Version: opts.Version,
		})
	})

	// A pattern ending in "/" matches the subtree, so guard the exact path.
	// ServeMux already redirects "/delphi" to "/delphi/" for a subpath base.
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != base {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(index)
	})

	return mux, nil
}

type statusBody struct {
	Status   string `json:"status"`
	Database string `json:"database,omitempty"`
	Version  string `json:"version,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
