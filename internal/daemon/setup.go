// First-run experience: when no config exists, `leetd` boots into a setup
// wizard served on the same localhost UI port (D14 keeps the daemon headless —
// this is presentation over the same config/enroll APIs the CLI uses). The
// wizard transitions the process into a full node without a restart.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"leetoffice/internal/chat"
	"leetoffice/internal/config"
	"leetoffice/internal/hostsign"
	leetNet "leetoffice/internal/net"
	"leetoffice/internal/store"
)

// Daemon is the always-on HTTP process: either a full node, or (before
// first-run setup) a wizard that becomes one.
type Daemon struct {
	cfgPath string
	ctx     context.Context // process-lifetime context for node loops

	mu      sync.Mutex
	handler http.Handler
}

// ListenAndServe is the daemon entrypoint. Missing config is not an error —
// it means first-run: the wizard is served until a team is created or joined,
// then the handler swaps to the live node.
//
// The HTTP listener is the process's heartbeat: if the port is taken or
// Serve returns, this function returns so the process exits and launchd
// KeepAlive can restart it. Logging the bind error and waiting on SIGTERM
// left a dead coordinator that had to be restarted by hand.
func ListenAndServe(ctx context.Context, cfgPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	quietKnownNoise()
	if bin, err := os.Executable(); err == nil && isLeetdBinary(bin) {
		if err := hostsign.Ensure(bin); err != nil {
			log.Printf("hostsign: %v", err)
		}
	}
	d := &Daemon{cfgPath: cfgPath, ctx: ctx}
	addr := httpListenAddr(cfgPath)

	var node *Node
	if _, err := os.Stat(cfgPath); err == nil {
		n, cfg, err := StartAtPath(cfgPath)
		if err != nil {
			return fmt.Errorf("start node from %s: %w", cfgPath, err)
		}
		node = n
		addr = cfg.Listen.HTTP
	} else {
		d.mu.Lock()
		d.handler = d.setupHandler()
		d.mu.Unlock()
		log.Printf("setup: no config at %s — first-run wizard on http://%s", cfgPath, addr)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("http listen %s: %w — another leetd is probably already running", addr, err)
	}
	if node != nil {
		if err := d.becomeNode(ctx, node); err != nil {
			_ = ln.Close()
			return err
		}
	}

	httpSrv := &http.Server{Handler: d}
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("http: listening on %s", ln.Addr())
		serveErr <- httpSrv.Serve(ln)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http: %w", err)
		}
		return nil
	case <-stop:
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutCtx)
}

func isLeetdBinary(path string) bool {
	base := filepath.Base(path)
	return base == "leetd" || base == "leetd.exe" || strings.HasPrefix(base, "leetd-")
}

func httpListenAddr(cfgPath string) string {
	addr := config.Default("", "").Listen.HTTP
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return addr
	}
	if cfg.Listen.HTTP != "" {
		return cfg.Listen.HTTP
	}
	return addr
}

// becomeNode swaps the daemon's handler to the live node and starts its loops.
func (d *Daemon) becomeNode(ctx context.Context, node *Node) error {
	if err := node.StartLoops(ctx); err != nil {
		// Git/enroll bind can fail when a leftover leetd still holds
		// 7666/7443; the UI must still come up so the operator can act.
		log.Printf("node loops: %v (continuing in degraded mode)", err)
	}
	d.mu.Lock()
	d.handler = node.ServeHTTP()
	d.mu.Unlock()
	return nil
}

func (d *Daemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	h := d.handler
	d.mu.Unlock()
	h.ServeHTTP(w, r)
}

// --- wizard actions (shared with the CLI) ----------------------------------

func wizardPaths(storeDir string) (share, identity string) {
	parent := filepath.Dir(storeDir)
	return filepath.Join(parent, leetNet.DefaultRepoName), config.IdentityDirFor(storeDir)
}

