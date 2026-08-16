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
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,system-ui,"Segoe UI",sans-serif;font-size:14px;color:#1f1f1f;height:100vh;display:flex;overflow:hidden}
.rail{width:264px;background:#1b1f2b;color:#cfd4e4;display:flex;flex-direction:column;flex-shrink:0}
.rail .brand{padding:1rem 1.1rem;font-weight:700;letter-spacing:.02em;color:#fff;border-bottom:1px solid #2a3040}
.rail .brand small{display:block;font-weight:400;font-size:.72rem;color:#8a93ab;margin-top:.15rem}
.rail h4{font-size:.68rem;text-transform:uppercase;letter-spacing:.12em;color:#8a93ab;padding:.9rem 1.1rem .35rem}
.rail a.ch{display:flex;justify-content:space-between;padding:.38rem 1.1rem;color:#cfd4e4;text-decoration:none;border-radius:0 18px 18px 0;margin-right:.6rem}
.rail a.ch:hover{background:#242a3a}
.rail a.ch.on{background:#3b4b8f;color:#fff}
.rail .new{margin:.3rem 1.1rem .3rem .5rem;font-size:.8rem;color:#8a93ab;background:none;border:1px dashed #3a4256;border-radius:6px;padding:.3rem .5rem;cursor:pointer;color:#aab2c9;width:calc(100% - 1.6rem)}
.rail .people{flex:1;overflow-y:auto;padding-bottom:1rem}
.rail .person{display:flex;align-items:center;gap:.5rem;padding:.28rem 1.1rem;font-size:.83rem}
.dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.dot.online{background:#4ade80}.dot.recent{background:#8a93ab}
.main{flex:1;display:flex;flex-direction:column;min-width:0;background:#fff}
.topbar{display:flex;align-items:center;gap:1rem;padding:.7rem 1.2rem;border-bottom:1px solid #e5e7ec}
.topbar b{font-size:1.05rem}
.topbar a{color:#3b4b8f;text-decoration:none;font-size:.85rem;margin-left:auto}
.topbar a+a{margin-left:1.1rem}
#stream{flex:1;overflow-y:auto;padding:1.1rem 1.3rem}
.msg{display:flex;gap:.7rem;margin:.45rem 0;max-width:72ch}
.msg .ava{width:34px;height:34px;border-radius:50%;color:#fff;display:flex;align-items:center;justify-content:center;font-weight:700;font-size:.8rem;flex-shrink:0;text-transform:uppercase}
.msg .who{font-weight:600;font-size:.85rem}
.msg .who span{font-weight:400;color:#8a93ab;margin-left:.45rem;font-size:.76rem}
.msg .body{margin-top:.1rem;white-space:pre-wrap;word-wrap:break-word;line-height:1.45}
.composer{border-top:1px solid #e5e7ec;padding:.8rem 1.2rem;display:flex;gap:.7rem}
.composer textarea{flex:1;resize:none;border:1px solid #d3d7e0;border-radius:8px;padding:.6rem .8rem;font:inherit;min-height:44px;max-height:160px;outline:none}
.composer textarea:focus{border-color:#3b4b8f}
.composer button{background:#3b4b8f;color:#fff;border:0;border-radius:8px;padding:0 1.2rem;font:inherit;font-weight:600;cursor:pointer}
.composer button:disabled{opacity:.5}
.hint{color:#8a93ab;font-size:.78rem;align-self:center}
`

func (u *UI) handleChat(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>LeetOffice — Team</title><style>` + chatCSS + `</style></head>
<body>
<nav class="rail">
  <div class="brand">` + html.EscapeString(teamName(u)) + `<small>local · encrypted · yours</small></div>
  <h4>Channels</h4>
  <div id="chans"></div>
  <button class="new" id="newch" title="Create a channel">+ new channel</button>
  <h4>People &amp; agents</h4>
  <div class="people" id="people"></div>
</nav>
<main class="main">
  <div class="topbar"><b id="chname"></b>
    <a href="/docs">docs</a><a href="/memory">memory</a><a href="/agents">agents</a>
  </div>
  <div id="stream"></div>
  <form class="composer" id="composer">
    <textarea id="text" placeholder="Message — Enter sends, Shift+Enter adds a line" autofocus></textarea>
    <span class="hint" id="hint"></span><button id="send">Send</button>
  </form>
</main>
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
	return name + "'s team"
}
