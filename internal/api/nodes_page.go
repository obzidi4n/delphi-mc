package api

import (
	"encoding/pem"
	"html/template"
	"net/http"

	"delphi.dev/delphi/internal/store"
)

type nodesPage struct {
	BasePath string
	Email    string
	Nodes    []store.Node
	CA       bool
}

func (s *server) renderNodesPage(w http.ResponseWriter, r *http.Request) {
	principal, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	data := nodesPage{BasePath: s.base, Email: user.Email, CA: s.opts.CA != nil}
	if s.opts.Services != nil && s.opts.Services.Fleet != nil {
		if nodes, _, err := s.opts.Services.Fleet.ListNodes(r.Context(), principal, 200, ""); err == nil {
			data.Nodes = nodes
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = nodesTemplate.Execute(w, data)
}

func (s *server) downloadCA(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.pagePrincipal(w, r); !ok {
		return
	}
	if s.opts.CA == nil {
		http.Error(w, "the panel CA is not configured", http.StatusNotImplemented)
		return
	}
	body := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.opts.CA.Cert.Raw})
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=\"ca.crt\"")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

var nodesTemplate = template.Must(template.New("nodes").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Nodes · DelphiMC</title>
<style>
 :root{color-scheme:dark}
 *{box-sizing:border-box}
 body{margin:0;background:#0d1117;color:#c9d1d9;font:14px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;min-height:100vh}
 header{display:flex;align-items:center;gap:.9rem;padding:.55rem .9rem;border-bottom:1px solid #21262d;background:#010409;flex-wrap:wrap}
 header a{color:#79c0ff;text-decoration:none}
 header a.on{color:#e6edf3}
 h1{font-size:.95rem;margin:0;color:#e6edf3}
 main{max-width:64rem;margin:0 auto;padding:1.2rem}
 table{border-collapse:collapse;width:100%;margin:.6rem 0 1.2rem}
 th,td{text-align:left;padding:.45rem .6rem;border-bottom:1px solid #21262d;font-size:.88rem}
 th{color:#8b949e;font-weight:600}
 code,pre{font:12px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace}
 pre{background:#010409;border:1px solid #30363d;border-radius:.4rem;padding:.7rem .8rem;overflow:auto;color:#a5d6ff}
 .muted{color:#6e7681}
 .notice{padding:.6rem .8rem;border:1px solid #9e6a03;border-radius:.4rem;background:#1c1608;color:#e3b341;margin:.7rem 0}
 .err{color:#ff7b72;margin:.5rem 0;white-space:pre-wrap}
 .ok{color:#3fb950}
 button{padding:.4rem .8rem;border-radius:.4rem;border:1px solid #30363d;background:#21262d;color:inherit;cursor:pointer;font:inherit}
 button:hover{background:#30363d}
 button:disabled{opacity:.5;cursor:default}
 .row{display:flex;gap:.6rem;align-items:center;flex-wrap:wrap}
 a{color:#79c0ff}
 .pill{font-size:.72rem;padding:.1rem .45rem;border:1px solid #30363d;border-radius:999px;color:#8b949e}
</style></head><body data-base="{{.BasePath}}">
<header>
  <a href="{{.BasePath}}">&larr; Fleet</a>
  <a href="{{.BasePath}}nodes" class="on">Nodes</a>
  <a href="{{.BasePath}}templates">Templates</a>
  <a href="{{.BasePath}}audit">Audit</a>
  <a href="{{.BasePath}}chat">AI</a>
  <h1>Nodes</h1>
  <span style="flex:1"></span><span class="muted">{{.Email}}</span>
</header>
<main>
  <h2>Add a node</h2>
  <p class="muted">Install the DelphiMC binary and container runtime on the new host, then enroll its agent.
    Generate a one-time token here; the token is shown once.</p>
  <div class="row">
    <input id="node-name" placeholder="node name" value="node-name" aria-label="Node name">
    <button id="token">Generate enrollment token</button>
    {{if .CA}}<a href="{{.BasePath}}nodes/ca.crt">Download ca.crt</a>{{else}}<span class="muted">CA download unavailable (TLS disabled)</span>{{end}}
    <button id="refresh">Refresh nodes</button>
  </div>
  <div id="result" hidden>
    <p class="ok" id="expires"></p>
    <pre id="command"></pre>
    <button id="copy">Copy command</button>
  </div>
  <p class="err" id="error"></p>

  <h2>Enrolled nodes</h2>
  {{if .Nodes}}
  <table>
    <tr><th>Name</th><th>Status</th><th>Version</th><th>FS</th><th>Last seen</th></tr>
    {{range .Nodes}}<tr>
      <td>{{.Name}}</td>
      <td>{{.Status}}{{if .RevokedAt}} · revoked{{end}}</td>
      <td>{{.AgentVersion}}</td>
      <td>{{.FSCapability}}</td>
      <td class="muted">{{if .LastSeen}}{{.LastSeen}}{{else}}-{{end}}</td>
    </tr>{{end}}
  </table>
  {{else}}<p class="muted">No nodes enrolled yet. Enroll an agent above.</p>{{end}}
  <p class="muted">The agent is one daemon per host; it manages every Minecraft server on that node.</p>
  <p><a href="{{.BasePath}}servers/new">Create a server on a node</a></p>
</main>
<script>
(function () {
  'use strict';
  var base = document.body.dataset.base || '/';
  var tokenBtn = document.getElementById('token');
  var result = document.getElementById('result');
  var command = document.getElementById('command');
  var expires = document.getElementById('expires');
  var error = document.getElementById('error');
  function problem(text) {
    try { var p = JSON.parse(text); return p.detail || p.title || text; }
    catch (e) { return text; }
  }
  tokenBtn.addEventListener('click', async function () {
    tokenBtn.disabled = true;
    error.textContent = '';
    try {
      var res = await fetch(base + 'api/v1/enrollment-tokens', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'content-type': 'application/json' },
        body: '{}'
      });
      if (!res.ok) { error.textContent = problem(await res.text()); return; }
      var data = await res.json();
      var origin = location.origin + base.replace(/\/$/, '');
      var nodeName = document.getElementById('node-name').value.trim() || 'node-name';
      var text = "DELPHI_ENROLL_TOKEN='" + data.token + "' delphi agent enroll --panel '" +
        origin + "' --ca ca.crt --name '" + nodeName + "'";
      command.textContent = text;
      expires.textContent = 'Token expires ' + new Date(data.expires_at).toLocaleString() + '. Copy it now; it is single-use.';
      result.hidden = false;
    } catch (e) { error.textContent = String(e); }
    finally { tokenBtn.disabled = false; }
  });
  document.getElementById('copy').addEventListener('click', function () {
    if (!navigator.clipboard) { return; }
    navigator.clipboard.writeText(command.textContent).then(function () {
      var b = document.getElementById('copy');
      b.textContent = 'Copied';
      setTimeout(function () { b.textContent = 'Copy command'; }, 900);
    });
  });
  document.getElementById('refresh').addEventListener('click', function () { location.reload(); });
})();
</script>
</body></html>`))
