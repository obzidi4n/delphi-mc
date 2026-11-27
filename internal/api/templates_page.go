package api

import (
	"html/template"
	"net/http"
)

type templatesPage struct {
	BasePath string
	Email    string
}

func (s *server) renderTemplatesPage(w http.ResponseWriter, r *http.Request) {
	_, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = templatesTemplate.Execute(w, templatesPage{BasePath: s.base, Email: user.Email})
}

var templatesTemplate = template.Must(template.New("templates").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Templates · DelphiMC</title>
<style>
 :root{color-scheme:dark}
 *{box-sizing:border-box}
 body{margin:0;background:#0d1117;color:#c9d1d9;font:14px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;height:100vh;display:flex;flex-direction:column}
 header{display:flex;align-items:center;gap:.9rem;padding:.55rem .9rem;border-bottom:1px solid #21262d;background:#010409}
 header a{color:#79c0ff;text-decoration:none}
 header a.on{color:#e6edf3}
 h1{font-size:.95rem;margin:0;color:#e6edf3}
 main{flex:1;display:flex;min-height:0}
 aside{width:18rem;border-right:1px solid #21262d;overflow:auto;padding:.6rem}
 aside button{width:100%;margin-bottom:.5rem}
 #list{display:flex;flex-direction:column;gap:.2rem}
 .item{text-align:left;background:none;border:1px solid transparent;border-radius:.35rem;color:#c9d1d9;padding:.4rem .5rem;cursor:pointer;font:inherit}
 .item:hover,.item.on{background:#161b22;border-color:#30363d}
 .item .sub{display:block;color:#6e7681;font-size:.72rem}
 .pane{flex:1;display:flex;flex-direction:column;min-width:0}
 .toolbar{display:flex;gap:.5rem;align-items:center;padding:.45rem .8rem;border-bottom:1px solid #21262d;flex-wrap:wrap}
 .toolbar strong{color:#e6edf3}
 .meta{color:#6e7681;font-size:.78rem}
 .status{color:#8b949e;font-size:.78rem}
 .status.ok{color:#3fb950}
 .status.err{color:#f85149}
 .notice{margin:.55rem .8rem 0;padding:.5rem .7rem;border:1px solid #9e6a03;border-radius:.4rem;background:#1c1608;color:#e3b341;font-size:.82rem;display:none}
 .err{margin:.55rem .8rem 0;padding:.5rem .7rem;border:1px solid #6e2c2c;border-radius:.4rem;background:#2d0f10;color:#ff7b72;font:12px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;display:none}
 .files{border-bottom:1px solid #21262d;padding:.4rem .8rem;max-height:22rem;overflow:auto}
 .filesHead{display:flex;gap:.6rem;align-items:baseline;margin-bottom:.3rem}
 .filesHead strong{color:#e6edf3;font-size:.85rem}
 .filesHead .meta{font-size:.72rem}
 .file{border-bottom:1px solid #161b22;padding:.25rem 0}
 .file:last-child{border-bottom:0}
 .file summary{cursor:pointer;color:#79c0ff;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}
 .file .fileMeta{color:#6e7681;font-size:.7rem;margin-left:.4rem}
 .file textarea{flex:none;width:100%;height:9rem;margin:.35rem 0 .2rem;resize:vertical;border:1px solid #21262d;border-radius:.3rem;background:#010409;color:#c9d1d9;font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;padding:.5rem .6rem;outline:none;tab-size:2;white-space:pre;overflow:auto}
 textarea{flex:1;width:100%;resize:none;border:0;background:#010409;color:#c9d1d9;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;padding:.8rem 1rem;outline:none;tab-size:2;white-space:pre;overflow:auto}
 button{padding:.35rem .7rem;border-radius:.4rem;border:1px solid #30363d;background:#21262d;color:inherit;cursor:pointer;font:inherit}
 button:hover{background:#30363d}
 button:disabled{opacity:.45;cursor:default}
 .builtin{color:#d29922}
</style></head><body data-base="{{.BasePath}}">
<header>
  <a href="{{.BasePath}}">&larr; Fleet</a>
  <a href="{{.BasePath}}groups">Groups</a>
  <a href="{{.BasePath}}templates" class="on">Templates</a>
  <a href="{{.BasePath}}audit">Audit</a>
  <a href="{{.BasePath}}chat">AI</a>
  <h1>Templates</h1>
  <span style="flex:1"></span><span class="meta">{{.Email}}</span>
</header>
<main>
  <aside>
    <button id="new">New template</button>
    <div id="list"></div>
  </aside>
  <section class="pane">
    <div class="toolbar">
      <strong id="title">Select a template</strong>
      <span class="meta" id="meta"></span>
      <span style="flex:1"></span>
      <button id="save" disabled>Save</button>
      <button id="copy" disabled>Save as copy</button>
      <span class="status" id="status"></span>
    </div>
    <div class="notice" id="notice"></div>
    <pre class="err" id="error"></pre>
    <div class="files" id="filesPanel" style="display:none">
      <div class="filesHead"><strong>Files</strong><span class="meta" id="filesHint"></span></div>
      <div id="filesList"></div>
    </div>
    <textarea id="editor" spellcheck="false" placeholder="Select a template or create a new one."></textarea>
  </section>
</main>
<script>
(function () {
  'use strict';
  var base = document.body.dataset.base || '/';
  var api = base + 'api/v1/templates';
  var listEl = document.getElementById('list');
  var editor = document.getElementById('editor');
  var titleEl = document.getElementById('title');
  var metaEl = document.getElementById('meta');
  var statusEl = document.getElementById('status');
  var noticeEl = document.getElementById('notice');
  var errorEl = document.getElementById('error');
  var filesPanel = document.getElementById('filesPanel');
  var filesList = document.getElementById('filesList');
  var filesHint = document.getElementById('filesHint');
  var saveBtn = document.getElementById('save');
  var copyBtn = document.getElementById('copy');
  var current = null;
  var filesTimer = null;

  function parseBody() {
    try { return JSON.parse(editor.value); }
    catch (e) { return null; }
  }
  function setFileValue(path, value, meta) {
    var obj = parseBody();
    if (!obj) { return; }
    if (!obj.files || typeof obj.files !== 'object') { obj.files = {}; }
    obj.files[path] = value;
    editor.value = JSON.stringify(obj, null, 2) + '\n';
    meta.textContent = value.split('\n').length + ' lines';
  }
  function renderFiles() {
    var obj = parseBody();
    filesList.textContent = '';
    if (!obj) {
      filesHint.textContent = 'Fix the JSON body to edit files.';
      return;
    }
    var files = obj.files && typeof obj.files === 'object' ? obj.files : {};
    var names = Object.keys(files).sort();
    filesHint.textContent = names.length + ' file' + (names.length === 1 ? '' : 's');
    if (!names.length) {
      var empty = document.createElement('p');
      empty.className = 'meta';
      empty.textContent = 'No files in this template.';
      filesList.appendChild(empty);
      return;
    }
    names.forEach(function (name) {
      var content = String(files[name] == null ? '' : files[name]);
      var details = document.createElement('details');
      details.className = 'file';
      details.open = names.length === 1;
      var summary = document.createElement('summary');
      summary.textContent = name;
      var meta = document.createElement('span');
      meta.className = 'fileMeta';
      meta.textContent = content.split('\n').length + ' lines';
      summary.appendChild(meta);
      var ta = document.createElement('textarea');
      ta.value = content;
      ta.spellcheck = false;
      ta.setAttribute('aria-label', name);
      ta.addEventListener('input', function () { setFileValue(name, ta.value, meta); });
      details.appendChild(summary);
      details.appendChild(ta);
      filesList.appendChild(details);
    });
  }
  function scheduleFiles() {
    if (filesTimer) { clearTimeout(filesTimer); }
    filesTimer = setTimeout(renderFiles, 250);
  }
  function setStatus(text, ok) {
    statusEl.textContent = text || '';
    statusEl.className = 'status' + (ok === true ? ' ok' : ok === false ? ' err' : '');
  }
  function clearError() {
    errorEl.textContent = '';
    errorEl.style.display = 'none';
  }
  function showError(text) {
    errorEl.textContent = text || 'request failed';
    errorEl.style.display = 'block';
    setStatus('', false);
  }
  function problem(text) {
    try {
      var body = JSON.parse(text);
      return body.detail || body.title || text;
    } catch (e) { return text; }
  }
  function pretty(text) {
    try { return JSON.stringify(JSON.parse(text), null, 2); }
    catch (e) { return text; }
  }
  function skeleton() {
    return JSON.stringify({
      schema_version: 1,
      name: 'New Template',
      description: '',
      base: 'paper',
      mc_version: '1.21.11',
      java: 21,
      defaults: { memory_mb: 4096, cpu_cores: 2, port: 0 },
      files: {},
      plugins: [],
      variables: {}
    }, null, 2) + '\n';
  }
  function setCurrent(tpl) {
    current = tpl;
    titleEl.textContent = tpl ? tpl.name : 'New template';
    metaEl.textContent = tpl ? tpl.base + ' · ' + tpl.mc_version + ' · rev ' + tpl.revision : '';
    noticeEl.style.display = tpl && tpl.builtin ? 'block' : 'none';
    noticeEl.textContent = tpl && tpl.builtin ? 'Built-in templates are read-only. Save as copy to make an editable template.' : '';
    saveBtn.disabled = !tpl || tpl.builtin;
    copyBtn.disabled = !tpl;
  }
  function openNew() {
    setCurrent(null);
    saveBtn.disabled = false;
    filesPanel.style.display = 'block';
    editor.value = skeleton();
    renderFiles();
    setStatus('');
    clearError();
  }
  async function loadList() {
    var res = await fetch(api + '?limit=200', { credentials: 'same-origin' });
    if (!res.ok) { showError(problem(await res.text())); return; }
    var data = await res.json();
    listEl.textContent = '';
    (data.items || []).forEach(function (tpl) {
      var b = document.createElement('button');
      b.className = 'item' + (current && current.id === tpl.id ? ' on' : '');
      b.dataset.id = tpl.id;
      b.textContent = tpl.name;
      var sub = document.createElement('span');
      sub.className = 'sub';
      sub.textContent = tpl.base + ' · ' + tpl.mc_version + (tpl.builtin ? ' · built-in' : '') + ' · rev ' + tpl.revision;
      b.appendChild(sub);
      b.addEventListener('click', function () { openTemplate(tpl.id); });
      listEl.appendChild(b);
    });
    if (!(data.items || []).length) {
      var empty = document.createElement('p');
      empty.className = 'meta';
      empty.textContent = 'No templates.';
      listEl.appendChild(empty);
    }
  }
  async function openTemplate(id) {
    clearError();
    setStatus('loading…');
    var res = await fetch(api + '/' + encodeURIComponent(id), { credentials: 'same-origin' });
    if (!res.ok) { setCurrent(null); showError(problem(await res.text())); return; }
    var tpl = await res.json();
    setCurrent(tpl);
    filesPanel.style.display = 'block';
    editor.value = pretty(tpl.body || '{}');
    renderFiles();
    setStatus('');
    loadList();
  }
  async function save(method) {
    var body = editor.value.trim();
    if (!body) { showError('template body is required'); return; }
    clearError();
    var url = api;
    if (method === 'save' && current && !current.builtin) {
      url += '/' + encodeURIComponent(current.id);
    }
    if (method === 'copy') {
      try {
        var parsed = JSON.parse(body);
        parsed.name = (parsed.name || 'Template') + ' copy';
        delete parsed.id;
        body = JSON.stringify(parsed, null, 2) + '\n';
        editor.value = body;
      } catch (e) {
        showError('template must be valid JSON: ' + e.message);
        return;
      }
    }
    var httpMethod = method === 'save' && current && !current.builtin ? 'PUT' : 'POST';
    setStatus(method === 'copy' ? 'copying…' : 'saving…');
    var res = await fetch(url, {
      method: httpMethod, credentials: 'same-origin',
      headers: { 'content-type': 'application/json' },
      body: body
    });
    if (!res.ok) { showError(problem(await res.text())); return; }
    var saved = await res.json();
    setCurrent(saved);
    filesPanel.style.display = 'block';
    editor.value = pretty(saved.body || body);
    renderFiles();
    setStatus('saved ' + saved.name + ' · rev ' + saved.revision, true);
    loadList();
  }

  editor.addEventListener('input', scheduleFiles);
  document.getElementById('new').addEventListener('click', openNew);
  saveBtn.addEventListener('click', function () { save('save'); });
  copyBtn.addEventListener('click', function () { save('copy'); });
  loadList();
})();
</script>
</body></html>`))
