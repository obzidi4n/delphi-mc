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
	"delphi.dev/delphi/internal/store"
)

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func boolPtr(v bool) *bool { return &v }

func (s *server) updatesService(w http.ResponseWriter) *capability.Updates {
	if s.opts.Services == nil || s.opts.Services.Updates == nil {
		writeProblem(w, http.StatusNotImplemented, "updates_disabled", "the update service is not configured")
		return nil
	}
	return s.opts.Services.Updates
}

func (s *server) searchPluginCatalog(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			writeProblem(w, http.StatusBadRequest, "invalid_limit", "limit must be 1..50")
			return
		}
		limit = n
	}
	items, err := service.Catalog(r.Context(), principal,
		r.URL.Query().Get("provider"), r.URL.Query().Get("query"),
		r.URL.Query().Get("mc_version"), limit)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.PluginCatalogEntry, 0, len(items))
	for _, item := range items {
		out = append(out, apitypes.PluginCatalogEntry{
			Provider: item.Provider, Project: item.Project, Title: item.Title,
			Description: strPtr(item.Description), Author: strPtr(item.Author),
			IconURL: strPtr(item.IconURL), PageURL: strPtr(item.PageURL),
			Downloads: func() *int64 {
				if item.Downloads == 0 {
					return nil
				}
				return &item.Downloads
			}(),
			Versions: item.Versions,
		})
	}
	writeJSON(w, http.StatusOK, apitypes.PluginCatalogDTO{Items: out})
}

func (s *server) installServerPlugin(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	var body apitypes.PluginInstallRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	version, versionRange, name := "", "", ""
	if body.Version != nil {
		version = *body.Version
	}
	if body.Range != nil {
		versionRange = *body.Range
	}
	if body.Name != nil {
		name = *body.Name
	}
	result, err := service.Add(r.Context(), principal, r.PathValue("id"), capability.PluginRequest{
		Provider: body.Provider, Project: body.Project, Name: name,
		Version: version, Range: versionRange,
	})
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apitypes.PluginAddResult{
		Plugin: pluginDTOs([]store.ServerPlugin{result.Plugin})[0], Revision: result.Revision,
	})
}

func (s *server) uploadServerPlugin(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 101<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_upload", "expected a multipart form with a jar file")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "missing_file", "the file field is required")
		return
	}
	defer file.Close()
	project := strings.TrimSpace(r.FormValue("project"))
	if project == "" {
		project = strings.TrimSuffix(header.Filename, ".jar")
	}
	result, err := service.Upload(r.Context(), principal, r.PathValue("id"),
		project, r.FormValue("version"), file)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apitypes.PluginAddResult{
		Plugin: pluginDTOs([]store.ServerPlugin{result.Plugin})[0], Revision: result.Revision,
	})
}

func (s *server) setServerPluginDisabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	var body apitypes.PluginToggleRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	result, err := service.SetDisabled(r.Context(), principal, r.PathValue("id"), body.Project, body.Disabled)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.PluginAddResult{
		Plugin: pluginDTOs([]store.ServerPlugin{result.Plugin})[0], Revision: result.Revision,
	})
}

func (s *server) removeServerPlugin(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.updatesService(w)
	if service == nil {
		return
	}
	project := r.URL.Query().Get("project")
	if project == "" || r.URL.Query().Get("confirm") != project {
		writeProblem(w, http.StatusBadRequest, "confirm_required", "removing a plugin also wipes its config folder; add ?confirm=<project>")
		return
	}
	if _, err := service.Remove(r.Context(), principal, r.PathValue("id"), project); err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
