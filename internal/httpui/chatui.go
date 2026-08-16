// The team-chat shell (M17 surface): a Teams/ZCode-style layout — channel
// rail with team presence, message stream, composer — served by the daemon
// over localhost. It is a view over the store: messages are attributed blocks
// in channel docs, synced by the same git transport as everything else.
package httpui

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"leetoffice/internal/chat"
	leetNet "leetoffice/internal/net"
)

// --- JSON API ----------------------------------------------------------------

type peerCache struct {
	mu      sync.Mutex
	at      time.Time
	entries []string
}

var peers peerCache

// onlineNodes lists node ids seen on the LAN recently (mDNS), cached so the
// poll endpoint doesn't hammer multicast.
func onlineNodes() []string {
	peers.mu.Lock()
	defer peers.mu.Unlock()
	if time.Since(peers.at) < 5*time.Second {
		return peers.entries
	}
	found, err := leetNet.DiscoverPeers(700 * time.Millisecond)
	if err != nil {
		found = nil
	}
	var ids []string
	for _, p := range found {
		ids = append(ids, p.NodeID)
	}
	peers.entries, peers.at = ids, time.Now()
	return ids
}

func (u *UI) handleAPIState(w http.ResponseWriter, r *http.Request) {
	channelName := chat.Normalize(r.URL.Query().Get("channel"))
	chans, err := chat.Channels(u.Store)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type chanRow struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
		Last string `json:"last_activity"`
		N    int    `json:"messages"`
	}
	var chanRows []chanRow
	var current *struct {
		Slug     string         `json:"slug"`
		Messages []chat.Message `json:"messages"`
	}
	for _, d := range chans {
		msgs := chat.Messages(d)
		chanRows = append(chanRows, chanRow{d.Slug, "#" + d.Slug, chat.LastActivity(d), len(msgs)})
		if d.Slug == channelName {
			current = &struct {
				Slug     string         `json:"slug"`
				Messages []chat.Message `json:"messages"`
			}{d.Slug, msgs}
		}
	}
	if current == nil && channelName != "" {
		current = &struct {
			Slug     string         `json:"slug"`
			Messages []chat.Message `json:"messages"`
		}{channelName, nil}
	}

	people := map[string]string{} // actor -> "online" | "recent"
	for _, node := range onlineNodes() {
		people["node:"+node] = "online"
	}
	for _, a := range chat.RecentActors(u.Repo, 24*time.Hour) {
		if _, ok := people[a]; !ok {
			people[a] = "recent"
		}
	}
	if _, ok := people[u.Config.Actor]; !ok {
		people[u.Config.Actor] = "online" // you
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"me":       u.Config.Actor,
		"channels": chanRows,
		"current":  current,
		"people":   people,
	})
}

func (u *UI) handleAPISend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req struct{ Channel, Text string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	msg, _, err := chat.Send(u.Store, u.Repo, u.Config.Actor, req.Channel, req.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msg)
}

// --- the shell ----------------------------------------------------------------