// CreateTeam builds a coordinator node config: local main share, enrollment
// secret, identity dir outside the store. Returns the secret to display once.
func CreateTeam(cfgPath, storeDir, actor string) (secret string, err error) {
	cfg := config.Default(storeDir, actor)
	cfg.Role = "coordinator"
	share, identity := wizardPaths(storeDir)
	cfg.MainShare = "file://" + share
	cfg.IdentityDir = identity
	cfg.EnrollmentSecret = store.NewID()[:12]
	if _, err := InitBareShare(cfg); err != nil {
		return "", err
	}
	if err := cfg.Save(cfgPath); err != nil {
		return "", err
	}
	return cfg.EnrollmentSecret, nil
}

// JoinTeam enrolls against a coordinator (D8) and writes the client config.
func JoinTeam(cfgPath, storeDir, actor, coordinator, secret string) error {
	cfg := config.Default(storeDir, actor)
	_, identity := wizardPaths(storeDir)
	cfg.IdentityDir = identity
	id, info, err := leetNet.Enroll(coordinator, cfg.NodeID, secret, "")
	if err != nil {
		return fmt.Errorf("enrollment rejected: %w", err)
	}
	if err := id.Save(cfg.IdentityDir); err != nil {
		return err
	}
	// The share URL must point at the GIT service, not the enrollment port
	// we just talked to — the coordinator tells us where git lives, and we
	// only fall back to the default port for older coordinators. GitPath
	// is the real repo name (main.git, or main-share.git on a v0.1.0
	// coordinator); empty means a pre-fix coordinator, so /main.git.
	gitAddr := info.GitAddr
	if gitAddr == "" {
		host, _, splitErr := net.SplitHostPort(coordinator)
		if splitErr != nil {
			host = coordinator
		}
		gitAddr = net.JoinHostPort(host, fmt.Sprint(leetNet.DefaultPort))
	}
	cfg.MainShare = leetNet.ShareRemote(gitAddr, info.GitPath)
	return cfg.Save(cfgPath)
}

// CreateLocal makes a single-player node: no share, no network role.
func CreateLocal(cfgPath, storeDir, actor string) error {
	cfg := config.Default(storeDir, actor)
	_, identity := wizardPaths(storeDir)
	cfg.IdentityDir = identity
	return cfg.Save(cfgPath)
}

// seedWelcome gives a brand-new team a living workspace instead of empty
// lists: a welcome doc, a starter channel with a first message, and a
// general greeting from the creator. No-op when the store already has
// content (joiners receive the real team's history via sync).
func seedWelcome(node *Node, actor string) {
	if docs, err := node.Store.List(); err == nil && len(docs) > 0 {
		return
	}
	d := store.NewDoc(store.TypeDoc, "welcome", "Welcome to LeetOffice")
	d.Tags = []string{"summary"}
	d.AddParagraph("This workspace is yours: chat in channels, write docs, link anything to anything.")
	d.AddParagraph("Every change — yours or an AI agent's — is attributed in History. Nothing leaves your machines.")
	if err := node.Store.Save(d, actor); err == nil {
		_, _ = node.Repo.CommitAll(actor, "welcome: first doc")
	}
	_, _, _ = chat.Send(node.Store, node.Repo, actor, "general",
		"Welcome! This is "+strings.TrimPrefix(actor, "human:")+"'s team channel. Invite a teammate from Settings, or connect an AI agent from the Agents page.")
	_, _, _ = chat.Send(node.Store, node.Repo, actor, "engineering",
		"Create channels for anything — design, ops, incidents. Agents can post here too via the send_message MCP tool.")
}

// DefaultStoreDir is where the wizard (and leetd init) put a store.
func DefaultStoreDir() string { return config.DefaultStoreDir() }

// --- wizard HTTP surface ----------------------------------------------------

