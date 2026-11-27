package api

import (
	"html/template"
	"net/http"
	"strings"
)

type consolePage struct {
	BasePath string
	Email    string
	ID       string
	Name     string
	Base     string
	State    string
}

func (s *server) renderConsole(w http.ResponseWriter, r *http.Request) {
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
	data := consolePage{
		BasePath: s.base, Email: user.Email, ID: srv.ID, Name: srv.Name,
		Base: srv.Base, State: srv.SummaryState,
	}
	if data.State == "" {
		data.State = srv.ObservedState
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = consoleTemplate.Execute(w, data)
}

var consoleTemplate = template.Must(template.New("console").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Name}} · DelphiMC</title>
<style>
 :root{color-scheme:dark}
 *{box-sizing:border-box}
 body{margin:0;background:#0d1117;color:#c9d1d9;font:13px/1.45 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;height:100vh;display:flex;flex-direction:column}
 header{display:flex;align-items:center;gap:.9rem;padding:.55rem .9rem;border-bottom:1px solid #21262d;background:#010409;flex-wrap:wrap}
 header a{color:#79c0ff;text-decoration:none}
 h1{font-size:.95rem;margin:0;font-weight:600;color:#e6edf3}
 .pill{font-size:.72rem;padding:.15rem .5rem;border-radius:999px;border:1px solid #30363d;text-transform:uppercase;letter-spacing:.04em}
 .pill.live{color:#3fb950;border-color:#238636}
 .pill.off{color:#d29922;border-color:#9e6a03}
 .wrap{flex:1;display:flex;flex-direction:column;min-height:0}
 .toolbar{display:flex;align-items:center;gap:.5rem;padding:.45rem .9rem;border-bottom:1px solid #21262d;background:#0d1117;flex-wrap:wrap}
 input[type=search]{flex:0 0 16rem;background:#010409;border:1px solid #30363d;border-radius:.35rem;color:inherit;padding:.3rem .5rem;font:inherit;outline:none}
 input[type=search]:focus{border-color:#388bfd}
 button{background:#21262d;border:1px solid #30363d;border-radius:.35rem;color:#c9d1d9;padding:.28rem .6rem;font:inherit;cursor:pointer}
 button:hover{background:#30363d}
 button.on{background:#1f6feb;border-color:#1f6feb;color:#fff}
 button:disabled{opacity:.4;cursor:default}
 label.check{display:inline-flex;align-items:center;gap:.3rem;color:#8b949e;cursor:pointer;user-select:none}
 .spacer{flex:1}
 .metrics{color:#8b949e;white-space:nowrap}
 .metrics b{color:#c9d1d9;font-weight:600}
 .mainrow{flex:1;display:flex;min-height:0}
 .termcol{flex:1;display:flex;flex-direction:column;min-width:0}
 #term{flex:1;overflow:auto;padding:.5rem .9rem 1.2rem;white-space:pre-wrap;word-break:break-word;min-height:0}
 .commandbar{display:flex;gap:.5rem;padding:.45rem .9rem;border-top:1px solid #21262d;background:#010409}
 .commandbar input{flex:1;min-width:0;background:#0d1117;border:1px solid #30363d;border-radius:.35rem;color:inherit;padding:.35rem .55rem;font:inherit;outline:none}
 .commandbar input:focus{border-color:#388bfd}
 .commandbar input:disabled{opacity:.5}
 .commandbar button{background:#21262d;border:1px solid #30363d;border-radius:.35rem;color:#c9d1d9;padding:.3rem .7rem;font:inherit;cursor:pointer}
 .commandbar button:hover{background:#30363d}
 .commandbar button:disabled{opacity:.4;cursor:default}
 aside{width:21rem;flex:0 0 auto;border-left:1px solid #21262d;background:#0d1117;overflow:auto;padding:.6rem .8rem}
 aside h2{font-size:.85rem;margin:.2rem 0 .5rem;display:flex;align-items:center;gap:.4rem}
 aside h3{font-size:.78rem;text-transform:uppercase;letter-spacing:.05em;color:#8b949e;margin:.9rem 0 .35rem}
 .statline{color:#8b949e;font-size:.8rem;margin-bottom:.4rem}
 .statline b{color:#c9d1d9}
 .players{list-style:none;margin:0;padding:0}
 .players li{display:flex;align-items:center;gap:.35rem;padding:.18rem 0;border-bottom:1px solid #21262d}
 .players li span{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
 .players button{padding:.1rem .4rem;font-size:.72rem}
 .chat{max-height:14rem;overflow:auto;font-size:.8rem;line-height:1.5;color:#c9d1d9}
 .chat .sys{color:#8b949e}
 .chat .err{color:#f85149}
 .chat .line{min-height:0}
 #cmddetails{margin-top:.9rem;font-size:.8rem;color:#8b949e}
 #cmddetails summary{cursor:pointer}
 .cmdlist{max-height:12rem;overflow:auto;margin-top:.4rem;color:#c9d1d9;word-break:break-word}
 .hint{color:#6e7681;font-size:.75rem;margin-top:.6rem}
 @media (max-width: 900px){.mainrow{flex-direction:column} aside{width:auto;border-left:0;border-top:1px solid #21262d;max-height:45%} #term{min-height:40vh}}
 .line{min-height:1.35em}
 .line.sys{color:#d29922}
 .line.hit{background:#3d2c00;outline:1px solid #9e6a03}
 .dim{color:#6e7681}
 .err{color:#f85149}
 .warn{color:#d29922}
 .ok{color:#3fb950}
 .b{font-weight:600}
 .count{color:#8b949e;font-size:.8rem;min-width:4rem}
 footer{color:#6e7681;font-size:.75rem;padding:.25rem .9rem;border-top:1px solid #21262d;background:#010409}
</style></head><body data-id="{{.ID}}" data-base="{{.BasePath}}" data-name="{{.Name}}" data-basekind="{{.Base}}">
<header>
  <a href="{{.BasePath}}">&larr; Fleet</a>
  <a href="{{.BasePath}}chat">AI</a>
  <a href="{{.BasePath}}servers/{{.ID}}/files">Files</a>
  <a href="{{.BasePath}}servers/{{.ID}}/plugins">Plugins</a>
  <h1>{{.Name}}</h1>
  <span class="pill off" id="status">connecting</span>
  <span class="pill" id="state">{{.State}}</span>
  <span class="pill">{{.Base}}</span>
  <span class="metrics" id="metrics">cpu – · mem –</span>
  <span class="spacer"></span>
  <button data-power="start">Start</button>
  <button data-power="restart">Restart</button>
  <button data-power="stop">Stop</button>
  <button data-power="kill">Kill</button>
</header>
<div class="wrap">
 <div class="mainrow">
  <div class="termcol">
  <div class="toolbar">
    <input id="search" type="search" placeholder="Search console (Ctrl+F)">
    <span class="count" id="count"></span>
    <button id="pause" title="Pause rendering (keeps buffering)">Pause</button>
    <label class="check"><input type="checkbox" id="follow" checked> Follow</label>
    <span class="spacer"></span>
    <button id="copy">Copy</button>
    <button id="clear">Clear</button>
  </div>
  <div id="term" tabindex="0"></div>
  <form class="commandbar" id="cmdform">
    <input id="cmdinput" list="commandOptions" autocomplete="off" disabled placeholder="Server command (no leading / needed)">
    <button id="cmdsend" type="submit" disabled>Send</button>
  </form>
  <datalist id="commandOptions"></datalist>
  </div>
  <aside>
    <h2>Bridge <span class="pill off" id="bridge-state">waiting</span></h2>
    <div class="statline">TPS <b id="tps">–</b> · players <b id="playercount">0</b></div>
    <ul class="players" id="players"></ul>
    <h3>Chat</h3>
    <div class="chat" id="chat"><div class="line sys">No chat yet.</div></div>
    <details id="cmddetails">
      <summary>Command tree (<span id="cmdcount">0</span>)</summary>
      <div class="cmdlist" id="commands"></div>
    </details>
    <p class="hint" id="bridgenote">Waiting for the bridge plugin…</p>
  </aside>
 </div>
</div>
<footer>Line ring 5000 · gaps are marked · Ctrl+F search · Esc clears · Alt+P pause</footer>
<script>
(function () {
  'use strict';
  var body = document.body;
  var id = body.dataset.id;
  var base = body.dataset.base || '/';
  var term = document.getElementById('term');
  var statusEl = document.getElementById('status');
  var stateEl = document.getElementById('state');
  var tpsEl = document.getElementById('tps');
  var playerCountEl = document.getElementById('playercount');
  var playersEl = document.getElementById('players');
  var chatEl = document.getElementById('chat');
  var cmdCountEl = document.getElementById('cmdcount');
  var commandsEl = document.getElementById('commands');
  var bridgeStateEl = document.getElementById('bridge-state');
  var bridgeNote = document.getElementById('bridgenote');
  var metricsEl = document.getElementById('metrics');
  var searchEl = document.getElementById('search');
  var cmdForm = document.getElementById('cmdform');
  var cmdInput = document.getElementById('cmdinput');
  var cmdSend = document.getElementById('cmdsend');
  var commandOptions = document.getElementById('commandOptions');
  var history = [];
  var historyIndex = 0;
  var countEl = document.getElementById('count');
  var pauseBtn = document.getElementById('pause');
  var followEl = document.getElementById('follow');
  var maxLines = 5000;
  var seqLast = 0;
  var paused = false;
  var queued = [];
  var retry = 600;
  var ws = null;
  var state = '';

  function setStatus(text, live) {
    statusEl.textContent = text;
    statusEl.className = 'pill ' + (live ? 'live' : 'off');
  }
  function connect() {
    setStatus('connecting', false);
    var proto = location.protocol === 'https:' ? 'wss://' : 'ws://';
    ws = new WebSocket(proto + location.host + base + 'ws/servers/' + encodeURIComponent(id));
    ws.binaryType = 'arraybuffer';
    ws.onopen = function () { retry = 600; setStatus('live', true); };
    ws.onmessage = function (ev) {
      if (typeof ev.data === 'string') { onText(ev.data); return; }
      onBinary(new Uint8Array(ev.data));
    };
    ws.onclose = function () {
      setStatus('offline · retrying', false);
      window.setTimeout(connect, retry);
      retry = Math.min(retry * 2, 5000);
    };
    ws.onerror = function () { try { ws.close(); } catch (e) {} };
  }
  function onText(text) {
    var msg;
    try { msg = JSON.parse(text); } catch (e) { return; }
    if (msg.type === 'stats') {
      metricsEl.innerHTML = 'cpu <b>' + (msg.cpu_percent || 0).toFixed(1) + '%</b> · mem <b>' +
        formatBytes(msg.memory_bytes || 0) + '</b>';
      return;
    }
    if (msg.type === 'bridge') { applyBridge(msg); return; }
    if (msg.type === 'bridge.event') { onBridgeEvent(msg.event); }
  }
  function applyBridge(m) {
    bridgeStateEl.textContent = 'live';
    bridgeStateEl.className = 'pill live';
    bridgeNote.hidden = true;
    tpsEl.textContent = (m.tps || 0).toFixed(2);
    var players = m.players || [];
    playerCountEl.textContent = players.length;
    playersEl.textContent = '';
    players.forEach(function (p) {
      var li = document.createElement('li');
      var name = document.createElement('span');
      name.textContent = p.name;
      li.appendChild(name);
      ['kick', 'ban'].forEach(function (action) {
        var b = document.createElement('button');
        b.textContent = action === 'kick' ? 'Kick' : 'Ban';
        b.dataset.action = action;
        b.dataset.player = p.name;
        li.appendChild(b);
      });
      playersEl.appendChild(li);
    });
    var commands = m.commands || [];
    cmdCountEl.textContent = commands.length;
    commandsEl.textContent = commands.join('  ');
    commandOptions.textContent = '';
    commands.forEach(function (command) {
      var option = document.createElement('option');
      option.value = command;
      commandOptions.appendChild(option);
    });
    cmdInput.disabled = false;
    cmdSend.disabled = false;
  }
  function chatLine(text, cls) {
    var el = document.createElement('div');
    el.className = 'line' + (cls ? ' ' + cls : '');
    el.textContent = text;
    chatEl.appendChild(el);
    while (chatEl.childElementCount > 200) { chatEl.removeChild(chatEl.firstElementChild); }
    chatEl.scrollTop = chatEl.scrollHeight;
  }
  function onBridgeEvent(event) {
    if (!event) { return; }
    if (event.kind === 'chat') {
      chatLine(event.player + ': ' + event.message, '');
    } else if (event.kind === 'join') {
      chatLine('+ ' + event.player, 'sys');
    } else if (event.kind === 'quit') {
      chatLine('- ' + event.player, 'sys');
    } else if (event.kind === 'moderation') {
      chatLine('[' + event.action + '] ' + event.player + ': ' + (event.ok ? 'ok' : (event.error || 'failed')),
        event.ok ? 'sys' : 'err');
    } else if (event.kind === 'command') {
      chatLine('/' + (event.message || '') + ' · ' + (event.ok ? 'ok' : (event.error || 'failed')),
        event.ok ? 'sys' : 'err');
    } else if (event.kind === 'ready') {
      bridgeNote.hidden = true;
    }
  }
  function loadBridge() {
    fetch(base + 'api/v1/servers/' + encodeURIComponent(id) + '/bridge', { credentials: 'same-origin' })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (m) { if (m) { applyBridge(m); } })
      .catch(function () {});
  }
  function moderate(action, player) {
    var reason = window.prompt((action === 'kick' ? 'Kick ' : 'Ban ') + player + ' — reason?', '');
    if (reason === null) { return; }
    fetch(base + 'api/v1/servers/' + encodeURIComponent(id) + '/moderation', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ action: action, player: player, reason: reason })
    }).then(function (r) {
      if (!r.ok) { chatLine('moderation refused (' + r.status + ')', 'err'); }
    }).catch(function () { chatLine('moderation failed', 'err'); });
  }
  playersEl.addEventListener('click', function (ev) {
    var button = ev.target.closest('button[data-action]');
    if (!button) { return; }
    moderate(button.dataset.action, button.dataset.player);
  });
  function sendCommand() {
    var text = cmdInput.value.trim();
    if (!text) { return; }
    var command = text.replace(/^\/+/, '');
    if (!command) { return; }
    cmdInput.value = '';
    history.push(text);
    historyIndex = history.length;
    appendLine('> /' + command, 'sys');
    fetch(base + 'api/v1/servers/' + encodeURIComponent(id) + '/command', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ command: command })
    }).then(function (r) {
      if (r.ok) { return; }
      return r.text().then(function (text) {
        var detail = text;
        try { detail = JSON.parse(text).detail || text; } catch (e) {}
        chatLine('command refused: ' + detail, 'err');
      });
    }).catch(function () { chatLine('command failed', 'err'); });
  }
  cmdForm.addEventListener('submit', function (ev) { ev.preventDefault(); sendCommand(); });
  cmdInput.addEventListener('keydown', function (ev) {
    if (ev.key === 'ArrowUp') {
      ev.preventDefault();
      if (historyIndex > 0) { historyIndex--; cmdInput.value = history[historyIndex] || ''; }
    } else if (ev.key === 'ArrowDown') {
      ev.preventDefault();
      if (historyIndex < history.length - 1) { historyIndex++; cmdInput.value = history[historyIndex] || ''; }
      else { historyIndex = history.length; cmdInput.value = ''; }
    } else if (ev.key === 'Enter') {
      ev.preventDefault();
      sendCommand();
    }
  });
  function onBinary(bytes) {
    if (bytes.length < 15 || bytes[0] !== 1) { return; }
    var view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    var idLen = view.getUint16(1);
    var off = 3 + idLen;
    var seq = Number(view.getBigUint64(off)); off += 8;
    var len = view.getUint32(off); off += 4;
    var text = new TextDecoder().decode(bytes.subarray(off, off + len));
    var lines = text.split('\n');
    if (seqLast && seq > seqLast + 1) {
      appendLine('[delphi] ' + (seq - seqLast - 1) + ' line(s) missing', 'sys');
    }
    lines.forEach(function (line, i) { appendLine(line, null, seq + i); });
    seqLast = Math.max(seqLast, seq + lines.length - 1);
    if (followEl.checked) { term.scrollTop = term.scrollHeight; }
  }
  function queueOrRender(line, cls, seq) {
    if (paused) {
      if (queued.length < 2000) { queued.push([line, cls, seq]); }
      return;
    }
    render(line, cls);
  }
  function appendLine(line, cls, seq) { queueOrRender(line, cls, seq); }
  function render(line, cls) {
    var el = document.createElement('div');
    el.className = 'line' + (cls ? ' ' + cls : '');
    renderANSI(line, el);
    if (searchEl.value) { markHit(el, searchEl.value); }
    term.appendChild(el);
    while (term.childElementCount > maxLines) { term.removeChild(term.firstElementChild); }
  }
  var colors = {30:'#6e7681',31:'#f85149',32:'#3fb950',33:'#d29922',34:'#58a6ff',35:'#bc8cff',36:'#39c5cf',37:'#c9d1d9',
                90:'#6e7681',91:'#ff7b72',92:'#56d364',93:'#e3b341',94:'#79c0ff',95:'#d2a8ff',96:'#56d4dd',97:'#f0f6fc'};
  function renderANSI(text, line) {
    var re = /\u001b\[([0-9;]*)m/g;
    var last = 0, match, st = { fg: null, bold: false };
    while ((match = re.exec(text)) !== null) {
      addSegment(line, text.slice(last, match.index), st);
      applySGR(match[1], st);
      last = re.lastIndex;
    }
    addSegment(line, text.slice(last), st);
  }
  function applySGR(codes, st) {
    var parts = codes === '' ? ['0'] : codes.split(';');
    for (var i = 0; i < parts.length; i++) {
      var n = parseInt(parts[i], 10);
      if (n === 0) { st.fg = null; st.bold = false; }
      else if (n === 1) { st.bold = true; }
      else if (n === 22) { st.bold = false; }
      else if (colors[n]) { st.fg = colors[n]; }
    }
  }
  function addSegment(line, text, st) {
    if (!text) { return; }
    var span = document.createElement('span');
    if (st.fg) { span.style.color = st.fg; }
    if (st.bold) { span.style.fontWeight = '600'; }
    span.textContent = text;
    line.appendChild(span);
  }
  function markHit(el, needle) {
    var hay = el.textContent.toLowerCase();
    if (hay.indexOf(needle.toLowerCase()) >= 0) { el.classList.add('hit'); }
  }
  function applySearch() {
    var needle = searchEl.value.trim();
    var lines = term.querySelectorAll('.line');
    var hits = 0;
    for (var i = 0; i < lines.length; i++) {
      lines[i].classList.remove('hit');
      if (!needle) { continue; }
      var hay = lines[i].textContent.toLowerCase();
      if (hay.indexOf(needle.toLowerCase()) >= 0) { lines[i].classList.add('hit'); hits++; }
    }
    countEl.textContent = needle ? hits + ' match' + (hits === 1 ? '' : 'es') : '';
    if (needle && hits > 0) {
      var first = term.querySelector('.line.hit');
      if (first) { first.scrollIntoView({ block: 'center' }); }
    }
  }
  function togglePause() {
    paused = !paused;
    pauseBtn.classList.toggle('on', paused);
    pauseBtn.textContent = paused ? 'Resume' : 'Pause';
    if (!paused && queued.length) {
      var frag = document.createDocumentFragment();
      queued.forEach(function (item) {
        var el = document.createElement('div');
        el.className = 'line' + (item[1] ? ' ' + item[1] : '');
        renderANSI(item[0], el);
        frag.appendChild(el);
      });
      term.appendChild(frag);
      queued = [];
      while (term.childElementCount > maxLines) { term.removeChild(term.firstElementChild); }
      if (followEl.checked) { term.scrollTop = term.scrollHeight; }
      applySearch();
    }
  }
  function formatBytes(n) {
    if (n >= 1 << 30) { return (n / (1 << 30)).toFixed(1) + ' GiB'; }
    if (n >= 1 << 20) { return (n / (1 << 20)).toFixed(0) + ' MiB'; }
    if (n >= 1 << 10) { return (n / (1 << 10)).toFixed(0) + ' KiB'; }
    return n + ' B';
  }
  function refreshState() {
    fetch(base + 'api/v1/servers/' + encodeURIComponent(id), { credentials: 'same-origin' })
      .then(function (r) { return r.ok ? r.json() : null; })
      .then(function (srv) {
        if (!srv) { return; }
        state = srv.summary_state || srv.observed_state || 'unknown';
        stateEl.textContent = state;
        stateEl.className = 'pill ' + (state === 'running' ? 'live' : 'off');
      })
      .catch(function () {});
  }
  function power(action) {
    stateEl.textContent = action + '…';
    fetch(base + 'api/v1/servers/' + encodeURIComponent(id) + '/power', {
      method: 'POST', credentials: 'same-origin',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ action: action })
    }).then(function (r) { return r.ok ? r.json() : null; })
      .then(function (srv) { if (srv) { stateEl.textContent = srv.desired_state; } })
      .catch(function () {});
    window.setTimeout(refreshState, 800);
  }

  document.querySelectorAll('[data-power]').forEach(function (btn) {
    btn.addEventListener('click', function () { power(btn.dataset.power); });
  });
  searchEl.addEventListener('input', applySearch);
  document.getElementById('copy').addEventListener('click', function () {
    var text = Array.prototype.map.call(term.querySelectorAll('.line'), function (el) { return el.textContent; }).join('\n');
    if (!navigator.clipboard) { return; }
    navigator.clipboard.writeText(text).then(function () {
      var b = document.getElementById('copy');
      b.textContent = 'Copied';
      window.setTimeout(function () { b.textContent = 'Copy'; }, 900);
    });
  });
  document.getElementById('clear').addEventListener('click', function () {
    term.textContent = ''; seqLast = 0; queued = []; countEl.textContent = '';
  });
  pauseBtn.addEventListener('click', togglePause);
  document.addEventListener('keydown', function (ev) {
    if (ev.ctrlKey && ev.key.toLowerCase() === 'f') { ev.preventDefault(); searchEl.focus(); return; }
    if (ev.key === 'Escape') { searchEl.value = ''; applySearch(); term.focus(); return; }
    if (ev.altKey && ev.key.toLowerCase() === 'p') { ev.preventDefault(); togglePause(); }
  });

  refreshState();
  loadBridge();
  window.setInterval(refreshState, 5000);
  connect();
})();
</script>
</body></html>`))
