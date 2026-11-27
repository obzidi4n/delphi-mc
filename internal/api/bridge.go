package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/auth"
	"delphi.dev/delphi/internal/wire"
)

// getServerBridge returns the plugin's latest snapshot.
func (s *server) getServerBridge(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}
	id := r.PathValue("id")
	if _, err := service.Get(r.Context(), principal, id); err != nil {
		writeCapabilityError(w, err)
		return
	}
	if s.opts.Console == nil {
		writeProblem(w, http.StatusNotImplemented, "bridge_disabled", "the bridge is not configured")
		return
	}
	state, ok := s.opts.Console.BridgeState(id)
	if !ok {
		writeProblem(w, http.StatusConflict, "bridge_unavailable", "the server bridge has not reported yet")
		return
	}
	players := make([]apitypes.BridgePlayerDTO, 0, len(state.Players))
	for _, p := range state.Players {
		players = append(players, apitypes.BridgePlayerDTO{Name: p.Name, UUID: p.UUID})
	}
	commands := state.Commands
	if commands == nil {
		commands = []string{}
	}
	writeJSON(w, http.StatusOK, apitypes.BridgeDTO{
		TPS: state.TPS, Players: players, Commands: commands, Updated: state.Updated,
	})
}

// runServerCommand queues a console command through the server's bridge.
func (s *server) runServerCommand(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}
	var body apitypes.CommandRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if s.opts.BridgeSender == nil {
		writeProblem(w, http.StatusNotImplemented, "bridge_disabled", "the agent channel is not configured")
		return
	}
	if s.opts.Console == nil {
		writeProblem(w, http.StatusConflict, "bridge_unavailable", "the server bridge is not ready")
		return
	}
	if _, ok := s.opts.Console.BridgeState(r.PathValue("id")); !ok {
		writeProblem(w, http.StatusConflict, "bridge_unavailable", "the server bridge is not ready")
		return
	}
	cmdID, err := service.Command(r.Context(), principal, r.PathValue("id"), body.Command, s.opts.BridgeSender)
	if err != nil {
		if errors.Is(err, ErrNodeOffline) {
			writeProblem(w, http.StatusConflict, "node_offline", "the server's node is not connected")
			return
		}
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, apitypes.CommandAcceptedDTO{ID: cmdID})
}

// moderatePlayer asks the agent's bridge to kick or ban a player.
func (s *server) moderatePlayer(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}
	id := r.PathValue("id")
	var body apitypes.ModerationRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	if action != "kick" && action != "ban" {
		writeProblem(w, http.StatusBadRequest, "invalid_action", "action must be kick or ban")
		return
	}
	player := strings.TrimSpace(body.Player)
	if player == "" || len(player) > 64 {
		writeProblem(w, http.StatusBadRequest, "invalid_player", "player must be 1-64 characters")
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = strings.TrimSpace(*body.Reason)
	}
	if len(reason) > 256 {
		writeProblem(w, http.StatusBadRequest, "invalid_reason", "reason must be at most 256 characters")
		return
	}
	srv, err := service.Get(r.Context(), principal, id)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	if !auth.Allows(principal.Scopes, "server:"+id+":moderate") {
		writeProblem(w, http.StatusForbidden, "forbidden", "missing scope server:"+id+":moderate")
		return
	}
	if s.opts.BridgeSender == nil {
		writeProblem(w, http.StatusNotImplemented, "bridge_disabled", "the agent channel is not configured")
		return
	}
	cmdID := "c_" + randomHex(8)
	err = s.opts.BridgeSender(r.Context(), srv.NodeID, wire.BridgeCommandFrame{
		Type: wire.TypeBridgeCommand, ServerID: id, ID: cmdID,
		Action: action, Player: player, Reason: reason,
	})
	if errors.Is(err, ErrNodeOffline) {
		writeProblem(w, http.StatusConflict, "node_offline", "the server's node is not connected")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "bridge_unreachable", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, apitypes.ModerationAcceptedDTO{ID: cmdID})
}

func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		panic("api: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(raw)
}
