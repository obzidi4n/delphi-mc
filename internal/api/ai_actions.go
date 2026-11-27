package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"delphi.dev/delphi/internal/ai"
	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/wire"
)

// AuditFunc mirrors store.DB.Audit.
type AuditFunc func(ctx context.Context, orgID, actor, via, action, target string, diff any) error

// NewActionExecutor previews and performs approved AI actions.
func NewActionExecutor(services *capability.Services, hub *Hub, audit AuditFunc, files *FileService) ai.ActionExecutor {
	return &actionExecutor{services: services, hub: hub, audit: audit, files: files}
}

const maxEditableFile = 1 << 20

type actionExecutor struct {
	services *capability.Services
	hub      *Hub
	audit    AuditFunc
	files    *FileService
}

func (e *actionExecutor) resolve(ctx context.Context, p capability.Principal, ref string) (store.Server, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return store.Server{}, fmt.Errorf("server is required")
	}
	if strings.HasPrefix(ref, "s_") {
		return e.services.Servers.Get(ctx, p, ref)
	}
	servers, _, err := e.services.Servers.List(ctx, p, 200, "")
	if err != nil {
		return store.Server{}, err
	}
	for _, srv := range servers {
		if strings.EqualFold(srv.Name, ref) {
			return e.services.Servers.Get(ctx, p, srv.ID)
		}
	}
	return store.Server{}, store.ErrNoServer
}

// Preview validates a write tool call and expands it into concrete actions.
func (e *actionExecutor) Preview(ctx context.Context, p capability.Principal, tool string, args json.RawMessage) ([]ai.Action, error) {
	switch tool {
	case "power":
		var in struct {
			Action string `json:"action"`
			Server string `json:"server"`
			Group  string `json:"group"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		switch in.Action {
		case "start", "stop", "restart", "kill":
		default:
			return nil, fmt.Errorf("action must be start, stop, restart, or kill")
		}
		if in.Server != "" && in.Group != "" {
			return nil, fmt.Errorf("set server or group, not both")
		}
		if in.Group != "" {
			groups, _, err := e.services.Groups.List(ctx, p, 200, "")
			if err != nil {
				return nil, err
			}
			var groupID string
			for _, g := range groups {
				if strings.EqualFold(g.Name, in.Group) {
					groupID = g.ID
					break
				}
			}
			if groupID == "" {
				return nil, fmt.Errorf("no group named %q", in.Group)
			}
			ids, _, err := e.services.Groups.Members(ctx, p, groupID, 200, "")
			if err != nil {
				return nil, err
			}
			if len(ids) == 0 {
				return nil, fmt.Errorf("group %q has no servers", in.Group)
			}
			actions := make([]ai.Action, 0, len(ids))
			for _, id := range ids {
				srv, err := e.services.Servers.Get(ctx, p, id)
				if err != nil {
					return nil, err
				}
				actions = append(actions, powerAction(srv, in.Action))
			}
			return actions, nil
		}
		srv, err := e.resolve(ctx, p, in.Server)
		if err != nil {
			return nil, err
		}
		return []ai.Action{powerAction(srv, in.Action)}, nil
	case "create_server":
		var in struct {
			Template string `json:"template"`
			Name     string `json:"name"`
			Node     string `json:"node"`
			MemoryMB int    `json:"memory_mb"`
			Port     int    `json:"port"`
			Desired  string `json:"desired_state"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		if in.Template == "" || in.Name == "" || in.Node == "" {
			return nil, fmt.Errorf("template, name and node are required")
		}
		tpl, row, err := e.services.Templates.Get(ctx, p, in.Template)
		if err != nil {
			return nil, err
		}
		desired := in.Desired
		if desired != "running" {
			desired = "stopped"
		}
		return []ai.Action{{
			Tool: "create_server", Summary: fmt.Sprintf("create %q from %s on %s", in.Name, tpl.Name, in.Node),
			TemplateID: row.ID, NewName: in.Name, NodeID: in.Node,
			MemoryMB: in.MemoryMB, Port: in.Port, Desired: desired, AcceptEULA: true,
		}}, nil
	case "run_command":
		var in struct {
			Server  string `json:"server"`
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Command), "/"))
		if command == "" || len(command) > 512 {
			return nil, fmt.Errorf("command must be 1-512 characters")
		}
		srv, err := e.resolve(ctx, p, in.Server)
		if err != nil {
			return nil, err
		}
		return []ai.Action{{
			Tool: "run_command", Summary: fmt.Sprintf("run %q on %s", command, srv.Name),
			ServerID: srv.ID, ServerName: srv.Name, Command: command,
		}}, nil
	case "write_file":
		var in struct {
			Server  string `json:"server"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		path := strings.TrimSpace(in.Path)
		if path == "" || len(path) > 255 || strings.HasPrefix(path, "/") || strings.Contains(path, "..") {
			return nil, fmt.Errorf("path must be a relative path inside the server directory")
		}
		if len(in.Content) > maxEditableFile {
			return nil, fmt.Errorf("content is larger than %d bytes", maxEditableFile)
		}
		srv, err := e.resolve(ctx, p, in.Server)
		if err != nil {
			return nil, err
		}
		return []ai.Action{{
			Tool: "write_file", Summary: fmt.Sprintf("write %s on %s (%d bytes)", path, srv.Name, len(in.Content)),
			ServerID: srv.ID, ServerName: srv.Name, Path: path, Content: in.Content,
		}}, nil
	case "moderate_player":
		var in struct {
			Action string `json:"action"`
			Server string `json:"server"`
			Player string `json:"player"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		action := strings.ToLower(strings.TrimSpace(in.Action))
		if action != "kick" && action != "ban" {
			return nil, fmt.Errorf("action must be kick or ban")
		}
		player := strings.TrimSpace(in.Player)
		if player == "" || len(player) > 64 {
			return nil, fmt.Errorf("player must be 1-64 characters")
		}
		srv, err := e.resolve(ctx, p, in.Server)
		if err != nil {
			return nil, err
		}
		return []ai.Action{{
			Tool: "moderate_player", Summary: fmt.Sprintf("%s %s on %s", action, player, srv.Name),
			ServerID: srv.ID, ServerName: srv.Name, Action: action, Player: player, Reason: in.Reason,
		}}, nil
	default:
		return nil, fmt.Errorf("tool %q is not an action", tool)
	}
}

