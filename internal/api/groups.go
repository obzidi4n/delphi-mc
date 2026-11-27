package api

import (
	"net/http"
	"strconv"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/store"
)

func (s *server) groups(w http.ResponseWriter) bool {
	if s.opts.Services == nil || s.opts.Services.Groups == nil {
		writeProblem(w, http.StatusNotImplemented, "groups_disabled", "the group service is not configured")
		return false
	}
	return true
}

func (s *server) listGroups(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.groups(w) {
		return
	}
	groups, err := s.opts.Services.Groups.List(r.Context(), principal)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	items := make([]apitypes.GroupDTO, 0, len(groups))
	for _, g := range groups {
		items = append(items, toGroupDTO(g))
	}
	writeJSON(w, http.StatusOK, apitypes.GroupListDTO{Items: items})
}

func (s *server) createGroup(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.groups(w) {
		return
	}
	var body apitypes.GroupCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	description := ""
	if body.Description != nil {
		description = *body.Description
	}
	group, err := s.opts.Services.Groups.Create(r.Context(), principal, body.Name, description)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toGroupDTO(group))
}

func (s *server) addGroupMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.groups(w) {
		return
	}
	var body apitypes.MemberRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := s.opts.Services.Groups.AddMember(r.Context(), principal, r.PathValue("id"), body.ServerID); err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) removeGroupMember(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.groups(w) {
		return
	}
	if err := s.opts.Services.Groups.RemoveMember(r.Context(), principal,
		r.PathValue("id"), r.PathValue("server_id")); err != nil {
		writeCapabilityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listGroupMembers(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok || !s.groups(w) {
		return
	}
	ids, err := s.opts.Services.Groups.Members(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.MemberListDTO{Items: ids})
}

func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.opts.Services == nil || s.opts.Services.Audit == nil {
		writeProblem(w, http.StatusNotImplemented, "audit_disabled", "the audit service is not configured")
		return
	}

	query := r.URL.Query()
	limit, _ := strconv.ParseInt(query.Get("limit"), 10, 64)
	before, _ := strconv.ParseInt(query.Get("before"), 10, 64)
	entries, err := s.opts.Services.Audit.List(r.Context(), principal, limit, before)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	items := make([]apitypes.AuditEntryDTO, 0, len(entries))
	for _, e := range entries {
		items = append(items, apitypes.AuditEntryDTO{
			Seq: e.Seq, OrgID: e.OrgID, Actor: e.Actor, Via: e.Via,
			Action: e.Action, Target: e.Target, Diff: e.Diff, CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, apitypes.AuditListDTO{Items: items})
}

func toGroupDTO(g store.Group) apitypes.GroupDTO {
	return apitypes.GroupDTO{
		ID: g.ID, OrgID: g.OrgID, Name: g.Name,
		Description: g.Description, CreatedAt: g.CreatedAt,
	}
}
