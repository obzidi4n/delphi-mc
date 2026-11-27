// Package api is the panel's HTTP surface: pages, the JSON API, agent
// enrollment and the node channel.
//go:generate go run delphi.dev/delphi/cmd/gen-api

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/config"
	"delphi.dev/delphi/internal/pki"
	"delphi.dev/delphi/internal/store"
)

// NodeStore is the subset of the registry the agent channel needs.
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
	BasePath   string
	Version    string
	Ready      func(context.Context) error
	Nodes      NodeStore
	Services   *capability.Services
	Identity   IdentityStore
	AdminToken string
	CA         *pki.CA
	Hub        *Hub
	Heartbeat  time.Duration
	Now        func() time.Time
}

type server struct {
	opts Options
	base string
}

// New builds the panel handler.
func New(opts Options) (http.Handler, error) {
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 30 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &server{opts: opts, base: config.NormalizeBasePath(opts.BasePath)}

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

	// Pages. A pattern ending in "/" matches the subtree, so guard exactness.
	mux.HandleFunc("GET "+s.base, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != s.base {
			http.NotFound(w, r)
			return
		}
		s.renderHome(w, r)
	})
	mux.HandleFunc("POST "+s.base+"login", s.login)
	mux.HandleFunc("POST "+s.base+"logout", s.logout)

	mux.HandleFunc("POST /agent/enroll", s.enroll)
	mux.HandleFunc("GET /agent/ws", s.agentSocket)

	mux.HandleFunc("GET /api/v1/nodes", s.listNodes)
	mux.HandleFunc("POST /api/v1/nodes/{id}/revoke", s.revokeNode)
	mux.HandleFunc("POST /api/v1/enrollment-tokens", s.createToken)

	mux.HandleFunc("POST /api/v1/servers", s.createServer)
	mux.HandleFunc("GET /api/v1/servers", s.listServers)
	mux.HandleFunc("GET /api/v1/servers/{id}", s.getServer)
	mux.HandleFunc("PATCH /api/v1/servers/{id}", s.renameServer)
	mux.HandleFunc("DELETE /api/v1/servers/{id}", s.deleteServer)
	mux.HandleFunc("POST /api/v1/servers/{id}/power", s.powerServer)

	mux.HandleFunc("GET /api/v1/groups", s.listGroups)
	mux.HandleFunc("POST /api/v1/groups", s.createGroup)
	mux.HandleFunc("GET /api/v1/groups/{id}/members", s.listGroupMembers)
	mux.HandleFunc("POST /api/v1/groups/{id}/members", s.addGroupMember)
	mux.HandleFunc("DELETE /api/v1/groups/{id}/members/{server_id}", s.removeGroupMember)

	mux.HandleFunc("GET /api/v1/audit", s.listAudit)
	mux.HandleFunc("POST /api/v1/api-keys", s.createAPIKey)

	return mux, nil
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