const chatCSS = `
:root{
 --red:#d92a2a; --bg:#fff; --surface:#f8f5eb; --text:#111; --muted:#5b5b57; --faint:#8a8a85;
 --line:rgba(17,17,17,.12); --line2:rgba(17,17,17,.24); --wash:rgba(17,17,17,.05);
 --term:#141414; --termline:#2a2a2a; --termtext:#f8f5eb;
 --sans:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
 --mono:'JetBrains Mono',ui-monospace,'SF Mono',SFMono-Regular,Menlo,Consolas,monospace;
}
*{box-sizing:border-box;margin:0;padding:0}
html,body{height:100%}
body{font-family:var(--sans);font-size:14px;color:var(--text);display:flex;flex-direction:column;overflow:hidden;background:var(--bg)}
header.topnav{position:sticky;top:0;z-index:100;background:rgba(255,255,255,.86);
 -webkit-backdrop-filter:blur(12px);backdrop-filter:blur(12px);border-bottom:1px solid var(--line);flex-shrink:0}
.navwrap{max-width:none;margin:0 auto;padding:0 20px;height:52px;display:flex;align-items:center;gap:24px}
.navbrand{display:inline-flex;gap:9px;align-items:center;font-family:var(--mono);font-weight:700;font-size:13px;letter-spacing:.18em;color:#111}
nav.bar{display:flex;align-items:center;gap:22px;flex:1}
nav.bar a{font-family:var(--mono);font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--muted);padding:4px 2px;border-bottom:2px solid transparent;text-decoration:none}
nav.bar a:hover{color:var(--text)}
nav.bar a.on{color:var(--text);border-bottom-color:var(--red)}
.navside{margin-left:auto;font-family:var(--mono);font-size:11px;letter-spacing:.1em;color:var(--faint);white-space:nowrap}
.app{flex:1;display:flex;min-height:0}
.rail{width:270px;background:var(--term);color:var(--termtext);display:flex;flex-direction:column;flex-shrink:0;border-right:1px solid var(--termline)}
.rail .brand{padding:18px 20px;font-family:var(--mono);font-weight:700;letter-spacing:.18em;font-size:13px;border-bottom:1px solid var(--termline);display:flex;align-items:center;gap:10px}
.pulse{width:6px;height:6px;border-radius:50%;background:var(--red);animation:xb-pulse 2s infinite}
@keyframes xb-pulse{0%,100%{opacity:1}50%{opacity:.35}}
.rail .brand small{display:block;font-weight:400;font-size:10px;color:rgba(248,245,235,.5);margin-top:4px;letter-spacing:.12em}
.rail h4{font-family:var(--mono);font-size:10px;text-transform:uppercase;letter-spacing:.2em;color:rgba(248,245,235,.5);padding:16px 20px 6px}
.rail a.ch{display:flex;justify-content:space-between;padding:7px 20px;color:var(--termtext);text-decoration:none;font-family:var(--mono);font-size:12.5px;letter-spacing:.04em}
.rail a.ch:hover{background:rgba(248,245,235,.07)}
.rail a.ch.on{background:var(--red);color:#fff}
.rail .new{margin:8px 20px;font-family:var(--mono);font-size:11px;letter-spacing:.08em;background:none;border:1px dashed rgba(248,245,235,.25);color:rgba(248,245,235,.6);padding:7px 10px;cursor:pointer;width:calc(100% - 40px)}
.rail .new:hover{color:var(--termtext);border-color:rgba(248,245,235,.5)}
.rail .people{flex:1;overflow-y:auto;padding-bottom:16px}
.rail .person{display:flex;align-items:center;gap:9px;padding:5px 20px;font-size:12.5px;color:rgba(248,245,235,.85)}
.rail .person .you{color:rgba(248,245,235,.4);font-size:11px}
.dot{width:6px;height:6px;border-radius:50%;flex-shrink:0}
.dot.online{background:var(--red);animation:xb-pulse 2s infinite}
.dot.recent{background:rgba(248,245,235,.35)}
.rail .foot{padding:14px 20px;border-top:1px solid var(--termline);display:flex;gap:14px}
.rail .foot a{font-family:var(--mono);font-size:10px;letter-spacing:.14em;text-transform:uppercase;color:rgba(248,245,235,.55);text-decoration:none}
.rail .foot a:hover{color:var(--termtext)}
.main{flex:1;display:flex;flex-direction:column;min-width:0;background:var(--bg)}
.topbar{display:flex;align-items:center;gap:18px;padding:14px 26px;border-bottom:1px solid var(--line)}
.topbar b{font-size:17px;font-weight:700;letter-spacing:-.02em}
.topbar a{color:var(--muted);text-decoration:none;font-family:var(--mono);font-size:11px;letter-spacing:.12em;text-transform:uppercase;margin-left:auto}
.topbar a+a{margin-left:18px}
.topbar a:hover{color:var(--text)}
#stream{flex:1;overflow-y:auto;padding:22px 28px}
.msg{display:flex;gap:12px;margin:10px 0;max-width:74ch}
.msg .ava{width:34px;height:34px;color:#fff;display:flex;align-items:center;justify-content:center;font-weight:700;font-size:13px;flex-shrink:0;text-transform:uppercase}
.msg .who{font-weight:600;font-size:13px}
.msg .who span{font-weight:400;color:var(--faint);margin-left:8px;font-size:11.5px;font-family:var(--mono)}
.msg .body{margin-top:2px;white-space:pre-wrap;word-wrap:break-word;line-height:1.5;font-size:14px}
.composer{border-top:1px solid var(--line);padding:14px 26px;display:flex;gap:12px}
.composer textarea{flex:1;resize:none;border:1px solid var(--line2);background:var(--bg);padding:11px 14px;font:inherit;min-height:46px;max-height:160px;outline:none}
.composer textarea:focus{border-color:var(--text)}
.composer button{background:var(--text);color:#fff;border:0;padding:0 22px;font-family:var(--mono);font-size:11px;font-weight:600;letter-spacing:.1em;text-transform:uppercase;cursor:pointer}
.composer button:hover{background:var(--red)}
.composer button:disabled{opacity:.4}
.hint{color:var(--faint);font-size:11.5px;font-family:var(--mono);align-self:center}
`

