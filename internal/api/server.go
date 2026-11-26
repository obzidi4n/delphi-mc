// Package api is the panel's HTTP surface: health, the embedded page, agent
// enrollment and channel, and the node registry API.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/config"
	"delphi.dev/delphi/internal/pki"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/web"
)

// NodeStore is the subset of the registry the API needs.
type NodeStore interface {
	CreateEnrollmentToken(ctx context.Context, orgID string, ttl time.Duration) (string, time.Time, error)
	EnrollNode(ctx context.Context, token string, n store.Node) (store.Node, error)
	NodeBySerial(ctx context.Context, serial string) (store.Node, error)
	TouchNode(ctx context.Context, id string, at time.Time) error
	UpdateNodeHello(ctx context.Context, id, agentVersion, fsCapability string, capacity []byte) error
	SetNodeStatus(ctx context.Context, id, status string) error
	ListNodes(ctx context.Context) ([]store.Node, error)
	RevokeNode(ctx context.Context, id string) error
}

// Options configures the handler.
type Options struct {
	// BasePath is the prefix the UI is served under; normalized on use.
	BasePath string
	// Version is reported by health and readiness.
	Version string
	// Ready is called by /readyz; nil means always ready.
	Ready func(context.Context) error
	// Nodes is the registry; nil disables the agent and node surfaces.
	Nodes NodeStore
	// AdminToken authenticates /api/v1 until sessions land at M3a.
	AdminToken string
	// CA signs node certificates; required for enrollment.
	CA *pki.CA
	// Servers is the server-lifecycle capability; nil disables server routes.
	Servers *capability.Servers
	// Hub pushes desired state to connected nodes.
	Hub *Hub
	// Heartbeat is the expected agent interval; channel timeouts derive from it.
	Heartbeat time.Duration
	// Now is the clock; tests override it.
	Now func() time.Time
}

type server struct {
	opts  Options
	base  string
	index []byte
}

// New builds the panel handler.
func New(opts Options) (http.Handler, error) {
	index, err := web.Index()
	if err != nil {
		return nil, err
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 30 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &server{opts: opts, base: config.NormalizeBasePath(opts.BasePath), index: index}

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
	mux.HandleFunc("GET "+s.base, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != s.base {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(s.index)
	})

	mux.HandleFunc("POST /agent/enroll", s.enroll)
	mux.HandleFunc("GET /agent/ws", s.agentSocket)
	mux.HandleFunc("GET /api/v1/nodes", s.listNodes)
	mux.HandleFunc("POST /api/v1/nodes/{id}/revoke", s.revokeNode)
	mux.HandleFunc("POST /api/v1/enrollment-tokens", s.createToken)
	mux.HandleFunc("POST /api/v1/servers", s.createServer)
	mux.HandleFunc("GET /api/v1/servers", s.listServers)
	mux.HandleFunc("GET /api/v1/servers/{id}", s.getServer)
	mux.HandleFunc("POST /api/v1/servers/{id}/power", s.powerServer)

	return mux, nil
}

func (s *server) authorized(r *http.Request) bool {
	if s.opts.AdminToken == "" {
		return false
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	return constantTimeEqual(strings.TrimPrefix(h, prefix), s.opts.AdminToken)
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range len(a) {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
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

func writeProblem(w http.ResponseWriter, code int, kind, detail string) {
	writeJSON(w, code, map[string]any{
		"type":   "https://delphi.dev/errors/" + kind,
		"title":  http.StatusText(code),
		"status": code,
		"detail": detail,
		"code":   kind,
	})
}
