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

	"leetoffice/internal/config"
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
func ListenAndServe(ctx context.Context, cfgPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	d := &Daemon{cfgPath: cfgPath, ctx: ctx}
	addr := config.Default("", "").Listen.HTTP

	if _, err := os.Stat(cfgPath); err == nil {
		node, cfg, err := StartAtPath(cfgPath)
		if err != nil {
			return fmt.Errorf("start node from %s: %w", cfgPath, err)
		}
		addr = cfg.Listen.HTTP
		d.becomeNode(ctx, node)
	} else {
		d.mu.Lock()
		d.handler = d.setupHandler()
		d.mu.Unlock()
		log.Printf("setup: no config at %s — first-run wizard on http://%s", cfgPath, addr)
	}

	httpSrv := &http.Server{Addr: addr, Handler: d}
	go func() {
		log.Printf("http: listening on %s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case <-stop:
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutCtx)
}

// becomeNode swaps the daemon's handler to the live node and starts its loops.
func (d *Daemon) becomeNode(ctx context.Context, node *Node) {
	if err := node.StartLoops(ctx); err != nil {
		log.Printf("node loops: %v (continuing in degraded mode)", err)
	}
	d.mu.Lock()
	d.handler = node.ServeHTTP()
	d.mu.Unlock()
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
	return filepath.Join(parent, "main-share.git"), filepath.Join(parent, ".leetoffice-identity")
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
	id, gitAddr, err := leetNet.Enroll(coordinator, cfg.NodeID, secret, "")
	if err != nil {
		return fmt.Errorf("enrollment rejected: %w", err)
	}
	if err := id.Save(cfg.IdentityDir); err != nil {
		return err
	}
	// The share URL must point at the GIT service, not the enrollment port
	// we just talked to — the coordinator tells us where git lives, and we
	// only fall back to the default port for older coordinators.
	if gitAddr == "" {
		host, _, splitErr := net.SplitHostPort(coordinator)
		if splitErr != nil {
			host = coordinator
		}
		gitAddr = net.JoinHostPort(host, fmt.Sprint(leetNet.DefaultPort))
	}
	cfg.MainShare = fmt.Sprintf("%s://%s/main.git", leetNet.Scheme, gitAddr)
	return cfg.Save(cfgPath)
}

// CreateLocal makes a single-player node: no share, no network role.
func CreateLocal(cfgPath, storeDir, actor string) error {
	cfg := config.Default(storeDir, actor)
	_, identity := wizardPaths(storeDir)
	cfg.IdentityDir = identity
	return cfg.Save(cfgPath)
}

// DefaultStoreDir is where the wizard puts a store by default.
func DefaultStoreDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		wd, _ := os.Getwd()
		return filepath.Join(wd, "LeetOffice")
	}
	return filepath.Join(home, "LeetOffice")
}

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
		d.becomeNode(d.ctx, node)

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
body{font-family:-apple-system,system-ui,sans-serif;max-width:720px;margin:3rem auto;padding:0 1.25rem;color:#1a1a1a}
h1{font-size:1.9rem;margin-bottom:.4rem}
.sub{color:#666;margin-bottom:2rem}
.choices{display:grid;gap:.8rem;margin-bottom:2rem}
.choice{border:2px solid #ddd;border-radius:10px;padding:1rem 1.2rem;cursor:pointer;background:#fff}
.choice:hover,.choice.sel{border-color:#1a1a1a}
.choice b{display:block;font-size:1.05rem}
.choice span{color:#666;font-size:.88rem}
form{display:none}
form.on{display:block}
label{display:block;font-weight:600;margin:.9rem 0 .3rem}
input{width:100%;padding:.6rem .7rem;border:1px solid #ccc;border-radius:8px;font:inherit;box-sizing:border-box}
button{margin-top:1.4rem;padding:.65rem 1.4rem;border-radius:8px;border:0;background:#1a1a1a;color:#fff;font:inherit;font-weight:600;cursor:pointer}
.err{color:#c0392b;margin-top:.8rem;min-height:1.2rem}
.secret{margin-top:1rem;padding:.8rem 1rem;background:#f4f9f4;border:1px solid #b7dfb7;border-radius:8px;font-family:ui-monospace,Menlo,monospace;font-size:1.05rem}
.hint{color:#666;font-size:.85rem;margin-top:.3rem}
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
