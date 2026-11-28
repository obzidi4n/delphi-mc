// Package api is the panel's HTTP surface: pages, the JSON API, agent
// enrollment and the node channel.
//go:generate go run delphi.dev/delphi/cmd/gen-api

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"delphi.dev/delphi/internal/ai"
	"delphi.dev/delphi/internal/artifact"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/config"
	"delphi.dev/delphi/internal/pki"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// NodeStore is the subset of the registry the agent channel needs.
type NodeStore interface {
	CreateEnrollmentToken(ctx context.Context, orgID string, ttl time.Duration) (string, time.Time, error)
	EnrollNode(ctx context.Context, token string, n store.Node) (store.Node, error)
	NodeBySerial(ctx context.Context, serial string) (store.Node, error)
	TouchNode(ctx context.Context, id string, at time.Time) error
	UpdateNodeHello(ctx context.Context, id, agentVersion, fsCapability string, capacity []byte) error
	SetNodeStatus(ctx context.Context, id, status string) error
	RevokeNode(ctx context.Context, id string) error
}

// Options configures the handler.
type Options struct {
	BasePath        string
	Version         string
	Ready           func(context.Context) error
	Nodes           NodeStore
	Services        *capability.Services
	Console         *ConsoleHub
	AI              *ai.Service
	Files           *FileService
	Artifacts       *artifact.Store
	Backups         *capability.Backups
	AgentUpdatesDir string
	Alerts          *capability.Alerts
	Identity        IdentityStore
	AdminToken      string
	CA              *pki.CA
	Hub             *Hub
	// BridgeSender writes a bridge command to a node's agent channel.
	BridgeSender func(ctx context.Context, nodeID string, frame wire.BridgeCommandFrame) error
	Heartbeat    time.Duration
	Now          func() time.Time
	StartedAt    time.Time
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
	if opts.StartedAt.IsZero() {
		opts.StartedAt = opts.Now()
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
	mux.HandleFunc("GET "+s.base+"chat", s.renderChat)
	mux.HandleFunc("GET "+s.base+"team", s.renderTeam)
	mux.HandleFunc("POST "+s.base+"team", s.createUserForm)
	mux.HandleFunc("POST "+s.base+"team/{id}/role", s.setUserRoleForm)
	mux.HandleFunc("GET "+s.base+"servers/new", s.renderNewServerPage)
	mux.HandleFunc("GET "+s.base+"servers/{id}", s.renderConsole)
	mux.HandleFunc("GET "+s.base+"servers/{id}/files", s.renderFiles)
	mux.HandleFunc("GET "+s.base+"servers/{id}/plugins", s.renderPluginsPage)
	mux.HandleFunc("GET "+s.base+"servers/{id}/files/download", s.downloadFile)
	mux.HandleFunc("GET "+s.base+"audit", s.renderAudit)
	mux.HandleFunc("GET "+s.base+"groups", s.renderGroups)
	mux.HandleFunc("GET "+s.base+"nodes", s.renderNodesPage)
	mux.HandleFunc("GET "+s.base+"nodes/ca.crt", s.downloadCA)
	mux.HandleFunc("GET "+s.base+"templates", s.renderTemplatesPage)
	mux.HandleFunc("POST "+s.base+"groups", s.createGroupForm)
	mux.HandleFunc("POST "+s.base+"groups/{id}/members", s.addGroupMemberForm)
	mux.HandleFunc("POST "+s.base+"groups/{id}/members/{sid}/remove", s.removeGroupMemberForm)

	mux.HandleFunc("POST "+s.base+"agent/enroll", s.enroll)
	mux.HandleFunc("GET "+s.base+"agent/ws", s.agentSocket)
	mux.HandleFunc("GET "+s.base+"agent/blobs/{sha}", s.agentBlob)
	mux.HandleFunc("POST "+s.base+"agent/backups/{id}", s.uploadBackup)
	mux.HandleFunc("GET "+s.base+"agent/backups/{id}", s.downloadBackup)
	mux.HandleFunc("GET "+s.base+"ws/servers/{id}", s.consoleSocket)

	mux.HandleFunc("GET "+s.base+"api/v1/nodes", s.listNodes)
	mux.HandleFunc("POST "+s.base+"api/v1/nodes/{id}/revoke", s.revokeNode)
	mux.HandleFunc("POST "+s.base+"api/v1/nodes/{id}/drain", s.drainNode)
	mux.HandleFunc("POST "+s.base+"api/v1/fleet/agent-update", s.uploadAgentUpdate)
	mux.HandleFunc("GET "+s.base+"agent/updates/{sha}", s.downloadAgentUpdate)
	mux.HandleFunc("POST "+s.base+"api/v1/enrollment-tokens", s.createToken)

	mux.HandleFunc("POST "+s.base+"api/v1/servers", s.createServer)
	mux.HandleFunc("GET "+s.base+"api/v1/servers", s.listServers)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}", s.getServer)
	mux.HandleFunc("PATCH "+s.base+"api/v1/servers/{id}", s.renameServer)
	mux.HandleFunc("DELETE "+s.base+"api/v1/servers/{id}", s.deleteServer)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/power", s.powerServer)
	mux.HandleFunc("GET "+s.base+"api/v1/plugin-catalog", s.searchPluginCatalog)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/dev", s.listDevServers)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/dev", s.createDevServer)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/promotion", s.previewPromotion)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/promotion", s.promoteDevChanges)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/plugins", s.listPlugins)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/plugins", s.installServerPlugin)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/plugins/upload", s.uploadServerPlugin)
	mux.HandleFunc("DELETE "+s.base+"api/v1/servers/{id}/plugins", s.removeServerPlugin)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/updates", s.listUpdates)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/updates/apply", s.applyUpdates)
	mux.HandleFunc("POST "+s.base+"api/v1/artifacts/gc", s.gcArtifacts)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/backups", s.listBackups)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/backups", s.createBackup)
	mux.HandleFunc("POST "+s.base+"api/v1/backups/{id}/restore", s.restoreBackup)
	mux.HandleFunc("DELETE "+s.base+"api/v1/backups/{id}", s.deleteBackup)

	mux.HandleFunc("GET "+s.base+"api/v1/networks", s.listNetworks)
	mux.HandleFunc("POST "+s.base+"api/v1/networks", s.createNetwork)
	mux.HandleFunc("GET "+s.base+"api/v1/networks/{id}", s.getNetwork)
	mux.HandleFunc("GET "+s.base+"api/v1/schedules", s.listSchedules)
	mux.HandleFunc("POST "+s.base+"api/v1/schedules", s.createSchedule)
	mux.HandleFunc("DELETE "+s.base+"api/v1/schedules/{id}", s.deleteSchedule)
	mux.HandleFunc("GET "+s.base+"api/v1/alert-rules", s.listAlertRules)
	mux.HandleFunc("POST "+s.base+"api/v1/alert-rules", s.createAlertRule)
	mux.HandleFunc("DELETE "+s.base+"api/v1/alert-rules/{id}", s.deleteAlertRule)
	mux.HandleFunc("GET "+s.base+"api/v1/alert-channels", s.listAlertChannels)
	mux.HandleFunc("POST "+s.base+"api/v1/alert-channels", s.createAlertChannel)
	mux.HandleFunc("DELETE "+s.base+"api/v1/alert-channels/{id}", s.deleteAlertChannel)
	mux.HandleFunc("GET "+s.base+"api/v1/alerts", s.listAlerts)
	mux.HandleFunc("GET "+s.base+"api/v1/maintenance-windows", s.listMaintenanceWindows)
	mux.HandleFunc("POST "+s.base+"api/v1/maintenance-windows", s.createMaintenanceWindow)
	mux.HandleFunc("DELETE "+s.base+"api/v1/maintenance-windows/{id}", s.deleteMaintenanceWindow)

	mux.HandleFunc("GET "+s.base+"api/v1/groups", s.listGroups)
	mux.HandleFunc("POST "+s.base+"api/v1/groups", s.createGroup)
	mux.HandleFunc("GET "+s.base+"api/v1/groups/{id}/members", s.listGroupMembers)
	mux.HandleFunc("POST "+s.base+"api/v1/groups/{id}/members", s.addGroupMember)
	mux.HandleFunc("DELETE "+s.base+"api/v1/groups/{id}/members/{server_id}", s.removeGroupMember)

	mux.HandleFunc("GET "+s.base+"api/v1/audit", s.listAudit)
	mux.HandleFunc("POST "+s.base+"api/v1/api-keys", s.createAPIKey)

	mux.HandleFunc("GET "+s.base+"api/v1/ai/status", s.aiStatus)
	mux.HandleFunc("GET "+s.base+"api/v1/ai/conversations", s.listAIConversations)
	mux.HandleFunc("GET "+s.base+"api/v1/ai/conversations/{id}", s.getAIConversation)
	mux.HandleFunc("POST "+s.base+"api/v1/ai/chat", s.aiChat)
	mux.HandleFunc("GET "+s.base+"api/v1/ai/approvals", s.listAIApprovals)
	mux.HandleFunc("POST "+s.base+"api/v1/ai/approvals/{id}/approve", s.approveAIPlan)
	mux.HandleFunc("POST "+s.base+"api/v1/ai/approvals/{id}/reject", s.rejectAIPlan)

	mux.HandleFunc("POST "+s.base+"api/v1/config/format", s.formatConfigFile)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/files", s.listFiles)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/files/content", s.readFile)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/files/nbt", s.readNBT)
	mux.HandleFunc("PUT "+s.base+"api/v1/servers/{id}/files/content", s.writeFile)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/files/revisions", s.listFileRevisions)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/files/revisions/{rev}", s.getFileRevision)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/files/rollback", s.rollbackFile)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/files/diff", s.diffFile)

	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/metrics", s.getServerMetrics)
	mux.HandleFunc("GET "+s.base+"api/v1/servers/{id}/bridge", s.getServerBridge)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/moderation", s.moderatePlayer)
	mux.HandleFunc("POST "+s.base+"api/v1/servers/{id}/command", s.runServerCommand)

	mux.HandleFunc("GET "+s.base+"api/v1/users", s.listUsers)
	mux.HandleFunc("POST "+s.base+"api/v1/users", s.createUser)
	mux.HandleFunc("PATCH "+s.base+"api/v1/users/{id}/role", s.setUserRole)

	mux.HandleFunc("GET "+s.base+"api/v1/templates", s.listTemplates)
	mux.HandleFunc("POST "+s.base+"api/v1/templates", s.importTemplate)
	mux.HandleFunc("GET "+s.base+"api/v1/templates/{id}", s.getTemplate)
	mux.HandleFunc("PUT "+s.base+"api/v1/templates/{id}", s.updateTemplate)

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
