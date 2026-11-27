package api

import (
	"html/template"
	"net/http"

	"delphi.dev/delphi/internal/capability"
	"delphi.dev/delphi/internal/store"
)

type pageData struct {
	BasePath string
	Page     string
	Email    string
	Error    string
	Notice   string

	Servers []store.Server
	Nodes   []store.Node
	Users   []store.UserWithRole
	Groups  []groupView
	Roles   []string

	CreatedEmail    string
	CreatedPassword string
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
 nav{display:flex;gap:1rem;flex:1}
 nav a{color:inherit;text-decoration:none;opacity:.75}
 nav a:hover,nav a.on{opacity:1}
 main{max-width:60rem;margin:0 auto;padding:1.5rem 1.25rem}
 table{border-collapse:collapse;width:100%;margin:.5rem 0 1.5rem}
 th,td{text-align:left;padding:.45rem .6rem;border-bottom:1px solid #8883;font-size:.9rem}
 th{font-weight:600;opacity:.75}
 .muted{opacity:.65}
 form{display:flex;gap:.5rem;align-items:center;flex-wrap:wrap}
 form.stack{flex-direction:column;align-items:stretch;max-width:24rem}
 input,select{padding:.45rem .6rem;border:1px solid #8886;border-radius:.4rem;background:transparent;color:inherit}
 button{padding:.45rem .9rem;border-radius:.4rem;border:1px solid #8886;background:transparent;color:inherit;cursor:pointer}
 .err{color:#c33;margin:.5rem 0}
 .ok{color:#2a8;margin:.5rem 0}
 .card{border:1px solid #8884;border-radius:.6rem;padding:1.1rem 1.25rem;max-width:26rem;margin:3rem auto}
 section{border:1px solid #8884;border-radius:.6rem;padding:1rem 1.25rem;margin:0 0 1.25rem}
 section h3{margin:.1rem 0 .5rem;font-size:1rem}
</style></head><body>
{{$base := .BasePath}}
<header><h1>DelphiMC</h1>
{{if .Email}}<nav>
 <a href="{{$base}}" {{if eq .Page "home"}}class="on"{{end}}>Fleet</a>
 <a href="{{$base}}team" {{if eq .Page "team"}}class="on"{{end}}>Team</a>
 <a href="{{$base}}groups" {{if eq .Page "groups"}}class="on"{{end}}>Groups</a>
 <a href="{{$base}}chat">AI</a>
</nav>{{end}}
{{if .Email}}<form method="post" action="{{$base}}logout"><span class="muted">{{.Email}}</span> <button type="submit">Sign out</button></form>{{end}}
</header>
<main>
{{if not .Email}}
  <div class="card">
    <h2>Sign in</h2>
    {{if .Error}}<p class="err">{{.Error}}</p>{{end}}
    <form method="post" action="{{$base}}login" class="stack">
      <input name="email" type="email" placeholder="email" required>
      <input name="password" type="password" placeholder="password" required>
      <button type="submit">Sign in</button>
    </form>
    <p class="muted">The owner account is created by <code>delphi init</code>.</p>
  </div>
{{else}}
  {{if .Notice}}<p class="ok">{{.Notice}}</p>{{end}}
  {{if .Error}}<p class="err">{{.Error}}</p>{{end}}

  {{if eq .Page "team"}}
    <h2>Team</h2>
    {{if .CreatedPassword}}<p class="ok">Password for {{.CreatedEmail}}: <code>{{.CreatedPassword}}</code> — copy it now, it is stored hashed.</p>{{end}}
    {{if .Users}}<table>
      <tr><th>Email</th><th>Role</th><th>Created</th><th>Set role</th></tr>
      {{range .Users}}<tr>
        <td>{{.Email}}</td>
        <td>{{if .Role}}{{.Role}}{{else}}<span class="muted">none</span>{{end}}</td>
        <td class="muted">{{.CreatedAt.Format "2006-01-02"}}</td>
        <td><form method="post" action="{{$base}}team/{{.ID}}/role">
          <select name="role">
            <option value="viewer" {{if eq .Role "viewer"}}selected{{end}}>viewer</option>
            <option value="developer" {{if eq .Role "developer"}}selected{{end}}>developer</option>
            <option value="admin" {{if eq .Role "admin"}}selected{{end}}>admin</option>
            <option value="owner" {{if eq .Role "owner"}}selected{{end}}>owner</option>
          </select>
          <button type="submit">Apply</button>
        </form></td>
      </tr>{{end}}
    </table>{{else}}<p class="muted">No accounts.</p>{{end}}
    <section>
      <h3>Add account</h3>
      <form method="post" action="{{$base}}team">
        <input name="email" type="email" placeholder="email" required>
        <select name="role">
          {{range .Roles}}<option value="{{.}}" {{if eq . "viewer"}}selected{{end}}>{{.}}</option>{{end}}
        </select>
        <button type="submit">Create</button>
      </form>
      <p class="muted">Passwords are generated once; there are no email invites yet.</p>
    </section>

  {{else if eq .Page "groups"}}
    <h2>Groups</h2>
    {{range .Groups}}{{$g := .}}
      <section>
        <h3>{{.Group.Name}}</h3>
        {{if .Group.Description}}<p class="muted">{{.Group.Description}}</p>{{end}}
        {{if .Members}}<table>
          <tr><th>Server</th><th>State</th><th></th></tr>
          {{range .Members}}<tr>
            <td>{{.Name}}</td><td>{{.ObservedState}}</td>
            <td><form method="post" action="{{$base}}groups/{{$g.Group.ID}}/members/{{.ID}}/remove"><button type="submit">Remove</button></form></td>
          </tr>{{end}}
        </table>{{else}}<p class="muted">No servers in this group.</p>{{end}}
        <form method="post" action="{{$base}}groups/{{.Group.ID}}/members">
          <select name="server_id" required>
            <option value="">add a server…</option>
            {{range $.Servers}}<option value="{{.ID}}">{{.Name}}</option>{{end}}
          </select>
          <button type="submit">Add</button>
        </form>
      </section>
    {{else}}<p class="muted">No groups yet.</p>{{end}}
    <section>
      <h3>Create group</h3>
      <form method="post" action="{{$base}}groups">
        <input name="name" placeholder="name" required>
        <input name="description" placeholder="description">
        <button type="submit">Create</button>
      </form>
    </section>

  {{else}}
    <h2>Servers</h2>
    {{if .Servers}}<table><tr><th>Name</th><th>Desired</th><th>Observed</th><th>Port</th><th>Node</th><th>ID</th></tr>
    {{range .Servers}}<tr><td><a href="{{$base}}servers/{{.ID}}">{{.Name}}</a></td><td>{{.DesiredState}}</td><td>{{.ObservedState}}</td><td>{{.Port}}</td><td>{{.NodeID}}</td><td class="muted">{{.ID}}</td></tr>{{end}}</table>
    {{else}}<p class="muted">No servers yet. Create one with the API or CLI.</p>{{end}}
    <h2>Nodes</h2>
    {{if .Nodes}}<table><tr><th>Name</th><th>Status</th><th>Version</th><th>FS</th><th>Last seen</th></tr>
    {{range .Nodes}}<tr><td>{{.Name}}</td><td>{{.Status}}</td><td>{{.AgentVersion}}</td><td>{{.FSCapability}}</td><td>{{if .LastSeen}}{{.LastSeen}}{{else}}-{{end}}</td></tr>{{end}}</table>
    {{else}}<p class="muted">No nodes enrolled.</p>{{end}}
  {{end}}
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
	data := pageData{BasePath: s.base, Page: "home"}
	user, ok := s.currentUser(r)
	if !ok {
		s.writePage(w, data)
		return
	}
	data.Email = user.Email

	if s.opts.Services == nil {
		data.Notice = "The database is not configured; the fleet is unavailable."
		s.writePage(w, data)
		return
	}
	principal := capability.Principal{UserID: user.ID, OrgID: user.OrgID, Via: "ui"}
	if scopes, perr := s.opts.Identity.ScopesForUser(r.Context(), user.ID); perr == nil {
		principal.Scopes = scopes
	}
	if servers, next, err := s.opts.Services.Servers.List(r.Context(), principal, 200, ""); err == nil {
		data.Servers = servers
		if next != "" {
			data.Notice = "Showing the first 200 servers; use the API for the rest."
		}
	} else {
		data.Notice = "Could not load servers: " + err.Error()
	}
	if nodes, _, err := s.opts.Services.Fleet.ListNodes(r.Context(), principal, 200, ""); err == nil {
		data.Nodes = nodes
	}
	s.writePage(w, data)
}
