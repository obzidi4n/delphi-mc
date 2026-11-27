package api

import (
	"html/template"
	"net/http"
)

type newServerPage struct {
	BasePath string
	Email    string
}

func (s *server) renderNewServerPage(w http.ResponseWriter, r *http.Request) {
	_, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = newServerTemplate.Execute(w, newServerPage{BasePath: s.base, Email: user.Email})
}

var newServerTemplate = template.Must(template.New("new-server").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>New server · DelphiMC</title>
<style>
 :root{color-scheme:dark}
 *{box-sizing:border-box}
 body{margin:0;background:#0d1117;color:#c9d1d9;font:14px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;min-height:100vh}
 header{display:flex;align-items:center;gap:.9rem;padding:.55rem .9rem;border-bottom:1px solid #21262d;background:#010409;flex-wrap:wrap}
 header a{color:#79c0ff;text-decoration:none}
 header a.on{color:#e6edf3}
 h1{font-size:.95rem;margin:0;color:#e6edf3}
 main{max-width:42rem;margin:0 auto;padding:1.2rem}
 form{display:flex;flex-direction:column;gap:.7rem}
 label{display:flex;flex-direction:column;gap:.2rem;color:#8b949e;font-size:.82rem}
 input,select{padding:.45rem .55rem;border:1px solid #30363d;border-radius:.35rem;background:#010409;color:#c9d1d9;font:inherit}
 .inline{flex-direction:row;align-items:center;gap:.45rem;color:#c9d1d9}
 .row{display:flex;gap:.6rem;flex-wrap:wrap}
 .row label{flex:1;min-width:9rem}
 button{padding:.45rem .9rem;border-radius:.4rem;border:1px solid #30363d;background:#21262d;color:inherit;cursor:pointer;font:inherit}
 button:hover{background:#30363d}
 button:disabled{opacity:.5;cursor:default}
 .muted{color:#6e7681}
 .err{color:#ff7b72;white-space:pre-wrap;margin:.5rem 0}
 .ok{color:#3fb950}
 a{color:#79c0ff}
 fieldset{border:1px solid #21262d;border-radius:.5rem;padding:.7rem .8rem}
 legend{color:#8b949e;font-size:.8rem;padding:0 .3rem}
</style></head><body data-base="{{.BasePath}}">
<header>
  <a href="{{.BasePath}}">&larr; Fleet</a>
  <a href="{{.BasePath}}nodes">Nodes</a>
  <a href="{{.BasePath}}templates">Templates</a>
  <a href="{{.BasePath}}audit">Audit</a>
  <a href="{{.BasePath}}chat">AI</a>
  <h1>New server</h1>
  <span style="flex:1"></span><span class="muted">{{.Email}}</span>
</header>
<main>
  <p class="muted">Create from a template. The node agent starts the server after you submit.</p>
  <form id="create">
    <label>Name
      <input id="name" required maxlength="64" placeholder="survival-1">
    </label>
    <label>Node
      <select id="node"><option value="auto">Automatic (least loaded)</option></select>
    </label>
    <label>Template
      <select id="template" required><option value="">Loading templates…</option></select>
    </label>
    <fieldset>
      <legend>Optional overrides</legend>
      <div class="row">
        <label>Memory MB
          <input id="memory" type="number" min="512" max="32768" placeholder="template default">
        </label>
        <label>CPU cores
          <input id="cpu" type="number" min="0.1" max="16" step="0.1" placeholder="template default">
        </label>
        <label>Port
          <input id="port" type="number" min="1024" max="65535" placeholder="allocate automatically">
        </label>
      </div>
    </fieldset>
    <label class="inline"><input id="eula" type="checkbox" required> I accept the Minecraft EULA for this server.</label>
    <div class="row">
      <button id="submit" type="submit">Create server</button>
      <a href="{{.BasePath}}nodes" class="muted">Add or inspect nodes</a>
    </div>
    <p class="err" id="error"></p>
    <p class="ok" id="status"></p>
  </form>
</main>
<script>
(function () {
  'use strict';
  var base = document.body.dataset.base || '/';
  var api = base + 'api/v1';
  var nameEl = document.getElementById('name');
  var nodeEl = document.getElementById('node');
  var templateEl = document.getElementById('template');
  var memoryEl = document.getElementById('memory');
  var cpuEl = document.getElementById('cpu');
  var portEl = document.getElementById('port');
  var eulaEl = document.getElementById('eula');
  var errorEl = document.getElementById('error');
  var statusEl = document.getElementById('status');
  var submitBtn = document.getElementById('submit');
  function problem(text) {
    try { var p = JSON.parse(text); return p.detail || p.title || text; }
    catch (e) { return text; }
  }
  function fail(text) { errorEl.textContent = text || ''; statusEl.textContent = ''; submitBtn.disabled = false; }
  async function load() {
    try {
      var nr = await fetch(api + '/nodes?limit=200', { credentials: 'same-origin' });
      if (nr.ok) {
        var nd = await nr.json();
        (nd.items || []).forEach(function (node) {
          if (node.revoked_at) { return; }
          var opt = document.createElement('option');
          opt.value = node.id;
          opt.textContent = node.name + ' · ' + node.status + (node.fs_capability ? ' · ' + node.fs_capability : '');
          nodeEl.appendChild(opt);
        });
      }
      var tr = await fetch(api + '/templates?limit=200', { credentials: 'same-origin' });
      if (!tr.ok) { fail(problem(await tr.text())); return; }
      var td = await tr.json();
      templateEl.textContent = '';
      (td.items || []).forEach(function (tpl) {
        var opt = document.createElement('option');
        opt.value = tpl.id;
        opt.textContent = tpl.name + ' · ' + tpl.base + ' · ' + tpl.mc_version + (tpl.builtin ? ' · built-in' : '');
        templateEl.appendChild(opt);
      });
      if (!(td.items || []).length) {
        var opt = document.createElement('option');
        opt.value = '';
        opt.textContent = 'No templates available';
        templateEl.appendChild(opt);
      }
    } catch (e) { fail(String(e)); }
  }
  document.getElementById('create').addEventListener('submit', async function (ev) {
    ev.preventDefault();
    errorEl.textContent = '';
    submitBtn.disabled = true;
    statusEl.textContent = 'creating…';
    var body = {
      name: nameEl.value.trim(),
      node_id: nodeEl.value,
      template: templateEl.value,
      accept_eula: eulaEl.checked,
      overrides: {}
    };
    if (memoryEl.value) { body.overrides.memory_mb = String(memoryEl.value); }
    if (cpuEl.value) { body.overrides.cpu_cores = String(cpuEl.value); }
    if (portEl.value) { body.overrides.port = String(portEl.value); }
    try {
      var res = await fetch(api + '/servers', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(body)
      });
      if (!res.ok) { fail(problem(await res.text())); return; }
      var srv = await res.json();
      statusEl.textContent = 'created ' + srv.name + '; opening console…';
      location.href = base + 'servers/' + encodeURIComponent(srv.id);
    } catch (e) { fail(String(e)); }
  });
  load();
})();
</script>
</body></html>`))
