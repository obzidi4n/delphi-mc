package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

// Action is one concrete, reviewable mutation.
type Action struct {
	Tool       string `json:"tool"`
	Summary    string `json:"summary"`
	ServerID   string `json:"server_id,omitempty"`
	ServerName string `json:"server_name,omitempty"`
	Action     string `json:"action,omitempty"`
	Command    string `json:"command,omitempty"`
	Path       string `json:"path,omitempty"`
	Content    string `json:"content,omitempty"`
	Player     string `json:"player,omitempty"`
	Reason     string `json:"reason,omitempty"`
	TemplateID string `json:"template_id,omitempty"`
	NewName    string `json:"new_name,omitempty"`
	NodeID     string `json:"node_id,omitempty"`
	Desired    string `json:"desired_state,omitempty"`
	MemoryMB   int    `json:"memory_mb,omitempty"`
	Port       int    `json:"port,omitempty"`
	AcceptEULA bool   `json:"accept_eula,omitempty"`
}

// ProposedCall is one tool call awaiting approval, with its concrete actions.
type ProposedCall struct {
	CallID    string          `json:"call_id"`
	Tool      string          `json:"tool"`
	Summary   string          `json:"summary"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Actions   []Action        `json:"actions"`
}

// Plan is everything one assistant turn proposed.
type Plan struct {
	Calls []ProposedCall `json:"calls"`
}

// ApprovalInfo is what the UI and the service exchange.
type ApprovalInfo struct {
	ID        string         `json:"id"`
	Status    string         `json:"status"`
	Calls     []ProposedCall `json:"calls"`
	CreatedAt time.Time      `json:"created_at"`
}

// ActionExecutor previews and performs approved mutations as the user.
type ActionExecutor interface {
	Preview(ctx context.Context, p capability.Principal, tool string, args json.RawMessage) ([]Action, error)
	Execute(ctx context.Context, p capability.Principal, action Action) (string, error)
}

// ApprovalStore persists two-phase plans.
type ApprovalStore interface {
	CreateApproval(ctx context.Context, orgID, userID, conversationID string, plan []byte) (store.Approval, error)
	Approval(ctx context.Context, orgID, userID, id string) (store.Approval, error)
	PendingApprovals(ctx context.Context, orgID, userID, conversationID string) ([]store.Approval, error)
	SetApprovalStatus(ctx context.Context, id, status string) error
	SetApprovalResults(ctx context.Context, id string, results []byte) error
}

var (
	powerSchema     = json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["start","stop","restart","kill"]},"server":{"type":"string"},"group":{"type":"string"}},"required":["action"]}`)
	createSchema    = json.RawMessage(`{"type":"object","properties":{"template":{"type":"string"},"name":{"type":"string"},"node":{"type":"string"},"memory_mb":{"type":"integer"},"port":{"type":"integer"},"desired_state":{"type":"string"}},"required":["template","name","node"]}`)
	runSchema       = json.RawMessage(`{"type":"object","properties":{"server":{"type":"string"},"command":{"type":"string"}},"required":["server","command"]}`)
	moderateSchema  = json.RawMessage(`{"type":"object","properties":{"action":{"type":"string","enum":["kick","ban"]},"server":{"type":"string"},"player":{"type":"string"},"reason":{"type":"string"}},"required":["action","server","player"]}`)
	writeFileSchema = json.RawMessage(`{"type":"object","properties":{"server":{"type":"string"},"path":{"type":"string"},"content":{"type":"string"}},"required":["server","path","content"]}`)
	updateSchema    = json.RawMessage(`{"type":"object","properties":{"server":{"type":"string"}},"required":["server"]}`)
)

// writeTools are proposed to the model; the service pauses for approval
// instead of running them.
func writeTools() []Tool {
	return []Tool{
		{Name: "create_server", Write: true, Description: "Propose creating a server from a template. Requires approval.", Parameters: createSchema,
			Run: func(context.Context, capability.Principal, json.RawMessage) (ToolResult, error) {
				return ToolResult{}, fmt.Errorf("approval required")
			}},
		{Name: "power", Write: true, Description: "Propose a power action (start/stop/restart/kill) for one server or a group. Requires approval.", Parameters: powerSchema,
			Run: func(context.Context, capability.Principal, json.RawMessage) (ToolResult, error) {
				return ToolResult{}, fmt.Errorf("approval required")
			}},
		{Name: "run_command", Write: true, Description: "Propose a console command for a server. Requires approval.", Parameters: runSchema,
			Run: func(context.Context, capability.Principal, json.RawMessage) (ToolResult, error) {
				return ToolResult{}, fmt.Errorf("approval required")
			}},
		{Name: "write_file", Write: true, Description: "Propose writing a text file under the server directory. Requires approval.", Parameters: writeFileSchema,
			Run: func(context.Context, capability.Principal, json.RawMessage) (ToolResult, error) {
				return ToolResult{}, fmt.Errorf("approval required")
			}},
		{Name: "update_server", Write: true, Description: "Propose installing available plugin updates and boot-checking the restart. Requires approval.", Parameters: updateSchema,
			Run: func(context.Context, capability.Principal, json.RawMessage) (ToolResult, error) {
				return ToolResult{}, fmt.Errorf("approval required")
			}},
		{Name: "moderate_player", Write: true, Description: "Propose kicking or banning a player through the bridge. Requires approval.", Parameters: moderateSchema,
			Run: func(context.Context, capability.Principal, json.RawMessage) (ToolResult, error) {
				return ToolResult{}, fmt.Errorf("approval required")
			}},
	}
}

func actionsSummary(tool string, actions []Action) string {
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		parts = append(parts, action.Summary)
	}
	if len(parts) == 0 {
		return tool + " (no actions)"
	}
	return strings.Join(parts, "; ")
}
