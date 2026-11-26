package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

type serverDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	NodeID          string    `json:"node_id"`
	DesiredState    string    `json:"desired_state"`
	DesiredRevision int64     `json:"desired_revision"`
	ObservedState   string    `json:"observed_state"`
	AppliedRevision int64     `json:"applied_revision"`
	KillRequested   bool      `json:"kill_requested"`
	Port            int       `json:"port"`
	Image           string    `json:"image"`
	JavaVersion     int       `json:"java_version"`
	MemoryMB        int       `json:"memory_mb"`
	CPUCores        float64   `json:"cpu_cores"`
	LastError       string    `json:"last_error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

func toServerDTO(s store.Server) serverDTO {
	return serverDTO{
		ID:              s.ID,
		Name:            s.Name,
		NodeID:          s.NodeID,
		DesiredState:    s.DesiredState,
		DesiredRevision: s.DesiredRevision,
		ObservedState:   s.ObservedState,
		AppliedRevision: s.AppliedRevision,
		KillRequested:   s.KillRequested,
		Port:            s.Port,
		Image:           s.Image,
		JavaVersion:     s.JavaVersion,
		MemoryMB:        s.MemoryMB,
		CPUCores:        s.CPUCores,
		LastError:       s.LastError,
		CreatedAt:       s.CreatedAt,
	}
}

func (s *server) createServer(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "admin token required")
		return
	}
	if s.opts.Servers == nil {
		writeProblem(w, http.StatusNotImplemented, "servers_disabled", "the capability layer is not configured")
		return
	}

	var req struct {
		Name       string  `json:"name"`
		NodeID     string  `json:"node_id"`
		Image      string  `json:"image"`
		MemoryMB   int     `json:"memory_mb"`
		CPUCores   float64 `json:"cpu_cores"`
		Port       int     `json:"port"`
		JarURL     string  `json:"jar_url"`
		JarSHA256  string  `json:"jar_sha256"`
		AcceptEULA bool    `json:"accept_eula"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	created, err := s.opts.Servers.Create(r.Context(), capability.Principal{Via: "api"}, capability.CreateRequest{
		Name:       req.Name,
		NodeID:     req.NodeID,
		Image:      req.Image,
		MemoryMB:   req.MemoryMB,
		CPUCores:   req.CPUCores,
		Port:       req.Port,
		JarURL:     req.JarURL,
		JarSHA256:  req.JarSHA256,
		AcceptEULA: req.AcceptEULA,
	})
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toServerDTO(created))
}

func (s *server) listServers(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "admin token required")
		return
	}
	if s.opts.Servers == nil {
		writeProblem(w, http.StatusNotImplemented, "servers_disabled", "the capability layer is not configured")
		return
	}

	servers, err := s.opts.Servers.List(r.Context(), capability.Principal{Via: "api"})
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	items := make([]serverDTO, 0, len(servers))
	for _, srv := range servers {
		items = append(items, toServerDTO(srv))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) getServer(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "admin token required")
		return
	}
	if s.opts.Servers == nil {
		writeProblem(w, http.StatusNotImplemented, "servers_disabled", "the capability layer is not configured")
		return
	}

	srv, err := s.opts.Servers.Get(r.Context(), capability.Principal{Via: "api"}, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toServerDTO(srv))
}

func (s *server) powerServer(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "admin token required")
		return
	}
	if s.opts.Servers == nil {
		writeProblem(w, http.StatusNotImplemented, "servers_disabled", "the capability layer is not configured")
		return
	}

	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	srv, err := s.opts.Servers.Power(r.Context(), capability.Principal{Via: "api"}, r.PathValue("id"), body.Action)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toServerDTO(srv))
}

func writeCapabilityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, capability.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, store.ErrNoServer):
		writeProblem(w, http.StatusNotFound, "server_not_found", "no such server")
	case errors.Is(err, store.ErrNoNode):
		writeProblem(w, http.StatusBadRequest, "node_not_found", "no such node")
	case errors.Is(err, store.ErrPortInUse):
		writeProblem(w, http.StatusConflict, "port_in_use", "that port is already allocated on the node")
	case errors.Is(err, store.ErrNameInUse):
		writeProblem(w, http.StatusConflict, "name_in_use", "that name is already used on the node")
	case errors.Is(err, store.ErrNoPort):
		writeProblem(w, http.StatusConflict, "no_free_port", "the node has no free port in the pool")
	default:
		writeProblem(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}
