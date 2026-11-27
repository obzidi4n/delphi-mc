package api

import (
	"net/http"

	"delphi.dev/delphi/internal/apitypes"
	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

func (s *server) usersService(w http.ResponseWriter) *capability.Users {
	if s.opts.Services == nil || s.opts.Services.Users == nil {
		writeProblem(w, http.StatusNotImplemented, "users_disabled", "the user service is not configured")
		return nil
	}
	return s.opts.Services.Users
}

func toUserDTO(u store.UserWithRole) apitypes.UserDTO {
	return apitypes.UserDTO{
		ID: u.ID, OrgID: u.OrgID, Email: u.Email, Role: u.Role, CreatedAt: u.CreatedAt,
	}
}

func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.usersService(w)
	if service == nil {
		return
	}
	limit, after := pageParams(r)
	users, next, err := service.List(r.Context(), principal, limit, after)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	items := make([]apitypes.UserDTO, 0, len(users))
	for _, u := range users {
		items = append(items, toUserDTO(u))
	}
	writeJSON(w, http.StatusOK, apitypes.UserListDTO{Items: items, NextCursor: cursorPtr(next)})
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.usersService(w)
	if service == nil {
		return
	}
	var body apitypes.UserCreateRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	role := ""
	if body.Role != nil {
		role = *body.Role
	}
	user, password, err := service.Create(r.Context(), principal, body.Email, role)
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apitypes.CreatedUserDTO{
		User: toUserDTO(user), Password: password,
	})
}

func (s *server) setUserRole(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	service := s.usersService(w)
	if service == nil {
		return
	}
	var body apitypes.RoleRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id := r.PathValue("id")
	if err := service.SetRole(r.Context(), principal, id, body.Role); err != nil {
		writeCapabilityError(w, err)
		return
	}
	// Re-read so the response carries the applied role.
	users, _, err := service.List(r.Context(), principal, 200, "")
	if err != nil {
		writeCapabilityError(w, err)
		return
	}
	for _, u := range users {
		if u.ID == id {
			writeJSON(w, http.StatusOK, toUserDTO(u))
			return
		}
	}
	writeProblem(w, http.StatusNotFound, "user_not_found", "no such user")
}
