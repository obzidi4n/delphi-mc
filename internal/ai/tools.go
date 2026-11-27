package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

// Citation is a source shown next to an answer.
type Citation struct {
	Kind  string `json:"kind"` // doc | server
	Ref   string `json:"ref"`
	Title string `json:"title"`
}

// ServerInfo is the read view tools return.
type ServerInfo struct {
	ID         string
	Name       string
	NodeID     string
	State      string
	Base       string
	TemplateID string
	Port       int
	LastError  string
}

// HealthInfo combines metrics and bridge facts for one server.
type HealthInfo struct {
	State       string
	TPS         float64
	Players     int
	CPUPercent  float64
	MemoryBytes int64
	HasBridge   bool
	HasMetrics  bool
}

// ToolResult is what a tool returns to the model and the UI.
type ToolResult struct {
	Text      string
	Citations []Citation
}

// Toolbox is the set of read-only operations the assistant may call. The API
// wires these to the capability layer and the console/metrics hub.
type Toolbox struct {
	SearchDocs  func(ctx context.Context, query string, limit int) ([]store.DocChunk, error)
	ListServers func(ctx context.Context, p capability.Principal) ([]ServerInfo, error)
	GetServer   func(ctx context.Context, p capability.Principal, ref string) (ServerInfo, error)
	Health      func(ctx context.Context, p capability.Principal, ref string) (HealthInfo, error)
	ConsoleTail func(ctx context.Context, p capability.Principal, ref string, lines int) ([]string, error)
	Plugins     func(ctx context.Context, p capability.Principal, ref string) ([]store.ServerPlugin, error)
	Updates     func(ctx context.Context, p capability.Principal, ref string) ([]capability.PluginUpdate, error)
}

// Tool is one executable tool.
type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
	Write       bool
	Run         func(ctx context.Context, p capability.Principal, args json.RawMessage) (ToolResult, error)
}

var (
	searchSchema  = json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"integer"}},"required":["query"]}`)
	emptySchema   = json.RawMessage(`{"type":"object","properties":{}}`)
	serverSchema  = json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Server name or id"}},"required":["server"]}`)
	consoleSchema = json.RawMessage(`{"type":"object","properties":{"server":{"type":"string"},"lines":{"type":"integer"}},"required":["server"]}`)
)

// Tools returns the tools the principal may see. Org read gates everything
// except documentation search; server scopes are still enforced by the
// capability layer when a tool resolves a server.
func (tb *Toolbox) Tools(p capability.Principal) []Tool {
	tools := []Tool{{
		Name:        "search_docs",
		Description: "Search DelphiMC documentation (architecture, operations, reference) and return ranked sections with paths.",
		Parameters:  searchSchema,
		Run:         tb.runSearchDocs,
	}}
	if !p.Allows("org:" + p.OrgID + ":read") {
		return tools
	}
	return append(tools,
		Tool{Name: "list_servers", Description: "List servers with state, port and node id.", Parameters: emptySchema, Run: tb.runListServers},
		Tool{Name: "get_server", Description: "Get one server by name or id: state, template, port, node, last error.", Parameters: serverSchema, Run: tb.runGetServer},
		Tool{Name: "get_server_health", Description: "Get live TPS, player count, CPU and memory for one server.", Parameters: serverSchema, Run: tb.runHealth},
		Tool{Name: "get_console_tail", Description: "Read the most recent console lines of one server (for diagnosing lag or crashes).", Parameters: consoleSchema, Run: tb.runConsoleTail},
		Tool{Name: "plugins", Description: "List managed plugins for one server and any available updates.", Parameters: serverSchema, Run: tb.runPlugins},
	)
}

func (tb *Toolbox) runSearchDocs(ctx context.Context, p capability.Principal, args json.RawMessage) (ToolResult, error) {
	if tb.SearchDocs == nil {
		return ToolResult{}, fmt.Errorf("documentation search is unavailable")
	}
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return ToolResult{}, fmt.Errorf("query is required")
	}
	if in.Limit <= 0 || in.Limit > 10 {
		in.Limit = 5
	}
	hits, err := tb.SearchDocs(ctx, in.Query, in.Limit)
	if err != nil {
		return ToolResult{}, err
	}
	if len(hits) == 0 {
		return ToolResult{Text: "No documentation sections matched that query."}, nil
	}
	var b strings.Builder
	var cites []Citation
	for i, hit := range hits {
		fmt.Fprintf(&b, "[%d] %s — %s\n%s\n\n", i+1, hit.Path, hit.Heading, truncate(hit.Content, 900))
		cites = append(cites, Citation{Kind: "doc", Ref: hit.Path, Title: hit.Heading})
	}
	return ToolResult{Text: b.String(), Citations: cites}, nil
}

