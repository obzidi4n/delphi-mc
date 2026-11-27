package api

import (
	"io"
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
	"delphi.dev/delphi/internal/template"
)

func (s *server) templatesService(w http.ResponseWriter) *capability.Templates {
	if s.opts.Services == nil || s.opts.Services.Templates == nil {
		writeProblem(w, http.StatusNotImplemented, "templates_disabled", "the template service is not configured")
		return nil
	}
	return s.opts.Services.Templates
}

func toTemplateSummary(tpl template.Template, row store.Template) apitypes.TemplateSummaryDTO {
	return apitypes.TemplateSummaryDTO{
		ID: row.ID, Name: tpl.Name, Description: tpl.Description,
		Base: tpl.Base, MCVersion: tpl.MCVersion, Java: tpl.Java,
		Revision: row.Revision, Builtin: row.Builtin,
	}
}

func toTemplateDTO(tpl template.Template, row store.Template) apitypes.TemplateDTO {
	defaults := apitypes.TemplateDefaultsDTO{
		MemoryMB: tpl.Defaults.MemoryMB, CPUCores: tpl.Defaults.CPUCores, Port: tpl.Defaults.Port,
	}
	plugins := make([]apitypes.TemplatePluginDTO, 0, len(tpl.Plugins))
	for _, p := range tpl.Plugins {
		required := p.Required
		plugin := apitypes.TemplatePluginDTO{Project: p.Project, Provider: p.Provider, Required: &required}
		if p.Version != "" {
			plugin.Version = &p.Version
		}
		if p.Range != "" {
			plugin.Range = &p.Range
		}
		plugins = append(plugins, plugin)
	}
	variables := make(map[string]apitypes.TemplateVariableDTO, len(tpl.Variables))
	for name, v := range tpl.Variables {
		required := v.Required
		values := apitypes.TemplateVariableDTO{Required: &required}
		if v.Description != "" {
			values.Description = &v.Description
		}
		if v.Default != "" {
			values.Default = &v.Default
		}
		variables[name] = values
	}
	files := make(map[string]string, len(tpl.Files))
	for path, body := range tpl.Files {
		files[path] = body
	}
	return apitypes.TemplateDTO{
		SchemaVersion: template.SchemaVersion,
		ID:            row.ID, Name: tpl.Name, Description: tpl.Description,
		Base: tpl.Base, MCVersion: tpl.MCVersion, Java: tpl.Java,
		Defaults: defaults, Files: files, Plugins: plugins, Variables: variables,
		Revision: row.Revision, Builtin: row.Builtin,
		Body: string(row.Body), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func (s *server) listTemplates(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.templatesService(w)
	if service == nil {
		return
	}
	limit, after := pageParams(r)
	page, err := service.List(r.Context(), principal, limit, after)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	items := make([]apitypes.TemplateSummaryDTO, 0, len(page.Items))
	for _, row := range page.Items {
		tpl, err := template.Parse(row.Body)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "invalid_template", "stored template is invalid")
			return
		}
		items = append(items, toTemplateSummary(tpl, row))
	}
	writeJSON(w, http.StatusOK, apitypes.TemplateListDTO{Items: items, NextCursor: cursorPtr(page.Next)})
}

func (s *server) getTemplate(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.templatesService(w)
	if service == nil {
		return
	}
	tpl, row, err := service.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTemplateDTO(tpl, row))
}

func (s *server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.templatesService(w)
	if service == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_body", "the template body could not be read")
		return
	}
	if len(body) == 0 {
		writeProblem(w, http.StatusBadRequest, "invalid_body", "the template body is empty")
		return
	}
	tpl, row, err := service.Update(r.Context(), principal, r.PathValue("id"), body)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTemplateDTO(tpl, row))
}

func (s *server) importTemplate(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.templatesService(w)
	if service == nil {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_body", "the template body could not be read")
		return
	}
	if len(body) == 0 {
		writeProblem(w, http.StatusBadRequest, "invalid_body", "the template body is empty")
		return
	}
	tpl, row, err := service.Import(r.Context(), principal, body)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTemplateDTO(tpl, row))
}
