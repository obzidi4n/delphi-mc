package api

import (
	"html/template"
	"net/http"
)

type chatPage struct {
	BasePath string
	Email    string
	Enabled  bool
	Model    string
}

func (s *server) renderChat(w http.ResponseWriter, r *http.Request) {
	_, user, ok := s.pagePrincipal(w, r)
	if !ok {
		return
	}
	data := chatPage{BasePath: s.base, Email: user.Email, Enabled: s.opts.AI != nil}
	if s.opts.AI != nil {
		data.Model = s.opts.AI.Model
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = chatTemplate.Execute(w, data)
}

var chatTemplate = template.Must(template.New("chat").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AI · DelphiMC</title>
<style>
 :root{color-scheme:dark}
 *{box-sizing:border-box}
 body{margin:0;background:#0d1117;color:#c9d1d9;font:14px/1.55 ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;height:100vh;display:flex;flex-direction:column}
 header{display:flex;align-items:center;gap:.9rem;padding:.55rem .9rem;border-bottom:1px solid #21262d;background:#010409}
 header a{color:#79c0ff;text-decoration:none}
 h1{font-size:.95rem;margin:0;color:#e6edf3}
 .pill{font-size:.72rem;padding:.15rem .5rem;border-radius:999px;border:1px solid #30363d;color:#8b949e}
 .pill.on{color:#3fb950;border-color:#238636}
 main{flex:1;display:flex;min-height:0}
 aside{width:16rem;border-right:1px solid #21262d;overflow:auto;padding:.6rem}
 aside button{width:100%;margin-bottom:.6rem}
 .conv{display:block;width:100%;text-align:left;padding:.4rem .5rem;border-radius:.35rem;background:none;border:0;color:#c9d1d9;cursor:pointer;font:inherit;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
 .conv:hover,.conv.on{background:#161b22}
 .pane{flex:1;display:flex;flex-direction:column;min-width:0}
 #log{flex:1;overflow:auto;padding:1rem;max-width:52rem;width:100%;margin:0 auto}
 .msg{margin:0 0 1rem}
 .role{font-size:.72rem;text-transform:uppercase;letter-spacing:.05em;color:#8b949e;margin-bottom:.2rem}
 .bubble{white-space:pre-wrap;word-wrap:break-word}
 .user .bubble{color:#e6edf3}
 .tools{display:flex;flex-wrap:wrap;gap:.35rem;margin:.35rem 0}
 .chip{font-size:.72rem;border:1px solid #30363d;border-radius:999px;padding:.1rem .5rem;color:#8b949e}
 .chip.err{color:#f85149;border-color:#6e2c2c}
 .cites{margin:.4rem 0 0;padding:0;list-style:none;font-size:.78rem;color:#79c0ff}
 .cites li{margin:.15rem 0}
 .chatonly{font-size:.72rem;color:#d29922;border:1px solid #9e6a03;border-radius:.35rem;padding:.1rem .5rem;display:inline-block;margin-top:.35rem}
 form{display:flex;gap:.5rem;padding:.7rem 1rem;border-top:1px solid #21262d;background:#010409;max-width:52rem;width:100%;margin:0 auto}
 textarea{flex:1;resize:none;height:3.2rem;background:#0d1117;border:1px solid #30363d;border-radius:.4rem;color:inherit;padding:.5rem .6rem;font:inherit;outline:none}
 textarea:focus{border-color:#388bfd}
 button{padding:.45rem .9rem;border-radius:.4rem;border:1px solid #30363d;background:#21262d;color:inherit;cursor:pointer;font:inherit}
 button:disabled{opacity:.45;cursor:default}
 .banner{padding:.5rem 1rem;background:#3d2c00;color:#e3b341;font-size:.82rem;border-bottom:1px solid #9e6a03}
 .err{color:#f85149}
 .muted{color:#6e7681}
 .approval{border:1px solid #9e6a03;background:#1c1608;border-radius:.5rem;padding:.7rem .8rem;margin:.6rem 0}
 .approval h4{margin:0 0 .4rem;font-size:.82rem;color:#e3b341}
 .approval ul{margin:.2rem 0 .5rem;padding-left:1.1rem}
 .approval li{margin:.15rem 0}
 .approval .acts{color:#8b949e;font-size:.8rem}
</style></head><body data-base="{{.BasePath}}" data-enabled="{{if .Enabled}}1{{else}}0{{end}}">
<header>
  <a href="{{.BasePath}}">&larr; Fleet</a><h1>AI operator</h1>
  <span class="pill {{if .Enabled}}on{{end}}" id="status">{{if .Enabled}}{{.Model}}{{else}}not configured{{end}}</span>
  <span style="flex:1"></span>
  <span class="muted">{{.Email}}</span>
</header>
{{if not .Enabled}}<div class="banner">No model provider is configured. Set [ai] base_url and model in the panel config.</div>{{end}}
<main>
  <aside>
    <button id="new">New conversation</button>
    <div id="convs"></div>
  </aside>
  <section class="pane">
    <div id="log"><p class="muted">Ask about servers, lag, configuration, or the docs.</p></div>
    <form id="ask">
      <textarea id="input" placeholder="Why is Survival-1 lagging? (Enter to send, Shift+Enter for a newline)" {{if not .Enabled}}disabled{{end}}></textarea>
      <button type="submit" id="send" {{if not .Enabled}}disabled{{end}}>Send</button>
    </form>
  </section>
</main>
<script>
(function () {
  'use strict';
  var base = document.body.dataset.base || '/';
  var enabled = document.body.dataset.enabled === '1';
  var log = document.getElementById('log');
  var input = document.getElementById('input');
  var ask = document.getElementById('ask');
  var convsEl = document.getElementById('convs');
  var statusEl = document.getElementById('status');
  var currentId = '';
  var busy = false;
  var pending = null;
  var citesSeen = {};

  function clearLog(hint) {
    log.textContent = '';
    if (hint) { addMuted(hint); }
  }
  function addMuted(text) {
    var p = document.createElement('p');
    p.className = 'muted';
    p.textContent = text;
    log.appendChild(p);
  }
  function addMessage(role, text) {
    var wrap = document.createElement('div');
    wrap.className = 'msg ' + role;
    var label = document.createElement('div');
    label.className = 'role';
    label.textContent = role === 'user' ? 'You' : 'Assistant';
    var bubble = document.createElement('div');
    bubble.className = 'bubble';
    bubble.textContent = text || '';
    wrap.appendChild(label);
    wrap.appendChild(bubble);
    log.appendChild(wrap);
    log.scrollTop = log.scrollHeight;
    return wrap;
  }
  function addChip(text, isErr) {
    if (!pending) { return; }
    var chip = document.createElement('span');
    chip.className = 'chip' + (isErr ? ' err' : '');
    chip.textContent = text;
    pending.tools.appendChild(chip);
  }
  function addCitation(c) {
    if (!pending) { return; }
    var key = c.kind + '|' + c.ref;
    if (citesSeen[key]) { return; }
    citesSeen[key] = true;
    var li = document.createElement('li');
    li.textContent = (c.kind === 'doc' ? 'docs: ' : 'server: ') + c.ref +
      (c.title && c.title !== c.ref ? ' — ' + c.title : '');
    pending.cites.appendChild(li);
  }
  function beginPending() {
    var wrap = addMessage('assistant', '');
    var tools = document.createElement('div');
    tools.className = 'tools';
    var cites = document.createElement('ul');
    cites.className = 'cites';
    wrap.insertBefore(tools, wrap.firstChild.nextSibling);
    wrap.appendChild(cites);
    pending = { wrap: wrap, bubble: wrap.querySelector('.bubble'), tools: tools, cites: cites, chatOnly: false };
    citesSeen = {};
    return pending;
  }
  function setBusy(value) {
    busy = value;
    document.getElementById('send').disabled = value || !enabled;
    input.disabled = value || !enabled;
  }
  function handleEvent(ev) {
    if (ev.type === 'delta') {
      if (!pending) { beginPending(); }
      pending.bubble.textContent += ev.text;
      log.scrollTop = log.scrollHeight;
    } else if (ev.type === 'tool') {
      if (!pending) { beginPending(); }
      addChip(ev.name + (ev.status === 'error' ? ' failed' : ''), ev.status === 'error');
    } else if (ev.type === 'citation') {
      addCitation(ev.citation);
    } else if (ev.type === 'done') {
      if (ev.conversation_id) { currentId = ev.conversation_id; }
      if (ev.chat_only && pending) {
        var badge = document.createElement('span');
        badge.className = 'chatonly';
        badge.textContent = 'chat-only answer (no tools used)';
        pending.wrap.appendChild(badge);
      }
      if (ev.citations) { ev.citations.forEach(addCitation); }
      if (ev.approval) { renderApproval(ev.approval); }
      pending = null;
    } else if (ev.type === 'error') {
      addChip('error', true);
      var p = document.createElement('div');
      p.className = 'err';
      p.textContent = ev.message;
      log.appendChild(p);
      pending = null;
    }
    log.scrollTop = log.scrollHeight;
  }
  async function sendMessage() {
    var text = input.value.trim();
    if (!text || busy || !enabled) { return; }
    input.value = '';
    if (!currentId) { clearLog(''); }
    addMessage('user', text);
    beginPending();
    setBusy(true);
    try {
      var res = await fetch(base + 'api/v1/ai/chat', {
        method: 'POST', credentials: 'same-origin',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(currentId ? { conversation_id: currentId, message: text } : { message: text })
      });
      if (!res.ok) {
        handleEvent({ type: 'error', message: 'HTTP ' + res.status + ': ' + (await res.text()).slice(0, 200) });
        return;
      }
      await readSSE(res);
    } catch (e) {
      handleEvent({ type: 'error', message: String(e) });
    } finally {
      pending = null;
      setBusy(false);
      loadConversations();
      input.focus();
    }
  }
  async function readSSE(res) {
    var reader = res.body.getReader();
    var decoder = new TextDecoder();
    var buffer = '';
    for (;;) {
      var step = await reader.read();
      if (step.done) { break; }
      buffer += decoder.decode(step.value, { stream: true });
      var idx;
      while ((idx = buffer.indexOf('\n\n')) >= 0) {
        var chunk = buffer.slice(0, idx);
        buffer = buffer.slice(idx + 2);
        chunk.split('\n').forEach(function (line) {
          if (line.indexOf('data: ') !== 0) { return; }
          try { handleEvent(JSON.parse(line.slice(6))); } catch (e) {}
        });
      }
    }
  }
  function renderApproval(approval) {
    var card = document.createElement('div');
    card.className = 'approval';
    var title = document.createElement('h4');
    title.textContent = 'Approval required';
    card.appendChild(title);
    var list = document.createElement('ul');
    (approval.calls || []).forEach(function (call) {
      var li = document.createElement('li');
      var strong = document.createElement('strong');
      strong.textContent = call.tool;
      li.appendChild(strong);
      li.appendChild(document.createTextNode(' — ' + call.summary));
      if (call.actions && call.actions.length) {
        var sub = document.createElement('div');
        sub.className = 'acts';
        sub.textContent = call.actions.map(function (a) { return a.summary; }).join(' · ');
        li.appendChild(sub);
      }
      list.appendChild(li);
    });
    card.appendChild(list);
    var approve = document.createElement('button');
    approve.textContent = 'Approve';
    var reject = document.createElement('button');
    reject.textContent = 'Reject';
    reject.style.marginLeft = '.4rem';
    function decide(ok) {
      approve.disabled = true;
      reject.disabled = true;
      var status = document.createElement('div');
      status.className = 'acts';
      status.textContent = ok ? 'Approved' : 'Rejected';
      card.appendChild(status);
      decideApproval(approval.id, ok);
    }
    approve.addEventListener('click', function () { decide(true); });
    reject.addEventListener('click', function () { decide(false); });
    card.appendChild(approve);
    card.appendChild(reject);
    log.appendChild(card);
    log.scrollTop = log.scrollHeight;
  }
  async function decideApproval(id, ok) {
    if (busy) { return; }
    setBusy(true);
    beginPending();
    try {
      var url = base + 'api/v1/ai/approvals/' + encodeURIComponent(id) + (ok ? '/approve' : '/reject');
      var res = await fetch(url, { method: 'POST', credentials: 'same-origin' });
      if (!res.ok) {
        handleEvent({ type: 'error', message: 'HTTP ' + res.status + ': ' + (await res.text()).slice(0, 200) });
        return;
      }
      await readSSE(res);
    } catch (e) {
      handleEvent({ type: 'error', message: String(e) });
    } finally {
      pending = null;
      setBusy(false);
      loadConversations();
    }
  }
  async function loadConversation(id) {
    var res = await fetch(base + 'api/v1/ai/conversations/' + encodeURIComponent(id), { credentials: 'same-origin' });
    if (!res.ok) { return; }
    var data = await res.json();
    currentId = id;
    clearLog('');
    (data.messages || []).forEach(function (m) {
      if (m.role === 'user') { addMessage('user', m.content); }
      else if (m.role === 'assistant' && m.content) { addMessage('assistant', m.content); }
    });
    (data.approvals || []).forEach(renderApproval);
    Array.prototype.forEach.call(convsEl.children, function (el) { el.classList.toggle('on', el.dataset.id === id); });
    pending = null;
  }
  async function loadConversations() {
    var res = await fetch(base + 'api/v1/ai/conversations', { credentials: 'same-origin' });
    if (!res.ok) { return; }
    var data = await res.json();
    convsEl.textContent = '';
    (data.items || []).forEach(function (c) {
      var b = document.createElement('button');
      b.className = 'conv';
      b.dataset.id = c.id;
      b.textContent = c.title || 'Untitled';
      b.addEventListener('click', function () { loadConversation(c.id); });
      convsEl.appendChild(b);
    });
  }
  document.getElementById('new').addEventListener('click', function () {
    currentId = '';
    pending = null;
    clearLog('Ask about servers, lag, configuration, or the docs.');
    Array.prototype.forEach.call(convsEl.children, function (el) { el.classList.remove('on'); });
    input.focus();
  });
  ask.addEventListener('submit', function (e) { e.preventDefault(); sendMessage(); });
  input.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); }
  });
  loadConversations();
})();
</script>
</body></html>`))
