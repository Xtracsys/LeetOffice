// The team-chat shell (M17 surface): a Teams/ZCode-style layout — channel
// rail with team presence, message stream, composer — served by the daemon
// over localhost. It is a view over the store: messages are attributed blocks
// in channel docs, synced by the same git transport as everything else.
package httpui

import (
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"leetoffice/internal/chat"
	leetNet "leetoffice/internal/net"
)

// --- JSON API ----------------------------------------------------------------

type peerCache struct {
	mu       sync.Mutex
	at       time.Time
	entries  []string
	inflight chan struct{}
}

var peers peerCache

// onlineNodes lists node ids seen on the LAN recently (mDNS), cached so the
// poll endpoint doesn't hammer multicast.
func onlineNodes() []string {
	peers.mu.Lock()
	if time.Since(peers.at) < 5*time.Second {
		out := peers.entries
		peers.mu.Unlock()
		return out
	}
	if peers.inflight != nil {
		wait := peers.inflight
		peers.mu.Unlock()
		<-wait
		peers.mu.Lock()
		out := peers.entries
		peers.mu.Unlock()
		return out
	}
	done := make(chan struct{})
	peers.inflight = done
	peers.mu.Unlock()

	found, err := leetNet.DiscoverPeers(700 * time.Millisecond)
	if err != nil {
		found = nil
	}
	var ids []string
	for _, p := range found {
		ids = append(ids, p.NodeID)
	}
	peers.mu.Lock()
	peers.entries, peers.at = ids, time.Now()
	peers.inflight = nil
	close(done)
	peers.mu.Unlock()
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

	// Chat polls this every 2.5s. Presence (mDNS + git audit) is cached so
	// a wedged discovery lookup cannot freeze the message stream.
	people := u.cachedPeople()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"me":       u.Config.Actor,
		"channels": chanRows,
		"current":  current,
		"people":   people,
	})
}

type peopleSnap struct {
	mu     sync.Mutex
	at     time.Time
	people map[string]string
}

var peopleCache peopleSnap

func (u *UI) cachedPeople() map[string]string {
	peopleCache.mu.Lock()
	if time.Since(peopleCache.at) < 8*time.Second && peopleCache.people != nil {
		out := peopleCache.people
		peopleCache.mu.Unlock()
		return out
	}
	peopleCache.mu.Unlock()

	people := map[string]string{}
	for _, node := range onlineNodes() {
		if u.Config.IsHidden(node) || u.Config.IsHidden("node:"+node) {
			continue
		}
		people["node:"+node] = "online"
	}
	for _, a := range chat.RecentActors(u.Repo, 24*time.Hour) {
		if u.Config.IsHidden(a) {
			continue
		}
		if _, ok := people[a]; !ok {
			people[a] = "recent"
		}
	}
	if _, ok := people[u.Config.Actor]; !ok {
		people[u.Config.Actor] = "online"
	}
	peopleCache.mu.Lock()
	peopleCache.people, peopleCache.at = people, time.Now()
	peopleCache.mu.Unlock()
	return people
}

func (u *UI) handleAgentInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	actor := strings.TrimSpace(r.URL.Query().Get("actor"))
	if actor == "" {
		http.Error(w, "actor required", http.StatusBadRequest)
		return
	}
	sub, ok := u.Config.Subscription(actor)
	if !ok {
		http.Error(w, "subscribe first", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	since := r.URL.Query().Get("since")
	if since == "" {
		since = r.URL.Query().Get("since_ts")
	}
	items, err := chat.CollectInbox(u.Store, sub, r.URL.Query().Get("channel"), since, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "count": len(items)})
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
	if u.KickSync != nil {
		u.KickSync()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msg)
}

// --- the shell ----------------------------------------------------------------

