package api

import (
	"encoding/json"
	"html/template"
	"net/http"

	"delphi.dev/delphi/internal/serverprops"
)

type filesPage struct {
	BasePath       string
	Email          string
	ID             string
	Name           string
	Version        string
	PropertiesJSON template.JS
}

func (s *server) renderFiles(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	properties, err := json.Marshal(serverprops.Reference(srv.MCVersion))
	if err != nil {
		properties = []byte("[]")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = filesTemplate.Execute(w, filesPage{
		BasePath: s.base, Email: user.Email, ID: srv.ID, Name: srv.Name,
		Version: srv.MCVersion, PropertiesJSON: template.JS(properties),
	})
}

var filesTemplate = template.Must(template.New("files").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Name}} · Files · DelphiMC</title>
<style>
 :root{color-scheme:dark}
 *{box-sizing:border-box}
 body{margin:0;background:#0d1117;color:#c9d1d9;font:14px/1.5 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;height:100vh;display:flex;flex-direction:column}
 header{display:flex;align-items:center;gap:.9rem;padding:.55rem .9rem;border-bottom:1px solid #21262d;background:#010409}
 header a{color:#79c0ff;text-decoration:none}
 h1{font-size:.95rem;margin:0;color:#e6edf3}
 main{flex:1;display:flex;min-height:0}
 aside{width:17rem;border-right:1px solid #21262d;display:flex;flex-direction:column;min-height:0}
 .crumbs{display:flex;gap:.3rem;align-items:center;padding:.4rem .6rem;border-bottom:1px solid #21262d;font-size:.8rem;color:#8b949e;flex-wrap:wrap}
 .crumbs button{padding:.05rem .3rem;font-size:.78rem}
 #list{flex:1;overflow:auto;padding:.2rem 0}
 .entry{display:block;width:100%;text-align:left;background:none;border:0;color:#c9d1d9;padding:.25rem .6rem;font:inherit;cursor:pointer;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
 .entry:hover{background:#161b22}
 .entry.dir{color:#79c0ff}
 .entry .meta{color:#6e7681;font-size:.75rem;margin-left:.35rem}
 .pane{flex:1;display:flex;flex-direction:column;min-width:0}
 .toolbar{display:flex;gap:.5rem;align-items:center;padding:.45rem .8rem;border-bottom:1px solid #21262d;flex-wrap:wrap}
 .toolbar input{flex:1;min-width:12rem;background:#010409;border:1px solid #30363d;border-radius:.35rem;color:inherit;padding:.3rem .5rem;font:inherit}
 button{padding:.3rem .65rem;background:#21262d;border:1px solid #30363d;border-radius:.35rem;color:inherit;font:inherit;cursor:pointer}
 button:hover{background:#30363d}
 button:disabled{opacity:.45;cursor:default}
 .editorwrap{flex:1;display:flex;min-height:0;min-width:0;background:#010409}
 .gutter{flex:none;min-width:3.2rem;padding:.7rem .55rem .7rem .7rem;border-right:1px solid #21262d;background:#0d1117;color:#6e7681;text-align:right;white-space:pre;overflow:hidden;user-select:none;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
 textarea{flex:1;width:auto;min-width:0;height:100%;resize:none;border:0;background:#010409;color:#c9d1d9;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;padding:.7rem .9rem;outline:none;tab-size:4;white-space:pre;overflow:auto}
 .revs{width:19rem;border-left:1px solid #21262d;overflow:auto;padding:.5rem .6rem}
 .revs h2{font-size:.8rem;text-transform:uppercase;letter-spacing:.05em;color:#8b949e;margin:.2rem 0 .5rem}
 .rev{border-bottom:1px solid #21262d;padding:.35rem 0;font-size:.8rem}
 .rev .when{color:#6e7681}
 .rev .row{display:flex;gap:.3rem;margin-top:.2rem}
 #diff{flex:1;overflow:auto;margin:0;padding:.7rem .9rem;background:#010409;white-space:pre;font:13px/1.5 ui-monospace,Menlo,monospace;display:none}
 .status{color:#8b949e;font-size:.8rem}
 .status.ok{color:#3fb950}
 .status.err{color:#f85149}
 .lintbar{height:2.1rem;box-sizing:border-box;display:flex;align-items:center;padding:0 .8rem;border-bottom:1px solid #21262d;background:#010409;color:#8b949e;font:12px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
 .lintbar.ok{color:#3fb950;background:#0d1b12;border-bottom-color:#238636}
 .lintbar.err{color:#ff7b72;background:#2d0f10;border-bottom-color:#6e2c2c}
 .lintbar.warn{color:#e3b341;background:#221a06;border-bottom-color:#9e6a03}
 .props{border-bottom:1px solid #21262d;padding-bottom:.5rem;margin-bottom:.5rem}
 .props input{width:100%;background:#010409;border:1px solid #30363d;border-radius:.35rem;color:inherit;padding:.25rem .45rem;font:inherit;margin-bottom:.4rem}
 .propsList{max-height:22rem;overflow:auto}
 .prop{padding:.3rem 0;border-bottom:1px solid #161b22}
 .prop:last-child{border-bottom:0}
 .prop code{color:#79c0ff;font:12px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace}
 .prop .ptype{color:#6e7681;font-size:.7rem;margin-left:.3rem}
 .prop .pdesc{font-size:.76rem;line-height:1.35;margin:.12rem 0;color:#c9d1d9}
 .prop .pmeta{font-size:.68rem;line-height:1.35;color:#6e7681}
 .nbtview{flex:1;overflow:auto;padding:.7rem .9rem;display:none;font:13px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}
 .nbtmeta{color:#8b949e;font-size:.78rem;margin-bottom:.6rem}
 .nbtnode{margin:.1rem 0 .1rem 1rem}
 .nbtnode summary,.nbthead{cursor:default;color:#c9d1d9}
 .nbtnode summary{cursor:pointer}
 .nbtname{color:#79c0ff}
 .nbttype{color:#6e7681;font-size:.72rem;margin-left:.35rem}
 .nbtval{color:#a5d6ff;margin-left:.4rem;white-space:pre-wrap}
 .nbtlen{color:#6e7681;font-size:.75rem;margin-left:.35rem}
 .nbtDownload{display:none;color:#79c0ff;text-decoration:none;padding:.3rem .65rem;border:1px solid #30363d;border-radius:.35rem}
</style></head><body data-id="{{.ID}}" data-base="{{.BasePath}}" data-version="{{.Version}}">
<header>
  <a href="{{.BasePath}}">&larr; Fleet</a>
  <a href="{{.BasePath}}servers/{{.ID}}">Console</a>
  <a href="{{.BasePath}}chat">AI</a>
  <h1>{{.Name}} · Files</h1>
  <span style="flex:1"></span><span class="status">{{.Email}}</span>
</header>
<script type="application/json" id="propertiesReference">{{.PropertiesJSON}}</script>
<main>
  <aside>
    <div class="crumbs" id="crumbs"></div>
    <div id="list"></div>
  </aside>
  <section class="pane">
    <div class="toolbar">
      <input id="path" placeholder="path/to/file (Open, or type a new path and Save)">
      <button id="open">Open</button>
      <button id="save" disabled>Save</button>
      <button id="diffBtn" disabled>Diff latest</button>
      <a class="nbtDownload" id="nbtDownload" download>Download raw</a>
      <span class="status" id="status"></span>
    </div>
    <div class="lintbar" id="lintbar">Open server.properties or a .json, .yaml, .yml, or .toml file to lint.</div>
    <div class="editorwrap" id="editorWrap">
      <div class="gutter" id="lineNumbers" aria-hidden="true">1</div>
      <textarea id="editor" spellcheck="false" wrap="off" placeholder="Open a file from the list, or type a path and press Open."></textarea>
    </div>
    <div class="nbtview" id="nbtView"></div>
    <pre id="diff"></pre>
  </section>
  <div class="revs">
    <div class="props" id="propsRef" style="display:none">
      <h2>server.properties <span id="propsVersion"></span></h2>
      <input id="propsFilter" placeholder="Filter properties">
      <div class="propsList" id="propsList"></div>
    </div>
    <h2>Revisions</h2>
    <div id="revisions"><p class="status">Open a file to see its revisions.</p></div>
  </div>
</main>
<script>
(function () {
  'use strict';
  var base = document.body.dataset.base || '/';
  var id = document.body.dataset.id;
  var version = document.body.dataset.version || '';
  var api = base + 'api/v1/servers/' + encodeURIComponent(id) + '/files';
  var propertiesReference = [];
  try {
    propertiesReference = JSON.parse(document.getElementById('propertiesReference').textContent || '[]');
  } catch (e) { propertiesReference = []; }
  var listEl = document.getElementById('list');
  var crumbsEl = document.getElementById('crumbs');
  var pathInput = document.getElementById('path');
  var editor = document.getElementById('editor');
  var saveBtn = document.getElementById('save');
  var diffBtn = document.getElementById('diffBtn');
  var statusEl = document.getElementById('status');
  var revisionsEl = document.getElementById('revisions');
  var diffEl = document.getElementById('diff');
  var editorWrap = document.getElementById('editorWrap');
  var lineNumbers = document.getElementById('lineNumbers');
  var nbtView = document.getElementById('nbtView');
  var nbtDownload = document.getElementById('nbtDownload');
  var lintbar = document.getElementById('lintbar');
  var propsRef = document.getElementById('propsRef');
  var propsList = document.getElementById('propsList');
  var propsFilter = document.getElementById('propsFilter');
  var propsVersion = document.getElementById('propsVersion');
  var dir = '';
  var currentPath = '';
  var currentSHA = '';
  var latestRevision = 0;
  var dirty = false;
  var lintTimer = null;
  var lintSeq = 0;
  var openSeq = 0;

  function enc(value) { return encodeURIComponent(value); }
  function setStatus(text, ok) {
    statusEl.textContent = text || '';
    statusEl.className = 'status' + (ok === true ? ' ok' : ok === false ? ' err' : '');
  }
  function join(parent, name) { return parent ? parent + '/' + name : name; }
  function renderLineNumbers() {
    var text = editor.value;
    var count = 1;
    for (var i = 0; i < text.length; i++) {
      if (text.charCodeAt(i) === 10) { count++; }
    }
    var lines = new Array(count);
    for (var n = 0; n < count; n++) { lines[n] = String(n + 1); }
    lineNumbers.textContent = lines.join('\n');
    lineNumbers.style.width = (String(count).length + 3) + 'ch';
    lineNumbers.scrollTop = editor.scrollTop;
  }
  function isServerProperties(path) {
    return String(path || '').split('/').pop() === 'server.properties';
  }
  function isNBTFile(path) {
    return /\.(dat|dat_old|nbt)$/i.test(path || '');
  }
  function isUIDFile(path) {
    return String(path || '').split('/').pop() === 'uid.dat';
  }
  function uuidFromBytes(buffer) {
    if (!buffer || buffer.byteLength !== 16) { return ''; }
    var bytes = new Uint8Array(buffer);
    var hex = '';
    for (var i = 0; i < bytes.length; i++) { hex += bytes[i].toString(16).padStart(2, '0'); }
    return hex.slice(0, 8) + '-' + hex.slice(8, 12) + '-' + hex.slice(12, 16) + '-' +
      hex.slice(16, 20) + '-' + hex.slice(20);
  }
  async function openUID(path) {
    pathInput.value = path;
    setStatus('loading ' + path);
    var seq = ++openSeq;
    currentPath = path;
    currentSHA = '';
    latestRevision = 0;
    dirty = false;
    saveBtn.disabled = true;
    diffBtn.disabled = true;
    showProperties('');
    nbtDownload.href = base + 'servers/' + encodeURIComponent(id) + '/files/download?path=' + enc(path);
    var res = await fetch(nbtDownload.href, { credentials: 'same-origin' });
    if (seq !== openSeq) { return; }
    if (!res.ok) {
      showNBTError('Could not read ' + path + ': HTTP ' + res.status);
      setStatus('read failed: ' + path, false);
      return;
    }
    var buffer = await res.arrayBuffer();
    if (seq !== openSeq) { return; }
    var uuid = uuidFromBytes(buffer);
    nbtView.textContent = '';
    var meta = document.createElement('div');
    meta.className = 'nbtmeta';
    meta.textContent = 'World UUID · ' + buffer.byteLength + ' bytes · read-only';
    nbtView.appendChild(meta);
    var value = document.createElement('div');
    value.className = 'nbtval';
    value.textContent = uuid || 'Binary data (not a 16-byte UUID)';
    nbtView.appendChild(value);
    if (!uuid) {
      var bytes = new Uint8Array(buffer);
      var hex = '';
      for (var i = 0; i < Math.min(bytes.length, 256); i++) { hex += bytes[i].toString(16).padStart(2, '0') + ' '; }
      var hexEl = document.createElement('div');
      hexEl.className = 'nbtmeta';
      hexEl.textContent = hex + (bytes.length > 256 ? '…' : '');
      nbtView.appendChild(hexEl);
    }
    showNBT();
    lintbar.textContent = 'binary read-only · uid.dat';
    lintbar.className = 'lintbar';
    lintbar.title = '';
    revisionsEl.textContent = 'Binary files are read-only.';
    setStatus('loaded ' + path + ' (' + buffer.byteLength + ' B)');
  }
  function showNBT() {
    editorWrap.style.display = 'none';
    diffEl.style.display = 'none';
    nbtView.style.display = 'block';
    nbtDownload.style.display = 'inline-block';
  }
  function nbtNode(node, depth, root) {
    var hasChildren = node.children && node.children.length > 0;
    var el = document.createElement(hasChildren ? 'details' : 'div');
    el.className = 'nbtnode';
    if (hasChildren) { el.open = depth < 2; }
    var head = document.createElement(hasChildren ? 'summary' : 'div');
    head.className = 'nbthead';
    if (node.name || root) {
      var name = document.createElement('span');
      name.className = 'nbtname';
      name.textContent = node.name || 'root';
      head.appendChild(name);
    }
    var type = document.createElement('span');
    type.className = 'nbttype';
    type.textContent = node.type;
    head.appendChild(type);
    if (hasChildren) {
      var count = document.createElement('span');
      count.className = 'nbtlen';
      count.textContent = (node.type === 'List' ? '[' + node.length + ']' : '{' + node.length + '}');
      head.appendChild(count);
    } else if (node.value !== '' && node.value !== undefined) {
      var value = document.createElement('span');
      value.className = 'nbtval';
      value.textContent = node.value + (node.truncated ? '…' : '');
      head.appendChild(value);
    }
    el.appendChild(head);
    if (hasChildren) {
      node.children.forEach(function (child) { el.appendChild(nbtNode(child, depth + 1, false)); });
    }
    return el;
  }
  function renderNBT(doc) {
    nbtView.textContent = '';
    var meta = document.createElement('div');
    meta.className = 'nbtmeta';
    meta.textContent = 'NBT · ' + doc.compression + ' · ' + doc.size + ' bytes compressed · ' +
      doc.decompressed_size + ' bytes decompressed · read-only';
    nbtView.appendChild(meta);
    nbtView.appendChild(nbtNode(doc.root, 0, true));
  }
  function showNBTError(message) {
    nbtView.textContent = '';
    var p = document.createElement('p');
    p.className = 'status err';
    p.textContent = message;
    nbtView.appendChild(p);
    showNBT();
  }
  function problemDetail(text) {
    try {
      var body = JSON.parse(text);
      return body.detail || body.title || text;
    } catch (e) { return text; }
  }
  async function openNBT(path) {
    pathInput.value = path;
    setStatus('loading NBT ' + path);
    var seq = ++openSeq;
    currentPath = path;
    currentSHA = '';
    latestRevision = 0;
    dirty = false;
    saveBtn.disabled = true;
    diffBtn.disabled = true;
    showProperties('');
    nbtDownload.href = base + 'servers/' + encodeURIComponent(id) + '/files/download?path=' + enc(path);
    var res = await fetch(api + '/nbt?path=' + enc(path), { credentials: 'same-origin' });
    if (seq !== openSeq) { return; }
    if (!res.ok) {
      var msg = problemDetail(await res.text());
      lintbar.textContent = 'NBT error';
      lintbar.className = 'lintbar err';
      lintbar.title = msg;
      showNBTError('Not an NBT file: ' + (msg || ('HTTP ' + res.status)));
      setStatus('NBT error: ' + path, false);
      revisionsEl.textContent = 'NBT files are read-only.';
      return;
    }
    var doc = await res.json();
    if (seq !== openSeq) { return; }
    renderNBT(doc);
    showNBT();
    lintbar.textContent = 'NBT read-only · ' + doc.compression;
    lintbar.className = 'lintbar';
    lintbar.title = '';
    revisionsEl.textContent = 'NBT files are read-only.';
    setStatus('loaded NBT ' + path + ' (' + doc.size + ' B)');
  }
  function configKind(path) {
    if (isServerProperties(path)) { return 'properties'; }
    var m = /\.([A-Za-z0-9]+)$/.exec(path || '');
    if (!m) { return ''; }
    var ext = m[1].toLowerCase();
    if (ext === 'yml') { return 'yaml'; }
    return ext === 'yaml' || ext === 'json' || ext === 'toml' ? ext : '';
  }
  function renderProperties(filter) {
    var q = String(filter || '').toLowerCase();
    var shown = 0;
    propsList.textContent = '';
    propertiesReference.forEach(function (key) {
      var hay = (key.name + ' ' + key.description + ' ' + (key.allowed || '')).toLowerCase();
      if (q && hay.indexOf(q) < 0) { return; }
      shown++;
      var box = document.createElement('div');
      box.className = 'prop';
      var head = document.createElement('div');
      var code = document.createElement('code');
      code.textContent = key.name;
      var typ = document.createElement('span');
      typ.className = 'ptype';
      typ.textContent = key.type + (key.since ? ' · since ' + key.since : '');
      head.appendChild(code);
      head.appendChild(typ);
      var desc = document.createElement('div');
      desc.className = 'pdesc';
      desc.textContent = key.description || '';
      var meta = document.createElement('div');
      meta.className = 'pmeta';
      meta.textContent = 'Default: ' + (key.default || '(empty)') +
        (key.allowed ? ' · Allowed: ' + key.allowed : '');
      box.appendChild(head);
      box.appendChild(desc);
      box.appendChild(meta);
      propsList.appendChild(box);
    });
    if (!shown) {
      var empty = document.createElement('p');
      empty.className = 'status';
      empty.textContent = propertiesReference.length ? 'No matching properties.' : 'No reference data for this server version.';
      propsList.appendChild(empty);
    }
  }
  function showProperties(path) {
    if (!isServerProperties(path)) {
      propsRef.style.display = 'none';
      return;
    }
    propsRef.style.display = 'block';
    propsVersion.textContent = version ? '· Minecraft ' + version : '· version unknown';
    propsFilter.value = '';
    renderProperties('');
  }
  function clearLint() {
    lintbar.textContent = currentPath ? 'Not a config file.' : 'Open server.properties or a .json, .yaml, .yml, or .toml file to lint.';
    lintbar.className = 'lintbar';
    lintbar.title = '';
    if (currentPath) { saveBtn.disabled = false; }
  }
  function applyLint(result) {
    if (!result) { clearLint(); return; }
    var valid = !!result.valid;
    var kind = (result.format || configKind(currentPath) || 'config').toUpperCase();
    var warning = valid ? (result.warning || '') : '';
    lintbar.textContent = valid
      ? (warning ? kind + ' warning: ' + warning : kind + ' valid')
      : kind + ' error: ' + (result.error || 'invalid ' + kind);
    lintbar.className = 'lintbar ' + (warning ? 'warn' : valid ? 'ok' : 'err');
    lintbar.title = lintbar.textContent;
    saveBtn.disabled = !valid;
  }
  async function checkConfig(path, content) {
    try {
      var res = await fetch(base + 'api/v1/config/format', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ path: path, content: content, version: version })
      });
      if (!res.ok) {
        return { format: configKind(path), valid: false, error: 'lint failed: HTTP ' + res.status + ' ' + (await res.text()).slice(0, 300) };
      }
      return await res.json();
    } catch (e) {
      return { format: configKind(path), valid: false, error: 'lint unavailable: ' + String(e) };
    }
  }
  function scheduleLint() {
    if (lintTimer) { clearTimeout(lintTimer); lintTimer = null; }
    var kind = configKind(currentPath);
    if (!kind) { clearLint(); return; }
    lintbar.textContent = 'Checking ' + kind.toUpperCase() + '…';
    lintbar.className = 'lintbar';
    lintbar.title = '';
    saveBtn.disabled = true;
    lintTimer = setTimeout(lintCurrent, 300);
  }
  async function lintCurrent() {
    var path = currentPath;
    if (!path || !configKind(path)) { return; }
    var seq = ++lintSeq;
    var result = await checkConfig(path, editor.value);
    if (seq !== lintSeq || path !== currentPath) { return; }
    applyLint(result);
  }

  async function loadDir(path) {
    try {
      var res = await fetch(api + '?path=' + enc(path), { credentials: 'same-origin' });
      if (!res.ok) { setStatus('cannot list: HTTP ' + res.status, false); return; }
      var data = await res.json();
      dir = path;
      renderCrumbs(path);
      listEl.textContent = '';
      (data.entries || []).forEach(function (entry) {
        var button = document.createElement('button');
        button.className = 'entry' + (entry.dir ? ' dir' : '');
        button.textContent = entry.name;
        if (!entry.dir) {
          var meta = document.createElement('span');
          meta.className = 'meta';
          meta.textContent = entry.size + ' B';
          button.appendChild(meta);
        }
        button.addEventListener('click', function () {
          if (entry.dir) { loadDir(join(dir, entry.name)); }
          else { openFile(join(dir, entry.name)); }
        });
        listEl.appendChild(button);
      });
      setStatus(path ? '/' + path : '/');
    } catch (e) { setStatus(String(e), false); }
  }
  function renderCrumbs(path) {
    crumbsEl.textContent = '';
    var root = document.createElement('button');
    root.textContent = '/';
    root.addEventListener('click', function () { loadDir(''); });
    crumbsEl.appendChild(root);
    var parts = path ? path.split('/') : [];
    parts.forEach(function (part, i) {
      var button = document.createElement('button');
      button.textContent = part;
      button.addEventListener('click', function () { loadDir(parts.slice(0, i + 1).join('/')); });
      crumbsEl.appendChild(button);
    });
  }
  async function openFile(path) {
    if (isUIDFile(path)) { await openUID(path); return; }
    if (isNBTFile(path)) { await openNBT(path); return; }
    pathInput.value = path;
    setStatus('loading ' + path);
    var seq = ++openSeq;
    var res = await fetch(api + '/content?path=' + enc(path), { credentials: 'same-origin' });
    if (seq !== openSeq) { return; }
    if (!res.ok) {
      editor.value = '';
      renderLineNumbers();
      currentPath = path;
      currentSHA = '';
      latestRevision = 0;
      dirty = false;
      diffBtn.disabled = true;
      revisionsEl.textContent = '';
      var note = document.createElement('p');
      note.className = 'status';
      note.textContent = 'New file (nothing stored yet).';
      revisionsEl.appendChild(note);
      showEditor();
      showProperties(path);
      if (configKind(path)) {
        var emptyLint = await checkConfig(path, '');
        if (seq !== openSeq) { return; }
        applyLint(emptyLint);
      } else {
        clearLint();
        saveBtn.disabled = false;
      }
      setStatus('new file: ' + path);
      return;
    }
    var data = await res.json();
    if (seq !== openSeq) { return; }
    var content = data.content;
    var lint = null;
    if (configKind(path)) {
      setStatus('linting ' + path);
      lint = await checkConfig(path, content);
      if (seq !== openSeq) { return; }
      if (lint && lint.valid && lint.format === 'json' && lint.formatted) {
        content = lint.formatted;
      }
    }
    editor.value = content;
    renderLineNumbers();
    currentPath = path;
    currentSHA = data.sha256;
    latestRevision = 0;
    dirty = false;
    diffBtn.disabled = true;
    showEditor();
    showProperties(path);
    if (lint) { applyLint(lint); } else { clearLint(); }
    setStatus('loaded ' + path + ' (' + data.size + ' B)' +
      (content !== data.content ? ' · JSON formatted' : ''));
    loadRevisions(path);
  }
  function showEditor() {
    diffEl.style.display = 'none';
    nbtView.style.display = 'none';
    nbtDownload.style.display = 'none';
    editorWrap.style.display = 'flex';
  }
  async function save() {
    var path = (currentPath || pathInput.value).trim();
    if (!path) { setStatus('path is required', false); return; }
    if (isNBTFile(path)) { setStatus('NBT files are read-only', false); return; }
    var content = editor.value;
    var lint = null;
    if (configKind(path)) {
      setStatus('linting ' + path + ' before save');
      lint = await checkConfig(path, content);
      if (!lint || !lint.valid) {
        applyLint(lint);
        setStatus('fix lint errors before saving', false);
        return;
      }
      if (lint.format === 'json' && lint.formatted) { content = lint.formatted; }
      applyLint(lint);
    }
    setStatus('saving…');
    var body = { path: path, content: content };
    if (currentSHA) { body.expected_sha256 = currentSHA; }
    var res = await fetch(api + '/content', {
      method: 'PUT', credentials: 'same-origin',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body)
    });
    if (res.status === 409) { setStatus('conflict: the file changed on disk; reopen it', false); return; }
    if (!res.ok) { setStatus('save failed: HTTP ' + res.status + ' ' + (await res.text()).slice(0, 160), false); return; }
    var result = await res.json();
    if (content !== editor.value) { editor.value = content; renderLineNumbers(); }
    currentPath = path;
    currentSHA = result.sha256;
    latestRevision = result.revision;
    dirty = false;
    if (lint) { applyLint(lint); }
    setStatus('saved ' + path + ' (revision ' + result.revision + ')' +
      (lint && lint.format === 'json' ? ' · JSON formatted' : ''), true);
    loadRevisions(path);
  }
  async function loadRevisions(path) {
    var res = await fetch(api + '/revisions?path=' + enc(path) + '&limit=30', { credentials: 'same-origin' });
    if (!res.ok) { return; }
    var data = await res.json();
    revisionsEl.textContent = '';
    var items = data.items || [];
    if (items.length) { latestRevision = items[0].revision; }
    diffBtn.disabled = latestRevision < 2;
    if (!items.length) {
      var p = document.createElement('p');
      p.className = 'status';
      p.textContent = 'No revisions stored yet.';
      revisionsEl.appendChild(p);
      return;
    }
    items.forEach(function (rev) {
      var box = document.createElement('div');
      box.className = 'rev';
      var head = document.createElement('div');
      head.textContent = 'r' + rev.revision + ' · ' + rev.author;
      var when = document.createElement('div');
      when.className = 'when';
      when.textContent = new Date(rev.created_at).toLocaleString();
      var row = document.createElement('div');
      row.className = 'row';
      if (rev.revision > 1) {
        var diff = document.createElement('button');
        diff.textContent = 'Diff';
        diff.addEventListener('click', function () { showDiff(rev.revision - 1, rev.revision); });
        row.appendChild(diff);
      }
      var restore = document.createElement('button');
      restore.textContent = 'Restore';
      restore.addEventListener('click', function () { rollback(rev.revision); });
      row.appendChild(restore);
      box.appendChild(head);
      box.appendChild(when);
      box.appendChild(row);
      revisionsEl.appendChild(box);
    });
  }
  async function showDiff(from, to) {
    if (!currentPath) { return; }
    setStatus('diffing r' + from + ' → r' + to);
    var res = await fetch(api + '/diff?path=' + enc(currentPath) + '&from=' + from + '&to=' + to, { credentials: 'same-origin' });
    if (!res.ok) { setStatus('diff failed: HTTP ' + res.status, false); return; }
    var data = await res.json();
    editorWrap.style.display = 'none';
    nbtView.style.display = 'none';
    nbtDownload.style.display = 'none';
    diffEl.style.display = 'block';
    diffEl.textContent = data.diff || '(no changes)';
    setStatus('diff r' + from + ' → r' + to + ' of ' + currentPath, true);
  }
  async function rollback(revision) {
    if (!currentPath) { return; }
    if (!window.confirm('Restore ' + currentPath + ' to revision ' + revision + '?')) { return; }
    var res = await fetch(api + '/rollback', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ path: currentPath, revision: revision })
    });
    if (!res.ok) { setStatus('rollback failed: HTTP ' + res.status, false); return; }
    var data = await res.json();
    setStatus('restored r' + revision + ' as r' + data.revision, true);
    openFile(currentPath);
  }

  pathInput.addEventListener('keydown', function (event) {
    if (event.key === 'Enter') { event.preventDefault(); openFile(pathInput.value.trim()); }
  });
  document.getElementById('open').addEventListener('click', function () {
    var path = pathInput.value.trim();
    if (path) { openFile(path); }
  });
  saveBtn.addEventListener('click', save);
  propsFilter.addEventListener('input', function () { renderProperties(propsFilter.value); });
  diffBtn.addEventListener('click', function () {
    if (latestRevision >= 2) { showDiff(latestRevision - 1, latestRevision); }
  });
  editor.addEventListener('input', function () {
    dirty = true;
    renderLineNumbers();
    setStatus('unsaved changes');
    if (configKind(currentPath)) { scheduleLint(); }
    else { clearLint(); }
  });
  editor.addEventListener('scroll', function () {
    lineNumbers.scrollTop = editor.scrollTop;
  });
  editor.addEventListener('keydown', function (event) {
    if (event.key === 'Tab') {
      event.preventDefault();
      var start = editor.selectionStart;
      editor.setRangeText('  ', start, editor.selectionEnd, 'end');
      renderLineNumbers();
      setStatus('unsaved changes');
      if (configKind(currentPath)) { scheduleLint(); }
    }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      save();
    }
  });
  renderLineNumbers();
  showProperties('');
  loadDir('');
})();
</script>
</body></html>`))
