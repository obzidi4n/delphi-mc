package api

import (
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
)

func (s *server) promotionService(w http.ResponseWriter) *capability.Dev {
	if s.opts.Services == nil || s.opts.Services.Dev == nil {
		writeProblem(w, http.StatusNotImplemented, "dev_disabled", "dev promotion is not configured")
		return nil
	}
	return s.opts.Services.Dev
}

func (s *server) previewPromotion(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.promotionService(w)
	if service == nil {
		return
	}
	preview, err := service.Preview(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := apitypes.PromotionPreviewDTO{
		DevID: preview.DevID, TargetID: preview.TargetID, TargetName: preview.TargetName,
		Plugins: make([]apitypes.PromotionPluginChange, 0, len(preview.Plugins)),
		Files:   make([]apitypes.PromotionFileChange, 0, len(preview.Files)),
	}
	for _, p := range preview.Plugins {
		out.Plugins = append(out.Plugins, apitypes.PromotionPluginChange{
			Project: p.Project, Provider: p.Provider, Current: strPtr(p.Current), Next: strPtr(p.Next), Change: p.Change,
		})
	}
	for _, f := range preview.Files {
		out.Files = append(out.Files, apitypes.PromotionFileChange{
			Path: f.Path, Status: f.Status, ProductionSha: strPtr(f.ProductionSHA), DevSha: strPtr(f.DevSHA), Size: &f.Size,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) promoteDevChanges(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.promotionService(w)
	if service == nil {
		return
	}
	var body apitypes.PromotionRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	result, err := service.Promote(r.Context(), principal, r.PathValue("id"), body.Plugins, body.Files)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.PromotionResultDTO{
		Plugins: result.Plugins, Files: result.Files, Pending: result.Pending,
	})
}