const chatCSS = xbCSS + `
html,body{height:100%;min-height:0}
body{height:100dvh;font-size:14px;display:flex;flex-direction:column;overflow:hidden;background:var(--bg)}
header.topnav{flex-shrink:0}
.navwrap{max-width:none;height:54px;padding:0 20px}
.navbrand{color:var(--text)}
.app{flex:1;display:flex;min-height:0}
.rail{width:258px;background:var(--term);color:var(--termtext);display:flex;flex-direction:column;flex-shrink:0;border-right:1px solid var(--termline);min-height:0}
.rail .brand{padding:17px 20px;font-family:var(--mono);font-weight:700;letter-spacing:.15em;font-size:12px;border-bottom:1px solid var(--termline);display:flex;align-items:center;gap:10px}
.rail .brand small{display:block;font-weight:400;font-size:9px;color:rgba(248,245,237,.68);margin-top:4px;letter-spacing:.1em}
.rail h4{font-family:var(--mono);font-size:9px;text-transform:uppercase;letter-spacing:.18em;color:rgba(248,245,237,.68);padding:16px 20px 7px}
#chans{overflow-y:auto;min-height:0;padding:0 10px}
.rail a.ch{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:8px 10px;color:var(--termtext);font-family:var(--mono);font-size:11px;letter-spacing:.035em;border-left:2px solid transparent}
.rail a.ch:hover{background:rgba(248,245,237,.08);color:#fff}
.rail a.ch.on{background:rgba(217,42,42,.18);border-left-color:var(--red);color:#fff}
.rail .count{color:rgba(248,245,237,.68);font-size:10px}
.rail .new{margin:9px 18px 5px;width:calc(100% - 36px);font-family:var(--mono);font-size:10px;letter-spacing:.08em;background:transparent;border:1px dashed rgba(248,245,237,.45);color:rgba(248,245,237,.82);padding:8px 10px;cursor:pointer}
.rail .new:hover{color:#fff;border-color:#fff;background:rgba(248,245,237,.07)}
.rail .empty-list{padding:8px 20px;color:rgba(248,245,237,.72);font-size:12px}
.rail .people{flex:1;min-height:60px;overflow-y:auto;padding:0 0 12px}
.rail .person{display:flex;align-items:center;gap:9px;padding:5px 20px;font-size:11.5px;color:rgba(248,245,237,.9)}
.rail .person .you{color:rgba(248,245,237,.68);font-size:10px}
.rail .person .presence-status{color:rgba(248,245,237,.68);font-family:var(--mono);font-size:9px;text-transform:uppercase;letter-spacing:.04em}
.rail .person .hide{margin-left:auto;font-family:var(--mono);font-size:9px;letter-spacing:.08em;text-transform:uppercase;color:rgba(248,245,237,.72);border-bottom:1px solid transparent}
.rail .person .hide:hover{color:#fff;border-bottom-color:var(--red)}
.dot{width:7px;height:7px;border-radius:50%;flex-shrink:0}
.dot.online{background:#ed5b5b}
.dot.recent{background:#aaa99f}
.rail .foot{padding:13px 20px;border-top:1px solid var(--termline);display:flex;gap:16px}
.rail .foot a{font-family:var(--mono);font-size:9px;letter-spacing:.13em;text-transform:uppercase;color:rgba(248,245,237,.72)}
.rail .foot a:hover{color:#fff}
.main{flex:1;display:flex;flex-direction:column;min-width:0;min-height:0;background:var(--bg)}
.topbar{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:12px 28px;min-height:68px;border-bottom:1px solid var(--line);background:var(--bg)}
.topbar-main{display:flex;align-items:baseline;gap:13px;min-width:0}
.topbar b{font-size:18px;font-weight:700;letter-spacing:-.025em;overflow-wrap:anywhere}
.topbar-state{display:flex;flex-direction:column;align-items:flex-end;gap:2px;min-width:0}
.connection{display:flex;align-items:center;gap:8px;font-family:var(--mono);font-size:10px;letter-spacing:.025em;color:var(--muted);text-align:right}
.status-dot{width:8px;height:8px;border-radius:50%;background:var(--warn);flex:none}
.connection[data-state="connected"] .status-dot{background:var(--good)}
.connection[data-state="stale"] .status-dot{background:var(--warn)}
.connection[data-state="error"] .status-dot{background:var(--red)}
.connection[data-state="loading"] .status-dot{background:#8a8375}
.sync-note{font-family:var(--mono);font-size:9px;line-height:1.35;color:var(--faint);text-align:right}
#members{white-space:nowrap}
.stream-wrap{position:relative;flex:1;min-height:0}
#stream{height:100%;overflow-y:auto;padding:20px 28px 30px;overscroll-behavior:contain;scrollbar-gutter:stable}
.msg{display:flex;gap:12px;margin:0;max-width:850px;padding:13px 0;border-bottom:1px solid var(--line)}
.msg .ava{width:34px;height:34px;color:#fff;display:flex;align-items:center;justify-content:center;font-weight:700;font-size:12px;flex-shrink:0;text-transform:uppercase;background:var(--text)}
.msg .who{font-weight:650;font-size:12.5px;line-height:1.4}
.msg .who span{font-weight:400;color:var(--muted);margin-left:9px;font-size:10px;font-family:var(--mono)}
.msg .body{margin-top:4px;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.55;font-size:14px}
.empty-state{max-width:720px;margin:22px 0;padding:22px 24px;border:1px solid var(--line);border-left:3px solid var(--red);background:var(--surface)}
.empty-state h2{margin:0 0 5px;font-size:15px;letter-spacing:0}
.empty-state p{color:var(--muted);font-size:13px}
.jump-latest{position:absolute;left:50%;bottom:14px;transform:translateX(-50%);z-index:2;box-shadow:0 4px 14px rgba(22,22,21,.18);white-space:nowrap}
.composer{border-top:1px solid var(--line);padding:13px 28px 11px;display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px 12px;background:var(--bg)}
.composer textarea{grid-column:1;grid-row:1;resize:vertical;border:1px solid var(--line2);background:#fff;padding:11px 13px;font:inherit;min-height:48px;max-height:160px;line-height:1.45;color:var(--text)}
.composer textarea::placeholder{color:#696960}
.composer textarea:focus{border-color:var(--red)}
.composer textarea:focus-visible{outline:3px solid var(--red);outline-offset:2px}
.composer button{grid-column:2;grid-row:1;align-self:stretch;min-width:94px;background:var(--text);color:#fff;border:1px solid var(--text);padding:0 20px;font-family:var(--mono);font-size:10px;font-weight:600;letter-spacing:.1em;text-transform:uppercase;cursor:pointer}
.composer button:hover{background:var(--red);border-color:var(--red)}
.composer button:disabled{opacity:.6;cursor:wait}
.compose-meta{grid-column:1/-1;display:flex;justify-content:space-between;gap:12px;min-height:18px}
.hint{color:var(--muted);font-size:10px;font-family:var(--mono)}
#send-status{color:var(--red);text-align:right}
@media(max-width:760px){
 .navwrap{height:50px;padding:0 12px;gap:12px}
 .navbrand{font-size:11px;letter-spacing:.08em}
 nav.bar{gap:13px}
 .app{min-height:0}
 .rail{width:auto;max-height:104px;min-height:92px;display:grid;grid-template-columns:minmax(0,1fr) auto;grid-template-rows:minmax(40px,auto) minmax(38px,auto);align-items:center;gap:0 8px;padding:3px 8px;border-right:0;border-bottom:1px solid var(--termline);overflow:hidden}
 .rail .brand,.rail h4{display:none}
 #chans{grid-column:1;grid-row:1;display:flex;align-items:center;gap:3px;overflow-x:auto;overflow-y:hidden;min-width:0;padding:3px 0}
 .rail a.ch{flex:none;white-space:nowrap;padding:7px 9px;border-left:0;border-bottom:2px solid transparent}
 .rail a.ch.on{border-bottom-color:var(--red)}
 .rail .new{grid-column:2;grid-row:1;width:auto;white-space:nowrap;margin:0 2px;padding:7px 9px;font-size:9px}
 .rail .people{grid-column:1/-1;grid-row:2;display:flex;align-items:center;flex:none;overflow-x:auto;overflow-y:hidden;min-height:0;padding:0}
 .rail .person{flex:none;white-space:nowrap;padding:4px 9px;font-size:10.5px}
 .rail .person .hide{margin-left:3px}
 .rail .foot{display:none}
 .topbar{min-height:58px;padding:8px 14px;gap:8px}
 .topbar-main{gap:7px;flex-wrap:wrap}
 .topbar b{font-size:16px}
 .topbar-state{max-width:52%;gap:1px}
 .connection{font-size:9px}
 .sync-note{max-width:175px;font-size:8px}
 #stream{padding:14px 14px 24px}
 .msg{gap:9px;padding:10px 0}
 .composer{padding:9px 11px;gap:7px 9px}
 .composer textarea{min-height:44px;padding:9px 10px}
 .composer button{min-width:78px;padding:0 13px}
 .compose-meta{align-items:flex-start;flex-direction:column;gap:0}
 #send-status{text-align:left}
}
@media(max-width:380px){
 .sync-note{max-width:145px;font-size:8px}
 .topbar{padding-left:10px;padding-right:10px}
 .connection{font-size:8px}
 #stream{padding-left:11px;padding-right:11px}
 .composer{padding-left:9px;padding-right:9px}
}
`