func (tb *Toolbox) runListServers(ctx context.Context, p capability.Principal, _ json.RawMessage) (ToolResult, error) {
	if tb.ListServers == nil {
		return ToolResult{}, fmt.Errorf("server listing is unavailable")
	}
	servers, err := tb.ListServers(ctx, p)
	if err != nil {
		return ToolResult{}, err
	}
	if len(servers) == 0 {
		return ToolResult{Text: "No servers exist yet."}, nil
	}
	var b strings.Builder
	var cites []Citation
	for _, s := range servers {
		fmt.Fprintf(&b, "- %s (id %s): state=%s port=%d node=%s base=%s\n", s.Name, s.ID, s.State, s.Port, s.NodeID, s.Base)
		cites = append(cites, Citation{Kind: "server", Ref: s.ID, Title: s.Name})
	}
	return ToolResult{Text: b.String(), Citations: cites}, nil
}

func (tb *Toolbox) runGetServer(ctx context.Context, p capability.Principal, args json.RawMessage) (ToolResult, error) {
	if tb.GetServer == nil {
		return ToolResult{}, fmt.Errorf("server lookup is unavailable")
	}
	ref, err := serverRef(args)
	if err != nil {
		return ToolResult{}, err
	}
	info, err := tb.GetServer(ctx, p, ref)
	if err != nil {
		return ToolResult{}, err
	}
	text := fmt.Sprintf("name=%s id=%s state=%s base=%s template=%s port=%d node=%s\nlast_error=%s\n",
		info.Name, info.ID, info.State, info.Base, info.TemplateID, info.Port, info.NodeID, orNone(info.LastError))
	return ToolResult{Text: text, Citations: []Citation{{Kind: "server", Ref: info.ID, Title: info.Name}}}, nil
}

func (tb *Toolbox) runHealth(ctx context.Context, p capability.Principal, args json.RawMessage) (ToolResult, error) {
	if tb.Health == nil {
		return ToolResult{}, fmt.Errorf("health data is unavailable")
	}
	ref, err := serverRef(args)
	if err != nil {
		return ToolResult{}, err
	}
	h, err := tb.Health(ctx, p, ref)
	if err != nil {
		return ToolResult{}, err
	}
	text := fmt.Sprintf("state=%s\n", h.State)
	if h.HasBridge {
		text += fmt.Sprintf("tps=%.2f players=%d\n", h.TPS, h.Players)
	} else {
		text += "bridge=unavailable (plugin not reporting)\n"
	}
	if h.HasMetrics {
		text += fmt.Sprintf("cpu_percent=%.2f memory_bytes=%d\n", h.CPUPercent, h.MemoryBytes)
	} else {
		text += "metrics=unavailable\n"
	}
	return ToolResult{Text: text, Citations: []Citation{{Kind: "server", Ref: ref, Title: ref}}}, nil
}

func (tb *Toolbox) runConsoleTail(ctx context.Context, p capability.Principal, args json.RawMessage) (ToolResult, error) {
	if tb.ConsoleTail == nil {
		return ToolResult{}, fmt.Errorf("console history is unavailable")
	}
	var in struct {
		Server string `json:"server"`
		Lines  int    `json:"lines"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Server) == "" {
		return ToolResult{}, fmt.Errorf("server is required")
	}
	if in.Lines <= 0 || in.Lines > 200 {
		in.Lines = 80
	}
	lines, err := tb.ConsoleTail(ctx, p, in.Server, in.Lines)
	if err != nil {
		return ToolResult{}, err
	}
	if len(lines) == 0 {
		return ToolResult{Text: "The console ring is empty for that server."}, nil
	}
	return ToolResult{
		Text:      strings.Join(lines, "\n"),
		Citations: []Citation{{Kind: "server", Ref: in.Server, Title: in.Server + " console"}},
	}, nil
}

func (tb *Toolbox) runPlugins(ctx context.Context, p capability.Principal, args json.RawMessage) (ToolResult, error) {
	if tb.Plugins == nil || tb.Updates == nil {
		return ToolResult{}, fmt.Errorf("plugin data is unavailable")
	}
	ref, err := serverRef(args)
	if err != nil {
		return ToolResult{}, err
	}
	installed, err := tb.Plugins(ctx, p, ref)
	if err != nil {
		return ToolResult{}, err
	}
	updates, err := tb.Updates(ctx, p, ref)
	if err != nil {
		return ToolResult{}, err
	}
	if len(installed) == 0 {
		return ToolResult{Text: "No managed plugins on that server."}, nil
	}
	var b strings.Builder
	for _, p := range installed {
		fmt.Fprintf(&b, "- %s %s (%s, policy=%s)\n", p.Project, p.Version, p.Provider, p.Policy)
	}
	if len(updates) == 0 {
		b.WriteString("No updates available.")
	} else {
		b.WriteString("Updates available:\n")
		for _, u := range updates {
			if u.Error != "" {
				fmt.Fprintf(&b, "- %s: check failed: %s\n", u.Project, u.Error)
				continue
			}
			fmt.Fprintf(&b, "- %s: %s -> %s\n", u.Project, u.Current, u.Latest)
		}
	}
	return ToolResult{Text: b.String(), Citations: []Citation{{Kind: "server", Ref: ref, Title: ref + " plugins"}}}, nil
}

func serverRef(args json.RawMessage) (string, error) {
	var in struct {
		Server string `json:"server"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Server) == "" {
		return "", fmt.Errorf("server is required")
	}
	return strings.TrimSpace(in.Server), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…[truncated]"
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