func (d *Daemon) setupHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		wizardPage(w)
	})
	mux.HandleFunc("/setup/peers", func(w http.ResponseWriter, r *http.Request) {
		peers, err := leetNet.DiscoverPeers(1200 * time.Millisecond)
		type row struct{ NodeID, Addr, Role string }
		out := []row{}
		for _, p := range peers {
			if p.Role == "coordinator" {
				addr := p.Addr
				if p.EnrollPort > 0 {
					if host, _, err := net.SplitHostPort(p.Addr); err == nil {
						addr = net.JoinHostPort(host, fmt.Sprint(p.EnrollPort))
					}
				}
				out = append(out, row{p.NodeID, addr, p.Role})
			}
		}
		if err != nil {
			log.Printf("setup: peer discovery: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("/setup/start", d.setupAction("start"))
	mux.HandleFunc("/setup/join", d.setupAction("join"))
	mux.HandleFunc("/setup/local", d.setupAction("local"))
	return mux
}

// setupAction runs one wizard action and, on success, swaps this daemon into
// the created node so the user lands in the workspace with no restart.
func (d *Daemon) setupAction(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Actor       string `json:"actor"`
			Coordinator string `json:"coordinator"`
			Secret      string `json:"secret"`
			StoreDir    string `json:"store_dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		actor := "human:" + strings.TrimSpace(strings.TrimPrefix(req.Actor, "human:"))
		if actor == "human:" {
			http.Error(w, "your name is required", http.StatusBadRequest)
			return
		}
		storeDir := req.StoreDir
		if storeDir == "" {
			storeDir = DefaultStoreDir()
		}
		if kind == "join" && (req.Coordinator == "" || req.Secret == "") {
			http.Error(w, "coordinator and enrollment secret are required", http.StatusBadRequest)
			return
		}

		var secret string
		var err error
		switch kind {
		case "start":
			secret, err = CreateTeam(d.cfgPath, storeDir, actor)
		case "join":
			err = JoinTeam(d.cfgPath, storeDir, actor, req.Coordinator, req.Secret)
		case "local":
			err = CreateLocal(d.cfgPath, storeDir, actor)
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		if err != nil {
			log.Printf("setup %s: %v", kind, err)
			http.Error(w, friendlySetupError(err), http.StatusBadRequest)
			return
		}

		node, _, err := StartAtPath(d.cfgPath)
		if err != nil {
			http.Error(w, "created config but failed to start: "+err.Error(), http.StatusInternalServerError)
			return
		}
		seedWelcome(node, actor)
		if err := d.becomeNode(d.ctx, node); err != nil {
			http.Error(w, "created config but failed to start: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "redirect": "/", "enrollment_secret": secret, "role": kind,
		})
	}
}

func friendlySetupError(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "enrollment") || strings.Contains(msg, "secret"):
		return "Enrollment was rejected — check the invite code and try again."
	case strings.Contains(msg, "tls") || strings.Contains(msg, "certificate") ||
		strings.Contains(msg, "connection") || strings.Contains(msg, "refused"):
		return "Could not reach enrollment on that address. Use the coordinator's enrollment address (port 7443 by default — the coordinator's own UI shows it), not its sync port."
	}
	return err.Error()
}

// --- the wizard page ---------------------------------------------------------

func wizardPage(w http.ResponseWriter) {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Welcome — LeetOffice</title>
<style>
:root{--red:#d92a2a;--bg:#fff;--surface:#f8f5eb;--text:#111;--muted:#5b5b57;--faint:#8a8a85;
--line:rgba(17,17,17,.12);--line2:rgba(17,17,17,.24);--wash:rgba(17,17,17,.05);
--term:#141414;--termline:#2a2a2a;--termtext:#f8f5eb;
--sans:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
--mono:'JetBrains Mono',ui-monospace,'SF Mono',SFMono-Regular,Menlo,Consolas,monospace}
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:var(--sans);background:var(--bg);color:var(--text);font-size:15px;line-height:1.6;
max-width:680px;margin:0 auto;padding:64px 24px}
.brand{display:inline-flex;gap:10px;align-items:center;font-family:var(--mono);font-weight:700;
font-size:13px;letter-spacing:.18em;margin-bottom:26px}
.pulse{width:6px;height:6px;border-radius:50%;background:var(--red);animation:p 2s infinite}
@keyframes p{0%,100%{opacity:1}50%{opacity:.35}}
h1{font-size:38px;font-weight:700;letter-spacing:-.035em;line-height:1.08;margin-bottom:10px}
.sub{color:var(--muted);margin-bottom:30px}
.choices{display:grid;gap:12px;margin-bottom:28px}
.choice{border:1px solid var(--line2);padding:16px 18px;cursor:pointer;background:var(--bg)}
.choice:hover{background:var(--wash)}
.choice.sel{border-color:var(--text);background:var(--surface)}
.choice b{display:block;font-size:15px}
.choice span{color:var(--muted);font-size:13px}
form{display:none}
form.on{display:block}
label{display:block;font-family:var(--mono);font-size:10px;letter-spacing:.14em;
text-transform:uppercase;color:var(--faint);margin:16px 0 6px}
input{width:100%;padding:11px 13px;border:1px solid var(--line2);font:inherit;background:var(--bg)}
input:focus{outline:none;border-color:var(--text)}
button{margin-top:20px;padding:14px 26px;background:var(--text);color:#fff;border:0;
font-family:var(--mono);font-size:12px;font-weight:600;letter-spacing:.1em;text-transform:uppercase;cursor:pointer}
button:hover{background:var(--red)}
.err{color:var(--red);margin-top:12px;min-height:1.3em;font-size:13.5px}
.termcard{background:var(--term);color:var(--termtext);border:1px solid var(--termline);
font-family:var(--mono);font-size:1.15rem;letter-spacing:.1em;padding:16px 20px;margin-top:18px;
box-shadow:0 12px 40px rgba(17,17,17,.18)}
.hint{color:var(--faint);font-size:12.5px;margin-top:8px}
</style></head><body>
<h1>Welcome to LeetOffice</h1>
<p class="sub">Two questions and you're working. Everything stays on your machines.</p>

<div class="choices">
<div class="choice" data-form="start"><b>Start a new team</b><span>This machine keeps the shared store and invites others.</span></div>
<div class="choice" data-form="join"><b>Join a team on my network</b><span>You have an invite code from your coordinator.</span></div>
<div class="choice" data-form="local"><b>Just me, for now</b><span>A private workspace on this machine. You can join a team later.</span></div>
</div>

<form id="f-start">
<label>Your name</label><input name="actor" placeholder="josh" autocomplete="name">
<button type="submit">Create my team</button>
<p class="err"></p>
<p class="hint">The team invite code appears after setup — share it once, out of band.</p>
</form>

<form id="f-join">
<label>Your name</label><input name="actor" placeholder="maya" autocomplete="name">
<label>Team coordinator</label><input name="coordinator" placeholder="host:port (discovered automatically when possible)" list="peers">
<datalist id="peers"></datalist>
<label>Invite code</label><input name="secret" placeholder="one-time code from your coordinator">
<button type="submit">Join</button>
<p class="err"></p>
</form>

<form id="f-local">
<label>Your name</label><input name="actor" placeholder="sam" autocomplete="name">
<button type="submit">Start working</button>
<p class="err"></p>
</form>

<div class="secret" id="done" style="display:none"></div>

<script>
const forms={start:1,join:1,local:1};
document.querySelectorAll('.choice').forEach(c=>c.addEventListener('click',()=>{
 document.querySelectorAll('.choice').forEach(x=>x.classList.toggle('sel',x===c));
 document.querySelectorAll('form').forEach(f=>f.classList.toggle('on',f.id==='f-'+c.dataset.form));
}));
// offer discovered coordinators while the user decides
fetch('/setup/peers').then(r=>r.json()).then(peers=>{
 const dl=document.getElementById('peers');
 peers.forEach(p=>{const o=document.createElement('option');o.value=p.Addr;o.label=p.NodeID;dl.appendChild(o);});
}).catch(()=>{});
document.querySelectorAll('form').forEach(f=>f.addEventListener('submit',async e=>{
 e.preventDefault();
 const fd=new FormData(e.target);
 const body={actor:fd.get('actor')||'',coordinator:fd.get('coordinator')||'',secret:fd.get('secret')||''};
 const err=f.querySelector('.err'); err.textContent='';
 try{
  const res=await fetch('/setup/'+f.id.slice(2),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  if(!res.ok){err.textContent=await res.text();return;}
  const out=await res.json();
  if(out.enrollment_secret){
   const d=document.getElementById('done');
   d.style.display='block';
   d.textContent='Team invite code (share once): '+out.enrollment_secret+'  —  opening your workspace…';
  }
  setTimeout(()=>location.href=out.redirect,out.enrollment_secret?2500:300);
 }catch(ex){err.textContent=ex.message;}
}));
</script>
</body></html>`)
	_, _ = w.Write([]byte(b.String()))
}
