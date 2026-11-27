package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
)

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *server) updatesService(w http.ResponseWriter) *capability.Updates {
	if s.opts.Services == nil || s.opts.Services.Updates == nil {
		writeProblem(w, http.StatusNotImplemented, "updates_disabled", "the update service is not configured")
		return nil
	}
	return s.opts.Services.Updates
}

func (s *server) listPlugins(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	items, err := service.List(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.PluginListDTO{Items: pluginDTOs(items)})
}

func (s *server) listUpdates(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	items, revision, err := service.Detect(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.PluginUpdateListDTO{
		Items: updateDTOs(items), Revision: revision,
	})
}

func (s *server) applyUpdates(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	result, err := service.Apply(r.Context(), principal, r.PathValue("id"))
	switch {
	case errors.Is(err, capability.ErrNoUpdates):
		writeProblem(w, http.StatusConflict, "no_updates", "no updates are available")
		return
	case err != nil:
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.PluginUpdateApplyDTO{
		Applied: updateDTOs(result.Applied), Revision: result.Revision, BootCheck: result.BootCheck,
	})
}

func updateDTOs(in []capability.PluginUpdate) []apitypes.PluginUpdate {
	out := make([]apitypes.PluginUpdate, 0, len(in))
	for _, u := range in {
		out = append(out, apitypes.PluginUpdate{
			Project: u.Project, Provider: u.Provider, Current: u.Current,
			Latest: strPtr(u.Latest), Error: strPtr(u.Error),
		})
	}
	return out
}

func (s *server) gcArtifacts(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	age := 3600
	if raw := r.URL.Query().Get("min_age_seconds"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 30*24*3600 {
			writeProblem(w, http.StatusBadRequest, "invalid_min_age", "min_age_seconds must be 0..2592000")
			return
		}
		age = n
	}
	result, err := service.GC(r.Context(), principal, time.Duration(age)*time.Second)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.GCResultDTO{
		Removed: result.Removed, FreedBytes: result.FreedByte,
	})
}

// agentBlob serves content-addressed plugin jars to authenticated nodes.
func (s *server) agentBlob(w http.ResponseWriter, r *http.Request) {
	if s.opts.Artifacts == nil || s.opts.Nodes == nil {
		writeProblem(w, http.StatusNotImplemented, "artifacts_disabled", "the artifact store is not configured")
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 {
		writeProblem(w, http.StatusForbidden, "client_cert_required", "a node certificate is required")
		return
	}
	leaf := r.TLS.PeerCertificates[0]
	node, err := s.opts.Nodes.NodeBySerial(r.Context(), leaf.SerialNumber.String())
	if err != nil || node.RevokedAt != nil {
		writeProblem(w, http.StatusForbidden, "node_unknown", "unknown or revoked node certificate")
		return
	}

	sha := strings.TrimSpace(r.PathValue("sha"))
	if len(sha) != 64 {
		writeProblem(w, http.StatusBadRequest, "invalid_artifact", "sha256 must be 64 hex characters")
		return
	}
	path, err := s.opts.Artifacts.Path(sha)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_artifact", err.Error())
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "artifact_not_found", "no such artifact")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "artifact_unreadable", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/java-archive")
	http.ServeContent(w, r, sha, info.ModTime(), file)
}
