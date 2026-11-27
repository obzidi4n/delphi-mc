package api

import (
	"errors"
	"net/http"
	"strconv"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

func (s *server) scheduleService(w http.ResponseWriter) *capability.Schedules {
	if s.opts.Services == nil || s.opts.Services.Schedules == nil {
		writeProblem(w, http.StatusNotImplemented, "schedules_disabled", "the schedule service is not configured")
		return nil
	}
	return s.opts.Services.Schedules
}

func (s *server) alertService(w http.ResponseWriter) *capability.Alerts {
	if s.opts.Alerts == nil {
		writeProblem(w, http.StatusNotImplemented, "alerts_disabled", "the alert service is not configured")
		return nil
	}
	return s.opts.Alerts
}

func toScheduleDTO(row store.Schedule) apitypes.ScheduleDTO {
	return apitypes.ScheduleDTO{
		ID: row.ID, Name: row.Name, ServerID: row.TargetID, Action: row.Action,
		Cron: row.Cron, Timezone: row.Timezone, Enabled: row.Enabled,
		NextRun: row.NextRun, LastRun: row.LastRun, CreatedAt: row.CreatedAt,
	}
}

func (s *server) listSchedules(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.scheduleService(w)
	if service == nil {
		return
	}
	items, err := service.List(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.ScheduleDTO, 0, len(items))
	for _, row := range items {
		out = append(out, toScheduleDTO(row))
	}
	writeJSON(w, http.StatusOK, apitypes.ScheduleListDTO{Items: out})
}

func (s *server) createSchedule(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.scheduleService(w)
	if service == nil {
		return
	}
	var body apitypes.ScheduleCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	timezone := ""
	if body.Timezone != nil {
		timezone = *body.Timezone
	}
	row, err := service.Create(r.Context(), principal, capability.ScheduleRequest{
		Name: body.Name, ServerID: body.ServerID, Action: body.Action, Cron: body.Cron, Timezone: timezone,
	})
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toScheduleDTO(row))
}

func (s *server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.scheduleService(w)
	if service == nil {
		return
	}
	err := service.Delete(r.Context(), principal, r.PathValue("id"))
	if errors.Is(err, store.ErrNoSchedule) {
		writeProblem(w, http.StatusNotFound, "schedule_not_found", "no such schedule")
		return
	}
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toRuleDTO(row store.AlertRule) apitypes.AlertRuleDTO {
	return apitypes.AlertRuleDTO{
		ID: row.ID, Kind: row.Kind, Threshold: row.Threshold,
		WindowSeconds: row.WindowSeconds, Enabled: row.Enabled, CreatedAt: row.CreatedAt,
	}
}

func (s *server) listAlertRules(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	items, err := service.Rules(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.AlertRuleDTO, 0, len(items))
	for _, row := range items {
		out = append(out, toRuleDTO(row))
	}
	writeJSON(w, http.StatusOK, apitypes.AlertRuleListDTO{Items: out})
}

func (s *server) createAlertRule(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	var body apitypes.AlertRuleCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	threshold, window := 0, 0
	if body.Threshold != nil {
		threshold = *body.Threshold
	}
	if body.WindowSeconds != nil {
		window = *body.WindowSeconds
	}
	row, err := service.CreateRule(r.Context(), principal, body.Kind, threshold, window)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRuleDTO(row))
}

func (s *server) deleteAlertRule(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	if err := service.DeleteRule(r.Context(), principal, r.PathValue("id")); err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toChannelDTO(row store.AlertChannel) apitypes.AlertChannelDTO {
	return apitypes.AlertChannelDTO{
		ID: row.ID, Name: row.Name, Kind: row.Kind, CreatedAt: row.CreatedAt,
	}
}

func (s *server) listAlertChannels(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	items, err := service.Channels(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.AlertChannelDTO, 0, len(items))
	for _, row := range items {
		out = append(out, toChannelDTO(row))
	}
	writeJSON(w, http.StatusOK, apitypes.AlertChannelListDTO{Items: out})
}

func (s *server) createAlertChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	var body apitypes.AlertChannelCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	row, err := service.CreateChannel(r.Context(), principal, body.Name, body.Kind, body.Config)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toChannelDTO(row))
}

func (s *server) deleteAlertChannel(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	if err := service.DeleteChannel(r.Context(), principal, r.PathValue("id")); err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAlertDTO(row store.Alert) apitypes.AlertDTO {
	return apitypes.AlertDTO{
		ID: row.ID, RuleID: row.RuleID, ServerID: row.ServerID, State: row.State,
		Summary: row.Summary, StartedAt: row.StartedAt, ResolvedAt: row.ResolvedAt,
	}
}

func (s *server) listAlerts(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	items, err := service.List(r.Context(), principal, limit)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.AlertDTO, 0, len(items))
	for _, row := range items {
		out = append(out, toAlertDTO(row))
	}
	writeJSON(w, http.StatusOK, apitypes.AlertListDTO{Items: out})
}

func toWindowDTO(row store.MaintenanceWindow) apitypes.MaintenanceWindowDTO {
	return apitypes.MaintenanceWindowDTO{
		ID: row.ID, StartsAt: row.StartsAt, EndsAt: row.EndsAt,
		SuppressAlerts: row.SuppressAlerts, BlockUpdates: row.BlockUpdates, CreatedAt: row.CreatedAt,
	}
}

func (s *server) listMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	items, err := service.Windows(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	out := make([]apitypes.MaintenanceWindowDTO, 0, len(items))
	for _, row := range items {
		out = append(out, toWindowDTO(row))
	}
	writeJSON(w, http.StatusOK, apitypes.MaintenanceWindowListDTO{Items: out})
}

func (s *server) createMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	var body apitypes.MaintenanceWindowCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	suppress := true
	if body.SuppressAlerts != nil {
		suppress = *body.SuppressAlerts
	}
	block := false
	if body.BlockUpdates != nil {
		block = *body.BlockUpdates
	}
	row, err := service.CreateWindow(r.Context(), principal, body.StartsAt, body.EndsAt, suppress, block)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWindowDTO(row))
}

func (s *server) deleteMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.alertService(w)
	if service == nil {
		return
	}
	if err := service.DeleteWindow(r.Context(), principal, r.PathValue("id")); err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
