package api

import (
	"net/http"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

// groupView is one group with its member servers resolved for rendering.
type groupView struct {
	Group   store.Group
	Members []store.Server
}

// pagePrincipal resolves the session user and enforces same-origin on
// mutations. It redirects anonymous visitors to the sign-in page.
func (s *server) pagePrincipal(w http.ResponseWriter, r *http.Request) (capability.Principal, store.User, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return capability.Principal{}, store.User{}, false
	}
	user, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, s.base, http.StatusSeeOther)
		return capability.Principal{}, store.User{}, false
	}
	principal := capability.Principal{UserID: user.ID, OrgID: user.OrgID, Via: "ui"}
	if s.opts.Identity != nil {
		if scopes, err := s.opts.Identity.ScopesForUser(r.Context(), user.ID); err == nil {
			principal.Scopes = scopes
		}
	}
	return principal, user, true
}

func (s *server) renderTeam(w http.ResponseWriter, r *http.Request) {
	s.renderTeamPage(w, r, pageData{})
}

func (s *server) renderTeamPage(w http.ResponseWriter, r *http.Request, data pageData) {
	principal, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	data.BasePath = s.base
	data.Page = "team"
	data.Email = user.Email
	data.Roles = []string{"viewer", "developer", "admin", "owner"}

	if s.opts.Services == nil || s.opts.Services.Users == nil {
		data.Notice = "The database is not configured; user management is unavailable."
		s.writePage(w, data)
		return
	}
	users, next, err := s.opts.Services.Users.List(r.Context(), principal, 200, "")
	if err != nil {
		data.Notice = "Could not load users: " + err.Error()
	} else {
		data.Users = users
		if next != "" {
			data.Notice = "Showing the first 200 accounts; use the API for the rest."
		}
	}
	s.writePage(w, data)
}

func (s *server) createUserForm(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderTeamPage(w, r, pageData{Error: "Invalid form."})
		return
	}
	email := r.PostFormValue("email")
	role := r.PostFormValue("role")

	data := pageData{}
	if s.opts.Services == nil || s.opts.Services.Users == nil {
		data.Notice = "The database is not configured; user management is unavailable."
		s.renderTeamPage(w, r, data)
		return
	}
	_, password, err := s.opts.Services.Users.Create(r.Context(), principal, email, role)
	if err != nil {
		data.Error = err.Error()
		s.renderTeamPage(w, r, data)
		return
	}
	data.CreatedEmail = email
	data.CreatedPassword = password
	data.Notice = "Account created. Copy the password now: it is not stored in plaintext."
	s.renderTeamPage(w, r, data)
}

func (s *server) setUserRoleForm(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, s.base+"team", http.StatusSeeOther)
		return
	}
	if s.opts.Services == nil || s.opts.Services.Users == nil {
		http.Redirect(w, r, s.base+"team", http.StatusSeeOther)
		return
	}
	role := r.PostFormValue("role")
	if err := s.opts.Services.Users.SetRole(r.Context(), principal, r.PathValue("id"), role); err != nil {
		s.renderTeamPage(w, r, pageData{Error: err.Error()})
		return
	}
	http.Redirect(w, r, s.base+"team", http.StatusSeeOther)
}

func (s *server) renderGroups(w http.ResponseWriter, r *http.Request) {
	s.renderGroupsPage(w, r, pageData{})
}

func (s *server) renderGroupsPage(w http.ResponseWriter, r *http.Request, data pageData) {
	principal, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	data.BasePath = s.base
	data.Page = "groups"
	data.Email = user.Email

	if s.opts.Services == nil || s.opts.Services.Groups == nil {
		data.Notice = "The database is not configured; groups are unavailable."
		s.writePage(w, data)
		return
	}
	groups, next, err := s.opts.Services.Groups.List(r.Context(), principal, 200, "")
	if err != nil {
		data.Notice = "Could not load groups: " + err.Error()
		s.writePage(w, data)
		return
	}
	servers, _, err := s.opts.Services.Servers.List(r.Context(), principal, 200, "")
	if err != nil {
		data.Notice = "Could not load servers: " + err.Error()
		s.writePage(w, data)
		return
	}
	data.Servers = servers
	if next != "" {
		data.Notice = "Showing the first 200 groups; use the API for the rest."
	}
	byID := make(map[string]store.Server, len(servers))
	for _, srv := range servers {
		byID[srv.ID] = srv
	}
	for _, group := range groups {
		view := groupView{Group: group}
		if ids, _, err := s.opts.Services.Groups.Members(r.Context(), principal, group.ID, 200, ""); err == nil {
			for _, id := range ids {
				if srv, ok := byID[id]; ok {
					view.Members = append(view.Members, srv)
				}
			}
		}
		data.Groups = append(data.Groups, view)
	}
	s.writePage(w, data)
}

func (s *server) createGroupForm(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderGroupsPage(w, r, pageData{Error: "Invalid form."})
		return
	}
	if s.opts.Services == nil || s.opts.Services.Groups == nil {
		http.Redirect(w, r, s.base+"groups", http.StatusSeeOther)
		return
	}
	if _, err := s.opts.Services.Groups.Create(r.Context(), principal,
		r.PostFormValue("name"), r.PostFormValue("description")); err != nil {
		s.renderGroupsPage(w, r, pageData{Error: err.Error()})
		return
	}
	http.Redirect(w, r, s.base+"groups", http.StatusSeeOther)
}

func (s *server) addGroupMemberForm(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderGroupsPage(w, r, pageData{Error: "Invalid form."})
		return
	}
	if s.opts.Services == nil || s.opts.Services.Groups == nil {
		http.Redirect(w, r, s.base+"groups", http.StatusSeeOther)
		return
	}
	if err := s.opts.Services.Groups.AddMember(r.Context(), principal,
		r.PathValue("id"), r.PostFormValue("server_id")); err != nil {
		s.renderGroupsPage(w, r, pageData{Error: err.Error()})
		return
	}
	http.Redirect(w, r, s.base+"groups", http.StatusSeeOther)
}

func (s *server) removeGroupMemberForm(w http.ResponseWriter, r *http.Request) {
	principal, _, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	if s.opts.Services == nil || s.opts.Services.Groups == nil {
		http.Redirect(w, r, s.base+"groups", http.StatusSeeOther)
		return
	}
	if err := s.opts.Services.Groups.RemoveMember(r.Context(), principal,
		r.PathValue("id"), r.PathValue("sid")); err != nil {
		s.renderGroupsPage(w, r, pageData{Error: err.Error()})
		return
	}
	http.Redirect(w, r, s.base+"groups", http.StatusSeeOther)
}

func (s *server) writePage(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if data.Error != "" {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}
	_ = pageTemplate.Execute(w, data)
}