// Execute performs one approved action as the user and audits it.
func (e *actionExecutor) Execute(ctx context.Context, p capability.Principal, action ai.Action) (string, error) {
	var out string
	switch action.Tool {
	case "power":
		srv, err := e.services.Servers.Power(ctx, p, action.ServerID, action.Action)
		if err != nil {
			return "", err
		}
		out = fmt.Sprintf("accepted, revision %d", srv.DesiredRevision)
	case "create_server":
		overrides := map[string]string{}
		if action.MemoryMB > 0 {
			overrides["memory_mb"] = fmt.Sprint(action.MemoryMB)
		}
		if action.Port > 0 {
			overrides["port"] = fmt.Sprint(action.Port)
		}
		srv, err := e.services.Templates.CreateServer(ctx, p, capability.InstantiateRequest{
			Name: action.NewName, NodeID: action.NodeID, TemplateID: action.TemplateID,
			Overrides: overrides, DesiredState: action.Desired, AcceptEULA: action.AcceptEULA,
		})
		if err != nil {
			return "", err
		}
		out = fmt.Sprintf("created %s (%s)", srv.Name, srv.ID)
	case "run_command":
		srv, err := e.resolve(ctx, p, action.ServerID)
		if err != nil {
			return "", err
		}
		if !auth.Allows(p.Scopes, "server:"+srv.ID+":moderate") {
			return "", fmt.Errorf("missing scope server:%s:moderate", srv.ID)
		}
		if err := e.sendBridge(ctx, srv.NodeID, wire.BridgeCommandFrame{
			Type: wire.TypeBridgeCommand, ServerID: srv.ID, ID: newActionID(),
			Action: "command", Command: action.Command,
		}); err != nil {
			return "", err
		}
		out = "command queued for " + srv.Name
	case "write_file":
		if e.files == nil {
			return "", fmt.Errorf("the file manager is not configured")
		}
		srv, err := e.resolve(ctx, p, action.ServerID)
		if err != nil {
			return "", err
		}
		if !auth.Allows(p.Scopes, "server:"+srv.ID+":update") {
			return "", fmt.Errorf("missing scope server:%s:update", srv.ID)
		}
		_, revision, err := e.files.Write(ctx, p, srv, action.Path, action.Content, "", "file.write")
		if err != nil {
			return "", err
		}
		out = fmt.Sprintf("wrote %s (revision %d)", action.Path, revision)
	case "moderate_player":
		srv, err := e.resolve(ctx, p, action.ServerID)
		if err != nil {
			return "", err
		}
		if !auth.Allows(p.Scopes, "server:"+srv.ID+":moderate") {
			return "", fmt.Errorf("missing scope server:%s:moderate", srv.ID)
		}
		if err := e.sendBridge(ctx, srv.NodeID, wire.BridgeCommandFrame{
			Type: wire.TypeBridgeCommand, ServerID: srv.ID, ID: newActionID(),
			Action: action.Action, Player: action.Player, Reason: action.Reason,
		}); err != nil {
			return "", err
		}
		out = fmt.Sprintf("%s queued for %s", action.Action, action.Player)
	default:
		return "", fmt.Errorf("unknown action %q", action.Tool)
	}
	if e.audit != nil {
		target := action.ServerID
		if target == "" {
			target = action.NewName
		}
		_ = e.audit(ctx, p.OrgID, actorName(p), "ai", "ai."+action.Tool, target,
			map[string]string{"summary": action.Summary, "result": out})
	}
	return out, nil
}

func (e *actionExecutor) sendBridge(ctx context.Context, nodeID string, frame wire.BridgeCommandFrame) error {
	if e.hub == nil {
		return fmt.Errorf("the agent channel is not configured")
	}
	if err := e.hub.Send(ctx, nodeID, frame); err != nil {
		if err == ErrNodeOffline {
			return fmt.Errorf("the server's node is not connected")
		}
		return err
	}
	return nil
}

func powerAction(srv store.Server, action string) ai.Action {
	return ai.Action{
		Tool: "power", Summary: fmt.Sprintf("%s %s", action, srv.Name),
		ServerID: srv.ID, ServerName: srv.Name, Action: action,
	}
}

func newActionID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		panic("api: crypto/rand failed: " + err.Error())
	}
	return "c_" + hex.EncodeToString(raw)
}

func actorName(p capability.Principal) string {
	if p.UserID != "" {
		return p.UserID
	}
	return "bootstrap"
}
