package api

import (
	"html/template"
	"net/http"
	"strings"
)

type pluginsPage struct {
	BasePath       string
	Email          string
	ID             string
	Name           string
	Base           string
	State          string
	MCVersion      string
	PendingPlugins bool
	PendingFiles   []string
	DevOf          string
}

func (s *server) renderPluginsPage(w http.ResponseWriter, r *http.Request) {
	principal, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	if s.opts.Services == nil || s.opts.Services.Servers == nil {
		http.Error(w, "the database is not configured", http.StatusNotImplemented)
		return
	}
	srv, err := s.opts.Services.Servers.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		status := http.StatusNotFound
		if strings.Contains(err.Error(), "forbidden") {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	data := pluginsPage{
		BasePath: s.base, Email: user.Email, ID: srv.ID, Name: srv.Name,
		Base: srv.Base, State: srv.SummaryState, MCVersion: srv.MCVersion,
		PendingPlugins: srv.PendingPlugins, PendingFiles: srv.PendingFiles,
		DevOf: srv.DevOf,
	}
	if data.State == "" {
		data.State = srv.ObservedState
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = pluginsTemplate.Execute(w, data)
}

var pluginsTemplate = template.Must(template.New("plugins").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Plugins · {{.Name}} · DelphiMC</title>
<style>
 :root{color-scheme:dark}
 *{box-sizing:border-box}
 body{margin:0;background:#0d1117;color:#c9d1d9;font:13px/1.45 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;min-height:100vh}
 header{display:flex;align-items:center;gap:.9rem;padding:.55rem .9rem;border-bottom:1px solid #21262d;background:#010409;flex-wrap:wrap}
 header a{color:#79c0ff;text-decoration:none}
 h1{font-size:.95rem;margin:0;font-weight:600;color:#e6edf3}
 h2{font-size:.88rem;margin:0 0 .6rem;color:#e6edf3}
 .pill{font-size:.72rem;padding:.15rem .5rem;border-radius:999px;border:1px solid #30363d;text-transform:uppercase;letter-spacing:.04em;color:#8b949e}
 main{max-width:1100px;margin:0 auto;padding:1rem}
 .grid{display:grid;grid-template-columns:1.2fr .8fr;gap:1rem;align-items:start}
 @media (max-width:900px){.grid{grid-template-columns:1fr}}
 section{border:1px solid #30363d;border-radius:.5rem;background:#161b22;padding:.8rem;margin-bottom:1rem}
 table{width:100%;border-collapse:collapse}
 th,td{text-align:left;padding:.35rem .3rem;border-bottom:1px solid #21262d;vertical-align:top}
 th{color:#8b949e;font-size:.75rem;text-transform:uppercase;letter-spacing:.04em}
 td.name{color:#e6edf3}
 .meta{color:#8b949e;font-size:.78rem}
 .actions{display:flex;gap:.4rem;flex-wrap:wrap}
 button{background:#21262d;border:1px solid #30363d;border-radius:.35rem;color:#c9d1d9;padding:.28rem .55rem;font:inherit;cursor:pointer}
 button:hover{background:#30363d}
 button.primary{background:#1f6feb;border-color:#1f6feb;color:#fff}
 button.danger{color:#ff7b72}
 button:disabled{opacity:.45;cursor:default}
 input,select{background:#010409;border:1px solid #30363d;border-radius:.35rem;color:inherit;padding:.3rem .45rem;font:inherit;outline:none}
 input:focus,select:focus{border-color:#388bfd}
 .row{display:flex;gap:.4rem;flex-wrap:wrap;align-items:center}
 .searchbar{display:flex;gap:.4rem;flex-wrap:wrap;margin-bottom:.7rem}
 .searchbar input[type=search]{flex:1;min-width:12rem}
 #results{display:flex;flex-direction:column;gap:.5rem;max-height:32rem;overflow:auto}
 .hit{display:flex;gap:.6rem;border:1px solid #21262d;border-radius:.4rem;padding:.5rem;background:#0d1117}
 .hit img{width:2rem;height:2rem;border-radius:.3rem;object-fit:cover;background:#21262d}
 .hit .body{flex:1;min-width:0}
 .hit .title{color:#e6edf3;font-weight:600}
 .hit .desc{color:#8b949e;font-size:.78rem;margin:.15rem 0}
 .hit .meta{font-size:.72rem}
 .badge{display:inline-block;border:1px solid #9e6a03;background:#3d2c00;color:#d29922;border-radius:.3rem;padding:.02rem .3rem;font-size:.7rem}
 .badge.ok{color:#3fb950;border-color:#238636;background:#0d2818}
 .hint{color:#6e7681;font-size:.75rem;margin:.5rem 0 0}
 .devbox{border:1px solid #30363d;border-radius:.5rem;background:#161b22;padding:.8rem;margin-bottom:1rem}
 .devbox.pending{border-color:#1f6feb;box-shadow:0 0 0 1px #1f6feb inset}
 .devpending{color:#79c0ff;font-size:.8rem;margin:.2rem 0 .5rem}
 #dev-status{margin-top:.5rem;color:#8b949e;font-size:.8rem;word-break:break-word}
 #promo-diff{margin-top:.5rem;max-height:26rem;overflow:auto}
 #promo-diff table{margin-top:.3rem}
 #promo-diff code{color:#79c0ff;font-size:.75rem}
 .promo-section{margin:.5rem 0}
 #toast{position:fixed;right:1rem;bottom:1rem;max-width:34rem;background:#161b22;border:1px solid #30363d;border-radius:.4rem;padding:.55rem .7rem;color:#e6edf3;display:none;white-space:pre-wrap}
 #toast.err{border-color:#f85149;color:#ff7b72}
 .loading{opacity:.55}
</style></head><body data-id="{{.ID}}" data-base="{{.BasePath}}" data-name="{{.Name}}" data-mc="{{.MCVersion}}">
<header>
  <a href="{{.BasePath}}">&larr; Fleet</a>
  <a href="{{.BasePath}}servers/{{.ID}}">Console</a>
  <a href="{{.BasePath}}servers/{{.ID}}/files">Files</a>
  <a href="{{.BasePath}}chat">AI</a>
  <h1>{{.Name}}</h1>
  <span class="pill">{{.Base}}</span>
  <span class="pill">{{.State}}</span>
  <span class="meta">Minecraft {{.MCVersion}}</span>
</header>
<main>
 {{if .DevOf}}
 <section id="promotion" class="devbox">
  <div class="row" style="justify-content:space-between">
   <h2>Promote to production</h2>
   <button id="promo-load">Review changes</button>
  </div>
  <p class="meta">Compare this dev clone with its production source. Selected plugins and config files are staged on production without restarting it.</p>
  <div id="promo-status" class="meta">Review changes to see the diff.</div>
  <div id="promo-diff"></div>
  <div class="row" style="margin-top:.5rem">
   <button id="promo-apply" class="primary" type="button" disabled>Promote selected</button>
  </div>
 </section>
 {{end}}
 <section id="devbox" class="devbox{{if or .PendingPlugins .PendingFiles}} pending{{end}}">
  <div class="row" style="justify-content:space-between">
   <h2>Test in a dev server</h2>
   {{if or .PendingPlugins .PendingFiles}}<span class="badge">changes staged</span>{{end}}
  </div>
  {{if or .PendingPlugins .PendingFiles}}
  <p class="devpending">Production has staged changes: {{if .PendingPlugins}}plugin set{{end}}{{if and .PendingPlugins .PendingFiles}} and {{end}}{{if .PendingFiles}}{{len .PendingFiles}} file(s){{end}}. A dev clone copies the current on-disk files, not the old loaded state.</p>
  {{else}}
  <p class="meta">Creates a temporary same-node copy for testing. Available even when production has no pending changes.</p>
  {{end}}
  <div class="row">
   <input id="dev-owner" style="flex:1;min-width:12rem" placeholder="Your Minecraft username" maxlength="32">
   <label class="check" style="color:#8b949e"><input id="dev-fresh" type="checkbox"> fresh world</label>
   <button id="dev-create" class="{{if or .PendingPlugins .PendingFiles}}primary{{end}}" type="button">Create dev server</button>
  </div>
  <p class="hint">The dev server gets a random port, 1 GB / 1 CPU, no proxy network, and a whitelist containing only this Minecraft user. It expires after 24 hours; restart it manually after testing plugin changes.</p>
  <div id="dev-status"></div>
 </section>
 <div class="grid">
  <div>
   <section>
    <div class="row" style="justify-content:space-between;margin-bottom:.4rem">
     <h2>Installed plugins</h2>
     <div class="actions">
      <button id="check">Check for updates</button>
      <button id="apply" class="primary" disabled>Apply updates</button>
     </div>
    </div>
    <div id="installed" class="meta">Loading…</div>
    <p class="hint">Checking runs automatically while this page is open. Installing, removing, or uploading a plugin stages the jar on disk without restarting the server; restart it when you are ready to load the change. Apply updates is an explicit restart with a boot check.</p>
   </section>
   <section>
    <h2>Available updates</h2>
    <div id="updates" class="meta">Run a check to inspect newer builds.</div>
   </section>
   <section>
    <h2>Automated updates</h2>
    <form id="autoform" class="row" style="margin-bottom:.5rem">
     <input id="cron" value="0 4 * * 1" size="14" aria-label="Cron schedule">
     <input id="tz" value="UTC" size="10" aria-label="Timezone">
     <button class="primary" type="submit">Schedule update</button>
    </form>
    <div id="schedules" class="meta">Loading schedules…</div>
    <p class="hint">The cron expression applies available plugin updates automatically. A running server restarts and is rolled back if the new revision does not boot.</p>
   </section>
  </div>
  <div>
   <section>
    <h2>Add a plugin</h2>
    <div class="searchbar">
     <select id="provider"><option value="modrinth">Modrinth</option><option value="hangar">Hangar</option></select>
     <input id="query" type="search" placeholder="Search LuckPerms, ViaVersion…">
     <button id="search" class="primary">Search</button>
    </div>
    <div id="results" class="meta">Search Modrinth or Hangar for a Paper plugin.</div>
    <p class="hint">Installing resolves the newest build compatible with Minecraft {{.MCVersion}} and downloads it through the panel's artifact store.</p>
    <details id="githubbox">
     <summary>Install from a GitHub repo</summary>
     <div class="row" style="margin-top:.5rem">
      <input id="github-name" style="flex:0 1 14rem" placeholder="Plugin name (optional)">
      <input id="github-repo" style="flex:1;min-width:14rem" placeholder="https://github.com/owner/repo">
      <button id="github-install" class="primary" type="button">Install latest release</button>
     </div>
     <p class="hint">Fetches the latest GitHub release, preferring a <code>paper</code> jar asset when the release has several jars.</p>
    </details>
    <details id="jenkinsbox">
     <summary>Install from Jenkins</summary>
     <div class="row" style="margin-top:.5rem">
      <input id="jenkins-name" style="flex:0 1 14rem" placeholder="Plugin name (optional)">
      <input id="jenkins-url" style="flex:1;min-width:14rem" placeholder="https://jenkins.example.com/job/my-plugin/">
      <button id="jenkins-install" class="primary" type="button">Install last successful build</button>
     </div>
     <p class="hint">Reads the job's <code>lastSuccessfulBuild</code> API and installs a jar artifact. The Jenkins job must be readable without authentication; the panel does not store Jenkins credentials.</p>
    </details>
    <details id="uploadbox">
     <summary>Upload a Spigot jar</summary>
     <form id="uploadform" enctype="multipart/form-data" style="display:flex;flex-direction:column;gap:.35rem;margin-top:.5rem">
      <input id="upload-project" placeholder="Project name (optional; default: filename)" maxlength="200">
      <input id="upload-version" placeholder="Version (optional)" maxlength="100">
      <input id="upload-file" type="file" accept=".jar,application/java-archive" required>
      <button class="primary" type="submit">Upload and install</button>
     </form>
     <p class="hint">For SpigotMC resources without a public API or direct download URL. The jar is stored in the panel's artifact store and pinned as a manual plugin; it is not checked for remote updates.</p>
    </details>
   </section>
  </div>
 </div>
</main>
<div id="toast"></div>
<script>
(function () {
  'use strict';
  var body = document.body;
  var id = body.dataset.id;
  var base = body.dataset.base || '/';
  var mc = body.dataset.mc || '';
  var api = base + 'api/v1/servers/' + encodeURIComponent(id);
  var installedEl = document.getElementById('installed');
  var updatesEl = document.getElementById('updates');
  var resultsEl = document.getElementById('results');
  var applyBtn = document.getElementById('apply');
  var checkBtn = document.getElementById('check');
  var autoForm = document.getElementById('autoform');
  var cronEl = document.getElementById('cron');
  var tzEl = document.getElementById('tz');
  var schedulesEl = document.getElementById('schedules');
  var serverName = body.dataset.name || id;
  var searchBtn = document.getElementById('search');
  var devbox = document.getElementById('devbox');
  var promoLoad = document.getElementById('promo-load');
  var promoApply = document.getElementById('promo-apply');
  var promoStatus = document.getElementById('promo-status');
  var promoDiff = document.getElementById('promo-diff');
  var devOwner = document.getElementById('dev-owner');
  var devFresh = document.getElementById('dev-fresh');
  var devCreate = document.getElementById('dev-create');
  var devStatus = document.getElementById('dev-status');
  var githubName = document.getElementById('github-name');
  var githubRepo = document.getElementById('github-repo');
  var githubInstall = document.getElementById('github-install');
  var jenkinsName = document.getElementById('jenkins-name');
  var jenkinsURL = document.getElementById('jenkins-url');
  var jenkinsInstall = document.getElementById('jenkins-install');
  var uploadForm = document.getElementById('uploadform');
  var uploadProject = document.getElementById('upload-project');
  var uploadVersion = document.getElementById('upload-version');
  var uploadFile = document.getElementById('upload-file');
  var providerEl = document.getElementById('provider');
  var queryEl = document.getElementById('query');
  var toastEl = document.getElementById('toast');
  var updateCount = 0;
  var installing = false;

  function toast(text, err) {
    toastEl.textContent = text;
    toastEl.className = err ? 'err' : '';
    toastEl.style.display = 'block';
    window.clearTimeout(toastEl._timer);
    toastEl._timer = window.setTimeout(function () { toastEl.style.display = 'none'; }, 7000);
  }
  function request(path, opts) {
    return fetch(path, opts).then(function (res) {
      return res.text().then(function (text) {
        var data = null;
        if (text) { try { data = JSON.parse(text); } catch (e) { data = null; } }
        if (!res.ok) {
          throw new Error((data && (data.detail || data.code)) || (text || res.status + ' ' + res.statusText));
        }
        return data;
      });
    });
  }
  function cell(text, className) {
    var td = document.createElement('td');
    if (className) { td.className = className; }
    td.textContent = text == null ? '' : String(text);
    return td;
  }
  function button(label, className, fn) {
    var b = document.createElement('button');
    b.type = 'button';
    b.textContent = label;
    if (className) { b.className = className; }
    b.addEventListener('click', fn);
    return b;
  }
  function renderInstalled(items) {
    installedEl.className = '';
    if (!items.length) {
      installedEl.textContent = 'No managed plugins. Add one from the catalog.';
      return;
    }
    var table = document.createElement('table');
    var head = document.createElement('tr');
    ['Plugin', 'Provider', 'Version', 'Policy', ''].forEach(function (label) {
      var th = document.createElement('th');
      th.textContent = label;
      head.appendChild(th);
    });
    table.appendChild(head);
    items.forEach(function (item) {
      var tr = document.createElement('tr');
      tr.appendChild(cell(item.project, 'name'));
      tr.appendChild(cell(item.provider));
      tr.appendChild(cell(item.version));
      tr.appendChild(cell(item.policy));
      var actions = document.createElement('td');
      actions.className = 'actions';
      actions.appendChild(button('Remove', 'danger', function () {
        if (!window.confirm('Remove ' + item.project + ' from this server?')) { return; }
        request(api + '/plugins?project=' + encodeURIComponent(item.project), {method: 'DELETE'})
          .then(function () { markStaged(); toast('Removed ' + item.project + ' — restart the server to unload it'); return refreshAll(); })
          .catch(function (err) { toast(err.message, true); });
      }));
      tr.appendChild(actions);
      table.appendChild(tr);
    });
    installedEl.textContent = '';
    installedEl.appendChild(table);
  }
  function renderUpdates(items, revision) {
    updateCount = items.length;
    applyBtn.disabled = updateCount === 0 || installing;
    updatesEl.className = '';
    if (!items.length) {
      updatesEl.innerHTML = '<span class="badge ok">up to date</span> revision ' + revision;
      return;
    }
    var table = document.createElement('table');
    var head = document.createElement('tr');
    ['Plugin', 'Current', 'Available', ''].forEach(function (label) {
      var th = document.createElement('th');
      th.textContent = label;
      head.appendChild(th);
    });
    table.appendChild(head);
    items.forEach(function (item) {
      var tr = document.createElement('tr');
      tr.appendChild(cell(item.project, 'name'));
      tr.appendChild(cell(item.current));
      var latest = cell(item.latest || (item.error ? 'error' : '—'));
      if (item.error) { latest.className = 'danger'; latest.title = item.error; }
      tr.appendChild(latest);
      var actions = document.createElement('td');
      actions.className = 'actions';
      actions.appendChild(button('Install', '', function () {
        install({provider: item.provider, project: item.project});
      }));
      tr.appendChild(actions);
      table.appendChild(tr);
    });
    updatesEl.textContent = '';
    updatesEl.appendChild(table);
    var hint = document.createElement('p');
    hint.className = 'hint';
    hint.textContent = 'Applying all updates downloads changed jars and restarts a running server. The new revision is rolled back if the server does not reach running.';
    updatesEl.appendChild(hint);
  }
  function loadInstalled() {
    return request(api + '/plugins').then(function (data) {
      renderInstalled((data && data.items) || []);
    }).catch(function (err) {
      installedEl.className = 'danger';
      installedEl.textContent = err.message;
    });
  }
  function checkUpdates(quiet) {
    checkBtn.disabled = true;
    if (!quiet) { updatesEl.textContent = 'Checking providers…'; }
    return request(api + '/updates').then(function (data) {
      renderUpdates((data && data.items) || [], data ? data.revision : '');
    }).catch(function (err) {
      updatesEl.className = 'danger';
      updatesEl.textContent = err.message;
    }).finally(function () {
      checkBtn.disabled = false;
    });
  }
  function renderSchedules(items) {
    schedulesEl.className = '';
    schedulesEl.textContent = '';
    if (!items.length) {
      schedulesEl.textContent = 'No automated update schedule.';
      return;
    }
    var table = document.createElement('table');
    items.forEach(function (item) {
      var tr = document.createElement('tr');
      tr.appendChild(cell(item.cron));
      tr.appendChild(cell(item.timezone));
      tr.appendChild(cell(item.next_run ? 'next ' + new Date(item.next_run).toLocaleString() : ''));
      var actions = document.createElement('td');
      actions.className = 'actions';
      actions.appendChild(button('Delete', 'danger', function () {
        if (!window.confirm('Delete this update schedule?')) { return; }
        request(base + 'api/v1/schedules/' + encodeURIComponent(item.id), {method: 'DELETE'})
          .then(function () { toast('Schedule deleted'); return loadSchedules(); })
          .catch(function (err) { toast(err.message, true); });
      }));
      tr.appendChild(actions);
      table.appendChild(tr);
    });
    schedulesEl.appendChild(table);
  }
  function loadSchedules() {
    return request(base + 'api/v1/schedules').then(function (data) {
      renderSchedules(((data && data.items) || []).filter(function (item) {
        return item.server_id === id && item.action === 'updates.apply';
      }));
    }).catch(function (err) {
      schedulesEl.className = 'danger';
      schedulesEl.textContent = err.message;
    });
  }
  function refreshAll() {
    return loadInstalled().then(function () { return loadSchedules(); }).then(function () { return checkUpdates(true); });
  }
  var promotion = null;

  function renderPromotion(preview) {
    promotion = preview;
    promoDiff.textContent = '';
    var hasPlugins = preview.plugins && preview.plugins.length > 0;
    var hasFiles = preview.files && preview.files.length > 0;
    if (!hasPlugins && !hasFiles) {
      promoStatus.textContent = 'No plugin or config differences.';
      promoApply.disabled = true;
      return;
    }
    promoStatus.textContent = 'Target: ' + preview.target_name + '. Select what to stage.';
    if (hasPlugins) {
      var pluginBox = document.createElement('div');
      pluginBox.className = 'promo-section';
      var pluginLabel = document.createElement('label');
      pluginLabel.className = 'check';
      var pluginCheck = document.createElement('input');
      pluginCheck.type = 'checkbox';
      pluginCheck.id = 'promo-plugins';
      pluginCheck.checked = true;
      pluginLabel.appendChild(pluginCheck);
      pluginLabel.appendChild(document.createTextNode(' Plugin set (' + preview.plugins.length + ' change(s))'));
      pluginBox.appendChild(pluginLabel);
      var table = document.createElement('table');
      preview.plugins.forEach(function (change) {
        var tr = document.createElement('tr');
        tr.appendChild(cell(change.project, 'name'));
        tr.appendChild(cell(change.change));
        tr.appendChild(cell(change.current || '—'));
        tr.appendChild(cell(change.next || '—'));
        table.appendChild(tr);
      });
      pluginBox.appendChild(table);
      promoDiff.appendChild(pluginBox);
    }
    if (hasFiles) {
      var fileBox = document.createElement('div');
      fileBox.className = 'promo-section';
      var title = document.createElement('div');
      title.className = 'meta';
      title.textContent = 'Config files';
      fileBox.appendChild(title);
      preview.files.forEach(function (file) {
        var label = document.createElement('label');
        label.className = 'check';
        label.style.display = 'flex';
        label.style.margin = '.15rem 0';
        var check = document.createElement('input');
        check.type = 'checkbox';
        check.checked = true;
        check.setAttribute('data-promo-file', file.path);
        label.appendChild(check);
        var code = document.createElement('code');
        code.textContent = file.path;
        label.appendChild(code);
        var status = document.createElement('span');
        status.className = 'meta';
        status.textContent = ' ' + file.status;
        label.appendChild(status);
        fileBox.appendChild(label);
      });
      promoDiff.appendChild(fileBox);
    }
    promoApply.disabled = false;
  }

  function loadPromotion() {
    if (!promoLoad) { return; }
    promoLoad.disabled = true;
    promoStatus.textContent = 'Comparing dev and production…';
    request(api + '/promotion').then(function (preview) {
      renderPromotion(preview);
    }).catch(function (err) {
      promoStatus.textContent = err.message;
    }).finally(function () { promoLoad.disabled = false; });
  }

  function applyPromotion() {
    if (!promotion) { return; }
    var files = Array.prototype.map.call(
      promoDiff.querySelectorAll('input[data-promo-file]:checked'),
      function (el) { return el.getAttribute('data-promo-file'); }
    );
    var pluginCheck = document.getElementById('promo-plugins');
    var plugins = !!(pluginCheck && pluginCheck.checked);
    if (!plugins && files.length === 0) {
      toast('Select at least one change to promote', true);
      return;
    }
    promoApply.disabled = true;
    request(api + '/promotion', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({plugins: plugins, files: files})
    }).then(function (result) {
      promoStatus.textContent = 'Staged ' + result.plugins + ' plugin change(s) and ' + result.files + ' file(s). Restart production to deploy.';
      var link = document.createElement('a');
      link.href = base + 'servers/' + encodeURIComponent(promotion.target_id);
      link.textContent = ' Open production console';
      promoStatus.appendChild(link);
      toast('Changes staged on production; restart it when ready');
    }).catch(function (err) {
      toast(err.message, true);
    }).finally(function () { promoApply.disabled = false; });
  }

  function markStaged() {
    if (!devbox) { return; }
    devbox.classList.add('pending');
    if (devCreate) { devCreate.classList.add('primary'); }
    if (devStatus) { devStatus.textContent = 'Changes staged; create a dev clone to test them.'; }
  }
  function install(entry) {
    if (installing) { return Promise.resolve(); }
    installing = true;
    applyBtn.disabled = true;
    return request(api + '/plugins', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({provider: entry.provider, project: entry.project, name: entry.name})
    }).then(function (result) {
      markStaged();
      toast('Installed ' + result.plugin.project + ' ' + result.plugin.version + ' — restart the server to load it');
      return refreshAll();
    }).catch(function (err) {
      toast(err.message, true);
    }).finally(function () {
      installing = false;
      applyBtn.disabled = updateCount === 0;
    });
  }
  function search() {
    var provider = providerEl.value;
    var query = queryEl.value.trim();
    resultsEl.className = 'meta';
    resultsEl.textContent = 'Searching ' + provider + '…';
    var path = base + 'api/v1/plugin-catalog?provider=' + encodeURIComponent(provider) +
      '&query=' + encodeURIComponent(query) +
      '&mc_version=' + encodeURIComponent(mc) + '&limit=20';
    request(path).then(function (data) {
      var items = (data && data.items) || [];
      resultsEl.className = '';
      resultsEl.textContent = '';
      if (!items.length) {
        resultsEl.className = 'meta';
        resultsEl.textContent = 'No matching Paper plugins.';
        return;
      }
      items.forEach(function (item) {
        var hit = document.createElement('div');
        hit.className = 'hit';
        if (item.icon_url) {
          var img = document.createElement('img');
          img.src = item.icon_url;
          img.alt = '';
          img.loading = 'lazy';
          hit.appendChild(img);
        }
        var bodyEl = document.createElement('div');
        bodyEl.className = 'body';
        var title = document.createElement('div');
        title.className = 'title';
        title.textContent = item.title || item.project;
        bodyEl.appendChild(title);
        var desc = document.createElement('div');
        desc.className = 'desc';
        desc.textContent = item.description || '';
        bodyEl.appendChild(desc);
        var meta = document.createElement('div');
        meta.className = 'meta';
        var parts = [item.project];
        if (item.author) { parts.push(item.author); }
        if (item.downloads) { parts.push(item.downloads.toLocaleString() + ' downloads'); }
        meta.textContent = parts.join(' · ');
        bodyEl.appendChild(meta);
        hit.appendChild(bodyEl);
        var add = button('Install', 'primary', function () { install(item); });
        hit.appendChild(add);
        resultsEl.appendChild(hit);
      });
    }).catch(function (err) {
      resultsEl.className = 'danger';
      resultsEl.textContent = err.message;
    });
  }
  devCreate.addEventListener('click', function () {
    var owner = devOwner.value.trim();
    if (!owner) { toast('Enter your Minecraft username', true); return; }
    devCreate.disabled = true;
    devStatus.textContent = 'Cloning the current server files…';
    request(api + '/dev', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({owner_username: owner, fresh_world: devFresh.checked})
    }).then(function (server) {
      devStatus.innerHTML = '';
      devStatus.appendChild(document.createTextNode('Dev server created: '));
      var link = document.createElement('a');
      link.href = base + 'servers/' + encodeURIComponent(server.id);
      link.textContent = server.name;
      devStatus.appendChild(link);
      toast('Dev server created; only ' + owner + ' is whitelisted');
    }).catch(function (err) {
      devStatus.textContent = '';
      toast(err.message, true);
    }).finally(function () { devCreate.disabled = false; });
  });
  jenkinsInstall.addEventListener('click', function () {
    var job = jenkinsURL.value.trim();
    if (!job) { toast('Enter a Jenkins job URL', true); return; }
    jenkinsInstall.disabled = true;
    install({provider: 'jenkins', project: job, name: jenkinsName.value.trim()}).finally(function () {
      jenkinsInstall.disabled = false;
    });
  });
  githubInstall.addEventListener('click', function () {
    var repo = githubRepo.value.trim();
    if (!repo) { toast('Enter a GitHub repository URL', true); return; }
    githubInstall.disabled = true;
    install({provider: 'github', project: repo, name: githubName.value.trim()}).finally(function () {
      githubInstall.disabled = false;
    });
  });
  uploadForm.addEventListener('submit', function (e) {
    e.preventDefault();
    if (!uploadFile.files.length) { toast('Choose a jar file', true); return; }
    var form = new FormData();
    form.append('project', uploadProject.value.trim());
    form.append('version', uploadVersion.value.trim());
    form.append('file', uploadFile.files[0]);
    var submit = uploadForm.querySelector('button[type=submit]');
    submit.disabled = true;
    request(api + '/plugins/upload', {method: 'POST', body: form})
      .then(function (result) {
        markStaged();
        toast('Installed ' + result.plugin.project + ' ' + result.plugin.version + ' — restart the server to load it');
        uploadForm.reset();
        return refreshAll();
      })
      .catch(function (err) { toast(err.message, true); })
      .finally(function () { submit.disabled = false; });
  });
  if (promoLoad) {
    promoLoad.addEventListener('click', loadPromotion);
    promoApply.addEventListener('click', applyPromotion);
    loadPromotion();
  }
  autoForm.addEventListener('submit', function (e) {
    e.preventDefault();
    var cron = cronEl.value.trim();
    if (!cron) { toast('Cron expression is required', true); return; }
    request(base + 'api/v1/schedules', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({
        name: 'Update plugins: ' + serverName,
        server_id: id,
        action: 'updates.apply',
        cron: cron,
        timezone: tzEl.value.trim() || 'UTC'
      })
    }).then(function () {
      toast('Automated update scheduled');
      return loadSchedules();
    }).catch(function (err) { toast(err.message, true); });
  });
  checkBtn.addEventListener('click', function () { checkUpdates(false); });
  applyBtn.addEventListener('click', function () {
    if (!window.confirm('Download and apply ' + updateCount + ' update(s)? A running server will restart.')) { return; }
    applyBtn.disabled = true;
    request(api + '/updates/apply', {method: 'POST'}).then(function (result) {
      toast('Applied ' + result.applied.length + ' update(s), revision ' + result.revision + '; boot check ' + result.boot_check);
      return refreshAll();
    }).catch(function (err) {
      toast(err.message, true);
      applyBtn.disabled = false;
    });
  });
  searchBtn.addEventListener('click', search);
  queryEl.addEventListener('keydown', function (e) { if (e.key === 'Enter') { search(); } });
  refreshAll();
  window.setInterval(function () {
    if (!document.hidden) { checkUpdates(true); }
  }, 300000);
})();
</script></body></html>
`))
