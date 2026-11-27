package api

import (
	"errors"
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

func (s *server) networkService(w http.ResponseWriter) *capability.Networks {
	if s.opts.Services == nil || s.opts.Services.Networks == nil {
		writeProblem(w, http.StatusNotImplemented, "networks_disabled", "the network service is not configured")
		return nil
	}
	return s.opts.Services.Networks
}

func toNetworkDTO(n store.Network) apitypes.NetworkDTO {
	return apitypes.NetworkDTO{ID: n.ID, Name: n.Name, ProxyServerID: n.ProxyServerID, Mode: n.Mode, CreatedAt: n.CreatedAt}
}

func toNetworkDetail(n store.Network, servers []store.Server) apitypes.NetworkDetailDTO {
	items := make([]apitypes.ServerDTO, 0, len(servers))
	for _, srv := range servers {
		items = append(items, toServerDTO(srv))
	}
	return apitypes.NetworkDetailDTO{
		ID: n.ID, Name: n.Name, ProxyServerID: n.ProxyServerID, Mode: n.Mode, CreatedAt: n.CreatedAt,
		Servers: items,
	}
}

func (s *server) listNetworks(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.networkService(w)
	if service == nil {
		return
	}
	items, err := service.List(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.NetworkDTO, 0, len(items))
	for _, n := range items {
		out = append(out, toNetworkDTO(n))
	}
	writeJSON(w, http.StatusOK, apitypes.NetworkListDTO{Items: out})
}

func (s *server) createNetwork(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.networkService(w)
	if service == nil {
		return
	}
	var body apitypes.NetworkCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req := capability.NetworkRequest{
		Name: body.Name, NodeID: body.NodeID, AcceptEULA: body.AcceptEULA,
	}
	if body.ProxyTemplate != nil {
		req.ProxyTemplate = *body.ProxyTemplate
	}
	if body.BackendTemplate != nil {
		req.BackendTemplate = *body.BackendTemplate
	}
	if body.Backends != nil {
		req.Backends = *body.Backends
	}
	if body.Mode != nil {
		req.Mode = *body.Mode
	}
	detail, err := service.Create(r.Context(), principal, req)
	switch {
	case errors.Is(err, store.ErrNoNetwork):
		writeProblem(w, http.StatusNotFound, "network_not_found", "no such network")
		return
	case errors.Is(err, store.ErrNameInUse):
		writeProblem(w, http.StatusConflict, "name_in_use", "that network name is already used")
		return
	case err != nil:
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toNetworkDetail(detail.Network, detail.Servers))
}

func (s *server) getNetwork(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.networkService(w)
	if service == nil {
		return
	}
	detail, err := service.Get(r.Context(), principal, r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNoNetwork):
		writeProblem(w, http.StatusNotFound, "network_not_found", "no such network")
		return
	case err != nil:
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNetworkDetail(detail.Network, detail.Servers))
}
