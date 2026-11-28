package api

import (
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
)

func (s *server) devService(w http.ResponseWriter) *capability.Dev {
	if s.opts.Services == nil || s.opts.Services.Dev == nil {
		writeProblem(w, http.StatusNotImplemented, "dev_disabled", "dev environments are not configured")
		return nil
	}
	return s.opts.Services.Dev
}

func (s *server) listDevServers(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.devService(w)
	if service == nil {
		return
	}
	items, err := service.List(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.ServerDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toServerDTO(item))
	}
	writeJSON(w, http.StatusOK, apitypes.ServerListDTO{Items: out, NextCursor: nil})
}

func (s *server) createDevServer(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.devService(w)
	if service == nil {
		return
	}
	var body apitypes.DevCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	fresh := false
	if body.FreshWorld != nil {
		fresh = *body.FreshWorld
	}
	result, err := service.Create(r.Context(), principal, r.PathValue("id"), body.OwnerUsername, fresh)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toServerDTO(result.Server))
}