func (u *UI) handleChat(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>LeetOffice — Team</title><style>` + chatCSS + `</style></head>
<body>
<a class="skiplink" href="#main-content">Skip to main content</a>
` + xbNav("chat", u.Config.Actor) + `
<div class="app">
<nav class="rail" aria-label="Workspace">
  <div class="brand"><span class="pulse" aria-hidden="true"></span><span>` + html.EscapeString(teamName(u)) + `<small>LOCAL · ENCRYPTED · YOURS</small></span></div>
  <h4 id="channels-heading">Channels</h4>
  <div id="chans" role="group" aria-label="Channels"><div class="empty-list">Loading channels…</div></div>
  <button class="new" id="newch" type="button" title="Create a channel">+ New channel</button>
  <h4 id="people-heading">People &amp; agents</h4>
  <div class="people" id="people" role="group" aria-label="People and agents"></div>
  <div class="foot"><a href="/settings">Settings</a><a href="/audit">History</a></div>
</nav>
<main class="main" id="main-content" tabindex="-1">
  <div class="topbar">
    <div class="topbar-main"><b id="chname">#general</b><span class="hint" id="members"></span></div>
    <div class="topbar-state">
      <div class="connection" id="connection" data-state="loading" role="status" aria-live="polite" aria-atomic="true"><span class="status-dot" aria-hidden="true"></span><span id="connection-text">Connecting to this node…</span></div>
      <span class="sync-note">Local data · Git sync not reported here.<span id="last-updated" aria-live="off"></span></span>
    </div>
  </div>
  <div class="stream-wrap">
    <div id="stream" role="log" aria-label="Messages" aria-live="off" tabindex="0"><div class="empty-state"><h2>Loading messages</h2><p>Checking the local workspace…</p></div></div>
    <button class="jump-latest" id="jump-latest" type="button" hidden>New messages ↓</button>
  </div>
  <div class="sr-only" id="announcer" aria-live="polite" aria-atomic="true"></div>
  <form class="composer" id="composer" aria-label="Send a message">
    <label class="sr-only" for="text">Message</label>
    <textarea id="text" placeholder="Write a message…" aria-describedby="composer-hint send-status" autofocus></textarea>
    <button id="send" type="submit" disabled>Send</button>
    <div class="compose-meta"><span class="hint" id="composer-hint">Enter sends · Shift+Enter adds a line</span><span class="hint" id="send-status" role="status" aria-live="polite"></span></div>
  </form>
</main>
</div>
<script>
const params = new URLSearchParams(window.location.search);
function normalizeChannel(value){return String(value||'').toLowerCase().replace(/[^a-z0-9-]+/g,'-').replace(/^-+|-+$/g,'')}
function initialChannel(){
  const query=params.get('channel');
  if(query)return normalizeChannel(query)||'general';
  const hash=window.location.hash.slice(1);
  if(hash){try{return normalizeChannel(decodeURIComponent(hash))||'general'}catch{}}
  try{return normalizeChannel(localStorage.getItem('ch'))||'general'}catch{return 'general'}
}
let current = initialChannel();
let lastID = '';
let lastCount = 0;
let renderedChannel = '';
let lastSuccessAt = 0;
let inflight = false;
let refreshPending = false;
let forceBottom = false;
let draftChannel = '';
let sending = false;
function esc(value){return String(value==null?'':value).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]))}
function color(value){const colors=['#d92a2a','#292928','#4b514e','#6b3535','#4b5870'];let h=0;for(const c of String(value||''))h=(h*31+c.codePointAt(0))|0;return colors[Math.abs(h)%colors.length]}
function avatar(author){const label=String(author||'?').split(':').pop();return '<div class="ava" aria-hidden="true" style="background:'+color(author)+'">'+esc([...label][0]||'?')+'</div>'}
function setConnection(state,message){
  const connection=document.getElementById('connection'),label=document.getElementById('connection-text');
  connection.dataset.state=state;
  if(label.textContent!==message)label.textContent=message;
}
function setSendStatus(message){document.getElementById('send-status').textContent=message||''}
function setHTML(el,markup,selector,keyName){
  if(el.innerHTML===markup)return false;
  const active=document.activeElement;
  const key=selector&&el.contains(active)?active.getAttribute(keyName):null;
  el.innerHTML=markup;
  if(key!==null){const next=[...el.querySelectorAll(selector)].find(node=>node.getAttribute(keyName)===key);if(next)next.focus({preventScroll:true})}
  return true;
}
function rememberChannel(channel,pushHistory=true){
  try{localStorage.setItem('ch',channel)}catch{}
  if(pushHistory)try{const url=new URL(window.location.href);if(url.searchParams.get('channel')!==channel){url.searchParams.set('channel',channel);url.hash='';history.pushState(null,'',url)}}catch{}
}
function formatTime(value){const date=new Date(value);return Number.isNaN(date.getTime())?'':date.toLocaleString([], {dateStyle:'medium',timeStyle:'short'})}
function nearBottom(el){return el.scrollHeight-el.scrollTop-el.clientHeight<80}
function updateJump(){
  if(nearBottom(document.getElementById('stream')))document.getElementById('jump-latest').hidden=true;
  else forceBottom=false;
}
async function refresh(force=false){
  if(inflight){if(force)refreshPending=true;return}
  inflight=true;refreshPending=false;
  const requested=current;
  const controller=new AbortController();
  const timeout=setTimeout(()=>controller.abort(),5000);
  try{
    const response=await fetch('/api/state?channel='+encodeURIComponent(requested),{headers:{'Accept':'application/json'},cache:'no-store',signal:controller.signal});
    if(!response.ok)throw new Error('Local service returned HTTP '+response.status);
    const state=await response.json();
    if(requested!==current){refreshPending=true;return}
    if(!state||!Array.isArray(state.channels))throw new Error('Invalid local state response');
    const channels=state.channels;
    const channelExists=channels.some(channel=>channel.slug===current);
    const channelRows=channels.slice();
    if(!channelExists&&draftChannel===current)channelRows.push({slug:current,messages:0,draft:true});
    const channelHTML=channelRows.length?channelRows.map(channel=>{
      const active=channel.slug===current;
      const count=Number(channel.messages)||0;
      const label=channel.draft?'NEW':(count?String(count):'');
      return '<a class="ch'+(active?' on':'')+'" href="/?channel='+encodeURIComponent(channel.slug)+'" data-ch="'+esc(channel.slug)+'"'+(active?' aria-current="page"':'')+'><span>#'+esc(channel.slug)+'</span>'+(label?'<span class="count">'+esc(label)+'</span>':'')+'</a>'
    }).join(''):'<div class="empty-list">No channels yet. Create one to start.</div>';
    const chEl=document.getElementById('chans');
    setHTML(chEl,channelHTML,'a.ch','data-ch');
    chEl.querySelectorAll('a.ch').forEach(link=>link.onclick=event=>{event.preventDefault();pick(link.dataset.ch)});

    const people=Object.entries(state.people||{}).sort((left,right)=>(right[1]==='online')-(left[1]==='online'));
    const peopleHTML=people.map(([person,presence])=>{
      const online=presence==='online';
      const own=person===state.me;
      return '<div class="person"><span class="dot '+(online?'online':'recent')+'" aria-hidden="true"></span><span>'+esc(person)+'</span><span class="presence-status">'+(online?'online':'recent')+'</span>'+(own?' <span class="you">(you)</span>':' <a class="hide" href="/settings" data-hide="'+esc(person)+'" aria-label="Hide '+esc(person)+'">hide</a>')+'</div>'
    }).join('')||'<div class="empty-list">No recent people yet.</div>';
    const peopleEl=document.getElementById('people');
    setHTML(peopleEl,peopleHTML,'a.hide','data-hide');
    peopleEl.querySelectorAll('a.hide').forEach(link=>link.onclick=async event=>{
      event.preventDefault();
      try{
        const hidden=await fetch('/settings/hide',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded','Accept':'application/json'},body:'actor='+encodeURIComponent(link.dataset.hide)});
        if(!hidden.ok)throw new Error('Could not update people');
        setSendStatus('Person hidden.');
        refresh(true);
      }catch{setSendStatus('Could not update people. Check the local connection and retry.');}
    });

    document.getElementById('chname').textContent=current?'#'+current:'Choose a channel';
    document.title='LeetOffice — '+(current?'#'+current:'Team');
    const messages=state.current&&Array.isArray(state.current.messages)?state.current.messages:[];
    const members=document.getElementById('members');
    members.textContent=messages.length+' '+(messages.length===1?'message':'messages');
    if(draftChannel===current&&channelExists)draftChannel='';
    const stream=document.getElementById('stream');
    const channelChanged=renderedChannel!==current;
    const wasAtBottom=nearBottom(stream);
    const last=messages[messages.length-1];
    const newMessage=!channelChanged&&!!last&&last.id!==lastID;
    const addedCount=channelChanged?0:Math.max(0,messages.length-lastCount);
    const streamHTML=messages.map(message=>'<article class="msg" id="m-'+esc(message.id)+'">'+avatar(message.author)+'<div><div class="who">'+esc(message.author)+'<span>'+esc(formatTime(message.at))+'</span></div><div class="body">'+esc(message.text)+'</div></div></article>').join('')||'<section class="empty-state"><h2>'+((!channelExists||draftChannel===current)?'Ready for a first message':'Start the conversation')+'</h2><p>'+(draftChannel===current?'Send the first message to create #'+esc(current)+'.':'No messages in #'+esc(current)+' yet. Write below to get started.')+'</p></section>';
    const changed=setHTML(stream,streamHTML);
    if(channelChanged){renderedChannel=current;lastCount=messages.length;lastID=last?last.id:'';forceBottom=true}
    else{
      if(newMessage){lastID=last.id;const added=Math.max(addedCount,1);document.getElementById('announcer').textContent=added+' new '+(added===1?'message':'messages')+' in #'+current+'.'}
      lastCount=messages.length;
    }
    if((changed||channelChanged||forceBottom)&&(channelChanged||forceBottom||wasAtBottom)){stream.scrollTop=stream.scrollHeight;document.getElementById('jump-latest').hidden=true}
    else if(newMessage)document.getElementById('jump-latest').hidden=false;
    forceBottom=false;
    lastSuccessAt=Date.now();
    document.getElementById('last-updated').textContent=' · Updated '+new Date(lastSuccessAt).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'});
    setConnection('connected','Connected');
    if(!sending)send.disabled=false;
  }catch(error){
    if(requested===current){
      if(!sending)send.disabled=renderedChannel!==current;
      if(lastSuccessAt){
        const time=new Date(lastSuccessAt).toLocaleTimeString([], {hour:'numeric',minute:'2-digit'});
        const shown=renderedChannel?'#'+renderedChannel:'previous data';
        setConnection('stale',renderedChannel!==requested?'Could not load #'+requested+' · showing '+shown+' from '+time:'Connection lost · showing '+shown+' from '+time);
      }else setConnection('error','Can’t reach this node · retrying');
    }
  }finally{
    clearTimeout(timeout);
    inflight=false;
    if(refreshPending){refreshPending=false;refresh()}
  }
}
function pick(channel){
  const normalized=normalizeChannel(channel);
  if(!normalized)return;
  current=normalized;rememberChannel(current);forceBottom=true;send.disabled=true;setSendStatus('');setConnection('loading','Loading #'+current+'…');refresh(true);
}
document.getElementById('newch').onclick=()=>{
  const name=prompt('Channel name (letters, numbers, hyphens)');
  if(name===null)return;
  const channel=normalizeChannel(name);
  if(!channel){setSendStatus('Use letters, numbers, or hyphens for a channel name.');document.getElementById('text').focus();return}
  draftChannel=channel;pick(channel);document.getElementById('text').focus();
};
const form=document.getElementById('composer'),text=document.getElementById('text'),send=document.getElementById('send');
text.addEventListener('keydown',event=>{if(event.key==='Enter'&&!event.shiftKey&&!event.isComposing){event.preventDefault();form.requestSubmit()}});
text.addEventListener('input',()=>setSendStatus(''));
form.onsubmit=async event=>{
  event.preventDefault();
  if(sending||renderedChannel!==current){setSendStatus('Wait for this channel to load before sending.');return}
  const draft=text.value,body=draft.trim();
  if(!body)return;
  const channel=current,restoreFocus=document.activeElement===text||document.activeElement===send;
  const controller=new AbortController(),timeout=setTimeout(()=>controller.abort(),15000);
  sending=true;send.disabled=true;setSendStatus('Sending…');
  try{
    const response=await fetch('/api/send',{method:'POST',headers:{'Content-Type':'application/json','Accept':'application/json'},body:JSON.stringify({channel,text:body}),signal:controller.signal});
    if(!response.ok){const detail=(await response.text()).trim();setSendStatus((detail||'Message could not be sent. Check the local connection and retry.').slice(0,180));return}
    if(text.value===draft)text.value='';
    if(draftChannel===channel)draftChannel='';
    if(current===channel)forceBottom=true;
    setSendStatus('Sent to #'+channel+'.');
    await refresh(true);
  }catch{setSendStatus('Could not send. Your draft is still here; check the local connection and retry.');}
  finally{clearTimeout(timeout);sending=false;send.disabled=renderedChannel!==current;if(restoreFocus)text.focus({preventScroll:true})}
};
document.getElementById('jump-latest').onclick=()=>{const stream=document.getElementById('stream');stream.scrollTop=stream.scrollHeight;document.getElementById('jump-latest').hidden=true};
document.getElementById('stream').addEventListener('scroll',updateJump,{passive:true});
window.addEventListener('online',()=>refresh(true));
window.addEventListener('popstate',()=>{
  const channel=normalizeChannel(new URLSearchParams(window.location.search).get('channel'))||'general';
  if(channel===current)return;
  current=channel;rememberChannel(current,false);forceBottom=true;send.disabled=true;setSendStatus('');setConnection('loading','Loading #'+current+'…');refresh(true);
});
document.addEventListener('visibilitychange',()=>{if(document.visibilityState==='visible')refresh(true)});
refresh();setInterval(()=>{if(document.visibilityState==='visible')refresh()},2500);
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
