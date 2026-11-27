package api

import (
	"html/template"
	"net/http"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

type pageData struct {
	BasePath string
	Email    string
	Error    string
	Servers  []store.Server
	Nodes    []store.Node
	Notice   string
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DelphiMC</title>
<style>
 :root{color-scheme:light dark}
 body{margin:0;font:15px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif}
 header{display:flex;justify-content:space-between;align-items:center;gap:1rem;padding:.9rem 1.25rem;border-bottom:1px solid #8884}
 h1{font-size:1.1rem;margin:0;letter-spacing:-.01em}
 main{max-width:60rem;margin:0 auto;padding:1.5rem 1.25rem}
 table{border-collapse:collapse;width:100%;margin:.5rem 0 1.5rem}
 th,td{text-align:left;padding:.45rem .6rem;border-bottom:1px solid #8883;font-size:.9rem}
 th{font-weight:600;opacity:.75}
 .muted{opacity:.65}
 form{display:flex;gap:.5rem;align-items:center}
 input{padding:.45rem .6rem;border:1px solid #8886;border-radius:.4rem;background:transparent;color:inherit}
 button{padding:.45rem .9rem;border-radius:.4rem;border:1px solid #8886;background:transparent;color:inherit;cursor:pointer}
 .err{color:#c33;margin:.5rem 0}
 .card{border:1px solid #8884;border-radius:.6rem;padding:1.1rem 1.25rem;max-width:26rem;margin:3rem auto}
</style></head><body>
<header><h1>DelphiMC</h1>{{if .Email}}<form method="post" action="{{.BasePath}}logout"><span class="muted">{{.Email}}</span> <button type="submit">Sign out</button></form>{{end}}</header>
<main>
{{if .Email}}
  {{if .Notice}}<p class="muted">{{.Notice}}</p>{{end}}
  <h2>Servers</h2>
  {{if .Servers}}<table><tr><th>Name</th><th>Desired</th><th>Observed</th><th>Port</th><th>Node</th><th>ID</th></tr>
  {{range .Servers}}<tr><td>{{.Name}}</td><td>{{.DesiredState}}</td><td>{{.ObservedState}}</td><td>{{.Port}}</td><td>{{.NodeID}}</td><td class="muted">{{.ID}}</td></tr>{{end}}</table>
  {{else}}<p class="muted">No servers yet. Create one with the API or CLI.</p>{{end}}
  <h2>Nodes</h2>
  {{if .Nodes}}<table><tr><th>Name</th><th>Status</th><th>Version</th><th>FS</th><th>Last seen</th></tr>
  {{range .Nodes}}<tr><td>{{.Name}}</td><td>{{.Status}}</td><td>{{.AgentVersion}}</td><td>{{.FSCapability}}</td><td>{{if .LastSeen}}{{.LastSeen}}{{else}}-{{end}}</td></tr>{{end}}</table>
  {{else}}<p class="muted">No nodes enrolled.</p>{{end}}
{{else}}
  <div class="card">
    <h2>Sign in</h2>
    {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
    <form method="post" action="{{.BasePath}}login">
      <input name="email" type="email" placeholder="email" value="{{.Email}}" required>
      <input name="password" type="password" placeholder="password" required>
      <button type="submit">Sign in</button>
    </form>
    <p class="muted">The owner account is created by <code>delphi init</code>.</p>
  </div>
{{end}}
</main></body></html>`))

func (s *server) renderLogin(w http.ResponseWriter, r *http.Request, message, email string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if message != "" {
		w.WriteHeader(http.StatusUnauthorized)
	}
	_ = pageTemplate.Execute(w, pageData{BasePath: s.base, Error: message, Email: email})
}

// renderHome shows the fleet for a session, otherwise the login card.
func (s *server) renderHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	data := pageData{BasePath: s.base}
	user, ok := s.currentUser(r)
	if !ok {
		_ = pageTemplate.Execute(w, data)
		return
	}
	data.Email = user.Email

	if s.opts.Services == nil {
		data.Notice = "The database is not configured; the fleet is unavailable."
		_ = pageTemplate.Execute(w, data)
		return
	}
	principal := capability.Principal{UserID: user.ID, OrgID: user.OrgID, Via: "ui"}
	if scopes, perr := s.opts.Identity.ScopesForUser(r.Context(), user.ID); perr == nil {
		principal.Scopes = scopes
	}
	if servers, err := s.opts.Services.Servers.List(r.Context(), principal); err == nil {
		data.Servers = servers
	} else {
		data.Notice = "Could not load servers: " + err.Error()
	}
	if nodes, err := s.opts.Services.Fleet.ListNodes(r.Context(), principal); err == nil {
		data.Nodes = nodes
	}
	_ = pageTemplate.Execute(w, data)
}