func (u *UI) handleChat(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>LeetOffice — Team</title><style>` + chatCSS + `</style></head>
<body>
` + xbNav("chat", u.Config.Actor) + `
<div class="app">
<nav class="rail">
  <div class="brand"><span class="pulse"></span>` + html.EscapeString(teamName(u)) + `<small>LOCAL · ENCRYPTED · YOURS</small></div>
  <h4>Channels</h4>
  <div id="chans"></div>
  <button class="new" id="newch" title="Create a channel">+ new channel</button>
  <h4>People &amp; agents</h4>
  <div class="people" id="people"></div>
  <div class="foot"><a href="/settings">settings</a><a href="/audit">history</a></div>
</nav>
<main class="main">
  <div class="topbar"><b id="chname"></b><span class="hint" id="members"></span></div>
  <div id="stream"></div>
  <form class="composer" id="composer">
    <textarea id="text" placeholder="Message — Enter sends, Shift+Enter adds a line" autofocus></textarea>
    <span class="hint" id="hint"></span><button id="send">Send</button>
  </form>
</main>
</div>
<script>
let current = localStorage.getItem('ch') || 'general';
let lastID = '';
function esc(s){const d=document.createElement('div');d.textContent=s;return d.innerHTML}
function color(s){const p=['#3b4b8f','#7c5cbf','#2f855a','#b7791f','#c05621','#2b6cb0','#9b2c2c','#6b46c1'];let h=0;for(const c of s)h=(h*31+c.codePointAt(0))|0;return p[Math.abs(h)%p.length]}
function avatar(a){return '<div class="ava" style="background:'+color(a)+'">'+esc((a.split(':')[1]||a)[0])+'</div>'}
async function refresh(){
  const q = current ? ('?channel='+encodeURIComponent(current)) : '';
  const st = await (await fetch('/api/state'+q)).json();
  const chEl = document.getElementById('chans');
  chEl.innerHTML = (st.channels||[]).map(c =>
    '<a class="ch'+(c.slug===current?' on':'')+'" href="#" data-ch="'+esc(c.slug)+'">#'+esc(c.slug)+
    (c.messages?'<span style="opacity:.6">'+c.messages+'</span>':'')+'</a>').join('')
    || '<div style="padding:.35rem 1.1rem;color:#8a93ab;font-size:.82rem">no channels yet</div>';
  chEl.querySelectorAll('a.ch').forEach(a=>a.onclick=e=>{e.preventDefault();pick(a.dataset.ch)});
  const pe = document.getElementById('people');
  pe.innerHTML = Object.entries(st.people||{}).sort((x,y)=> (y[1]==='online')-(x[1]==='online')).map(([p,s]) =>
    '<div class="person"><span class="dot '+s+'"></span>'+esc(p)+(p===st.me?' <span style="color:#8a93ab">(you)</span>':'')+'</div>').join('');
  document.getElementById('chname').textContent = current ? '#'+current : 'pick a channel';
  const mem=document.getElementById('members');
  if(mem){const n=(st.current&&st.current.messages)?st.current.messages.length:0;mem.textContent=n?n+' messages':''}
  const stream = document.getElementById('stream');
  const cur = st.current;
  if(cur){
    const atBottom = stream.scrollHeight-stream.scrollTop-stream.clientHeight < 80;
    stream.innerHTML = (cur.messages||[]).map(m =>
      '<div class="msg" id="m-'+esc(m.id)+'">'+avatar(m.author)+
      '<div><div class="who">'+esc(m.author)+'<span>'+new Date(m.at).toLocaleString()+'</span></div>'+
      '<div class="body">'+esc(m.text)+'</div></div></div>').join('') ||
      '<div style="color:#8a93ab">No messages yet — say hello.</div>';
    const last=(cur.messages||[])[cur.messages.length-1];
    if(last && last.id!==lastID){lastID=last.id; if(atBottom)stream.scrollTop=stream.scrollHeight}
  }
}
function pick(ch){current=ch;localStorage.setItem('ch',ch);lastID='';refresh()}
document.getElementById('newch').onclick=()=>{
  const n=prompt('New channel name (e.g. ops, design)'); if(!n)return;
  current=n.toLowerCase().replace(/[^a-z0-9-]+/g,'-').replace(/^-+|-+$/g,'')||'general';
  localStorage.setItem('ch',current);refresh();
};
const form=document.getElementById('composer'), text=document.getElementById('text');
text.addEventListener('keydown',e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();form.requestSubmit()}});
form.onsubmit=async e=>{
  e.preventDefault();
  const body=text.value.trim(); if(!body)return;
  const btn=document.getElementById('send'); btn.disabled=true;
  try{
    const res=await fetch('/api/send',{method:'POST',headers:{'Content-Type':'application/json'},
      body:JSON.stringify({channel:current,text:body})});
    if(!res.ok){document.getElementById('hint').textContent=await res.text();return}
    text.value='';document.getElementById('hint').textContent='';await refresh();
    document.getElementById('stream').scrollTop=1e9;
  }finally{btn.disabled=false;text.focus()}
};
refresh(); setInterval(refresh, 2500);
</script>
</body></html>`)
	w.Write([]byte(b.String()))
}

func teamName(u *UI) string {
	name := strings.TrimPrefix(u.Config.Actor, "human:")
	if name == "" {
		name = u.Config.NodeID
	}
	// When this node is joined to a team, say whose — a local-name label
	// here once made a joined node look like an isolated one.
	if share := u.Config.MainShare; share != "" {
		if at := strings.LastIndex(share, "@"); at >= 0 {
			return "team @ " + share[at+1:]
		}
		if host := strings.TrimPrefix(strings.TrimPrefix(share, "leet://"), "file://"); host != "" {
			if i := strings.IndexAny(host, "/"); i > 0 {
				return "team @ " + host[:i]
			}
		}
	}
	return name + "'s team (local)"
}
