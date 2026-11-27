package api

import (
	"errors"
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

func toServerDTO(sv store.Server) apitypes.ServerDTO {
	var lastErr *string
	if sv.LastError != "" {
		lastErr = &sv.LastError
	}
	return apitypes.ServerDTO{
		ID: sv.ID, Name: sv.Name, NodeID: sv.NodeID,
		DesiredState: sv.DesiredState, DesiredRevision: sv.DesiredRevision,
		ObservedState: sv.ObservedState, SummaryState: sv.SummaryState,
		AppliedRevision: sv.AppliedRevision,
		KillRequested:   sv.KillRequested, Port: sv.Port, Image: sv.Image,
		JavaVersion: sv.JavaVersion, MemoryMB: sv.MemoryMB, CPUCores: sv.CPUCores,
		LastError: lastErr, CreatedAt: sv.CreatedAt,
		DeletedAt: sv.DeletedAt, PurgeAfter: sv.PurgeAfter,
	}
}

func (s *server) serverService(w http.ResponseWriter) *capability.Servers {
	if s.opts.Services == nil || s.opts.Services.Servers == nil {
		writeProblem(w, http.StatusNotImplemented, "servers_disabled", "the capability layer is not configured")
		return nil
	}
	return s.opts.Services.Servers
}

func (s *server) createServer(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}

	var req struct {
		Name         string  `json:"name"`
		NodeID       string  `json:"node_id"`
		Image        string  `json:"image"`
		MemoryMB     int     `json:"memory_mb"`
		CPUCores     float64 `json:"cpu_cores"`
		Port         int     `json:"port"`
		JarURL       string  `json:"jar_url"`
		JarSHA256    string  `json:"jar_sha256"`
		AcceptEULA   bool    `json:"accept_eula"`
		GroupID      string  `json:"group_id"`
		DesiredState string  `json:"desired_state"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	created, err := service.Create(r.Context(), principal, capability.CreateRequest{
		Name: req.Name, NodeID: req.NodeID, Image: req.Image, MemoryMB: req.MemoryMB,
		CPUCores: req.CPUCores, Port: req.Port, JarURL: req.JarURL,
		JarSHA256: req.JarSHA256, AcceptEULA: req.AcceptEULA, GroupID: req.GroupID,
		DesiredState: req.DesiredState,
	})
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toServerDTO(created))
}

func (s *server) listServers(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}

	servers, err := service.List(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	items := make([]apitypes.ServerDTO, 0, len(servers))
	for _, srv := range servers {
		items = append(items, toServerDTO(srv))
	}
	writeJSON(w, http.StatusOK, apitypes.ServerListDTO{Items: items})
}

func (s *server) getServer(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}

	srv, err := service.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toServerDTO(srv))
}

func (s *server) renameServer(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}

	var body apitypes.RenameRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	updated, err := service.Rename(r.Context(), principal, r.PathValue("id"), body.Name)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toServerDTO(updated))
}

func (s *server) deleteServer(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}

	srv, err := service.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	if r.URL.Query().Get("confirm") != srv.Name {
		writeProblem(w, http.StatusBadRequest, "confirm_required",
			"add ?confirm=<server name> to delete a server")
		return
	}

	deleted, err := service.Delete(r.Context(), principal, srv.ID)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toServerDTO(deleted))
}

func (s *server) powerServer(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.serverService(w)
	if service == nil {
		return
	}

	var body apitypes.PowerRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	srv, err := service.Power(r.Context(), principal, r.PathValue("id"), body.Action)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toServerDTO(srv))
}

func writeCapabilityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, capability.ErrUnauthorized):
		writeProblem(w, http.StatusUnauthorized, "unauthorized", err.Error())
	case errors.Is(err, capability.ErrForbidden):
		writeProblem(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, capability.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, capability.ErrLastOwner):
		writeProblem(w, http.StatusConflict, "last_owner", err.Error())
	case errors.Is(err, store.ErrNoServer):
		writeProblem(w, http.StatusNotFound, "server_not_found", "no such server")
	case errors.Is(err, store.ErrNoUser):
		writeProblem(w, http.StatusNotFound, "user_not_found", "no such user")
	case errors.Is(err, store.ErrEmailInUse):
		writeProblem(w, http.StatusConflict, "email_in_use", "that email is already registered")
	case errors.Is(err, store.ErrNoNode):
		writeProblem(w, http.StatusBadRequest, "node_not_found", "no such node")
	case errors.Is(err, store.ErrPortInUse):
		writeProblem(w, http.StatusConflict, "port_in_use", "that port is already allocated on the node")
	case errors.Is(err, store.ErrNameInUse):
		writeProblem(w, http.StatusConflict, "name_in_use", "that name is already used on the node")
	case errors.Is(err, store.ErrNoPort):
		writeProblem(w, http.StatusConflict, "no_free_port", "the node has no free port in the pool")
	case errors.Is(err, store.ErrNoGroup):
		writeProblem(w, http.StatusNotFound, "group_not_found", "no such group")
	case errors.Is(err, store.ErrGroupInUse):
		writeProblem(w, http.StatusConflict, "group_in_use", "that group name is already used")
	default:
		writeProblem(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}
