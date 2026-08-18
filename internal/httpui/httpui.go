// Package httpui is the human client served by the daemon over localhost
// (M17, D14): the bundled desktop app wraps this UI; any browser can render
// the store's tabbed HTML files as a read-only fallback. Edits here go through
// the store (embedded JSON) and land as attributed git commits.
package httpui

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
	"leetoffice/internal/update"
)

// UI serves the editor. Writes are attributed to the node's human actor.
type UI struct {
	Store  *store.Store
	Repo   *leetSync.Repo
	Config *config.Config
	// BinaryPath/CfgPath fill in the agent-connection snippet with real
	// locations (daemon sets them; tests can stub).
	BinaryPath string
	CfgPath    string
	// RotateEnrollment swaps the live EnrollmentServer secret after
	// handleInviteRegen persists the new code (D8). Nil when enrollment
	// is not running (tests, clients).
	RotateEnrollment func(secret string)
	// RescheduleSync restarts the daemon's sync ticker after saveSettings
	// writes a new sync_every_sec. Nil when the UI is not wired to a live
	// node (tests).
	RescheduleSync func()
	// Updater talks to GitHub Releases. Nil means update.Default()
	// (production). Tests inject an httptest client. Never called except
	// from POST /settings/update/{check,apply} (P1).
	Updater *update.Client

	updateMu  sync.Mutex
	lastCheck *update.CheckResult
	lastApply *update.ApplyResult
	updateErr error
}

// Handler builds the HTTP routes.
func (u *UI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", u.handleChat)
	mux.HandleFunc("/docs", u.handleHome)
	mux.HandleFunc("/doc/new", u.handleNew)
	mux.HandleFunc("/doc/", u.handleDoc)
	mux.HandleFunc("/sync", u.handleSync)
	mux.HandleFunc("/memory", u.handleMemory)
	mux.HandleFunc("/agents", u.handleAgents)
	mux.HandleFunc("/api/state", u.handleAPIState)
	mux.HandleFunc("/api/send", u.handleAPISend)
	mux.HandleFunc("/api/agent/inbox", u.handleAgentInbox)
	mux.HandleFunc("/audit", u.handleAudit)
	mux.HandleFunc("/settings", u.handleSettings)
	mux.HandleFunc("/settings/invite", u.handleInviteRegen)
	mux.HandleFunc("/settings/update/check", u.handleUpdateCheck)
	mux.HandleFunc("/settings/update/apply", u.handleUpdateApply)
	mux.HandleFunc("/settings/hide", u.handleHideActor)
	mux.HandleFunc("/settings/unhide", u.handleUnhideActor)
	return mux
}

const pageStyle = `<style>
body{font-family:-apple-system,system-ui,sans-serif;max-width:860px;margin:2rem auto;padding:0 1rem;color:#1a1a1a}
a{color:#06c}nav{display:flex;gap:1rem;margin-bottom:1.5rem;align-items:center}
nav b{margin-right:auto}
table{border-collapse:collapse;width:100%}td,th{padding:.35rem .5rem;border-bottom:1px solid #eee;text-align:left}
textarea{width:100%;min-height:3.5rem;font:inherit;padding:.4rem;margin-bottom:.6rem;border:1px solid #ccc;border-radius:6px}
button{padding:.4rem 1rem;border-radius:6px;border:1px solid #888;background:#1a1a1a;color:#fff;cursor:pointer}
select,input[type=text]{padding:.35rem;border:1px solid #ccc;border-radius:6px;font:inherit}
.meta{color:#666;font-size:.85rem}.conflict{color:#c0392b}
blockquote{border-left:3px solid #ccc;margin:0;padding:.2rem .8rem;color:#444}
code{background:#f4f4f4;padding:0 .3rem;border-radius:4px}
</style>`

func (u *UI) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/docs" {
		http.NotFound(w, r)
		return
	}
	docs, err := u.Store.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var b strings.Builder
	b.WriteString(`<div class="eyebrow"><span class="pulse"></span>FILE / DOCS · SHARED WORKSPACE</div><h1>Docs</h1>`)
	fmt.Fprintf(&b, "<p class=meta>node %s · role %s · store %s · actor %s · <a href='/settings'>settings</a></p>",
		html.EscapeString(u.Config.NodeID), u.Config.Role, html.EscapeString(u.Config.StoreDir), html.EscapeString(u.Config.Actor))

	b.WriteString("<table><tr><th>slug</th><th>type</th><th>title</th><th>updated</th><th>links</th></tr>")
	for _, d := range docs {
		fmt.Fprintf(&b, "<tr><td><a href='/doc/%s'>%s</a></td><td>%s</td><td>%s</td><td>%s</td><td>%d</td></tr>",
			d.Slug, html.EscapeString(d.Slug), d.Type, html.EscapeString(d.Title), d.Updated, linkTotal(d))
	}
	b.WriteString("</table>")

	b.WriteString(`<div class="card"><h3 style="margin-top:0">New document</h3><form method="post" action="/doc/new">
<select name="type"><option>doc</option><option>task</option><option>contact</option><option>channel</option><option>company</option><option>email</option></select>
<input name="slug" placeholder="slug" required size=14>
<input name="title" placeholder="title" required size=32>
<textarea name="body" placeholder="first paragraph (optional)"></textarea>
<div class="row" style="margin-top:12px"><button>create</button></div></form></div>`)
	_, _ = w.Write([]byte(xbPageActor("Docs", "docs", b.String(), u.Config.Actor)))
}

func linkTotal(d *store.Doc) int {
	n := 0
	for i := range d.Blocks {
		n += len(d.Blocks[i].Links)
	}
	return n
}

func (u *UI) handleNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	slug := slugify(r.FormValue("slug"))
	title := r.FormValue("title")
	if slug == "" || title == "" {
		http.Error(w, "slug and title required", 400)
		return
	}
	if _, err := u.Store.Load(slug); err == nil {
		http.Error(w, "a document with that slug already exists", 409)
		return
	}
	d := store.NewDoc(store.DocType(r.FormValue("type")), slug, title)
	if body := r.FormValue("body"); body != "" {
		d.AddParagraph(body)
	}
	if err := u.Store.Save(d, u.Config.Actor); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if _, err := u.Repo.CommitAll(u.Config.Actor, "ui: create "+slug); err != nil && err != leetSync.ErrNoChanges {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/doc/"+slug, http.StatusSeeOther)
}

func (u *UI) handleDoc(w http.ResponseWriter, r *http.Request) {
	slug := path.Base(r.URL.Path)
	if strings.Contains(slug, "/") || slug == "" {
		http.NotFound(w, r)
		return
	}
	d, err := u.Store.Load(slug)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if r.Method == http.MethodPost {
		u.saveEdits(w, r, d)
		return
	}
	u.renderDoc(w, d)
}

func (u *UI) renderDoc(w http.ResponseWriter, d *store.Doc) {
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="eyebrow"><span class="pulse"></span>FILE / DOC · %s</div>`, html.EscapeString(d.Slug))
	fmt.Fprintf(&b, "<h1>%s</h1><p class=meta>%s · v%d · updated %s · last editor <code>%s</code> · <a href='/audit'>history</a></p>",
		html.EscapeString(d.Title), d.Type, d.Version, d.Updated, html.EscapeString(d.Audit.LastEditor))

	b.WriteString(`<div class="card"><form method="post">`)
	for i := range d.Blocks {
		blk := &d.Blocks[i]
		if _, flagged := blk.Meta["conflict"]; flagged {
			b.WriteString(`<blockquote class="conflict">⚠ conflicting edit — both versions retained (merge D6)</blockquote>`)
		}
		fmt.Fprintf(&b, "<div class='field'><span class='mono muted' style='font-size:10px'>%s</span><textarea name='block_%s'>%s</textarea></div>",
			blk.Type, blk.ID, html.EscapeString(blk.Content))
	}
	b.WriteString(`<h3>Add block</h3>
<select name="new_type"><option>paragraph</option><option>heading</option><option>task-item</option><option>list-item</option><option>code</option><option>field</option><option>divider</option></select>
<textarea name="new_content" placeholder="new block content"></textarea>
<div class="row" style="margin-top:12px"><button>save</button></div></form></div>`)
	_, _ = w.Write([]byte(xbPageActor(d.Title, "docs", b.String(), u.Config.Actor)))
}

func (u *UI) saveEdits(w http.ResponseWriter, r *http.Request, d *store.Doc) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	changed := false
	for i := range d.Blocks {
		if v, ok := r.Form["block_"+d.Blocks[i].ID]; ok && v[0] != d.Blocks[i].Content {
			d.Blocks[i].Content = v[0]
			changed = true
		}
	}
	if nc := r.FormValue("new_content"); strings.TrimSpace(nc) != "" {
		d.AddBlock(store.Block{Type: store.BlockType(r.FormValue("new_type")), Content: nc})
		changed = true
	}
	if changed {
		if err := u.Store.Save(d, u.Config.Actor); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if _, err := u.Repo.CommitAll(u.Config.Actor, "ui: edit "+d.Slug); err != nil && err != leetSync.ErrNoChanges {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, "/doc/"+d.Slug, http.StatusSeeOther)
}

func (u *UI) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	if u.Config.MainShare == "" {
		http.Error(w, "no main share configured", 400)
		return
	}
	res, err := u.Repo.Sync("origin", u.Config.Actor)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintf(w, "synced: pulled=%v pushed=%v merged=%v conflicts=%d", res.Pulled, res.Pushed, res.Merged, len(res.Conflicts))
}

func (u *UI) handleMemory(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(u.Store.Root + "/MEMORY.md")
	if err != nil {
		raw = []byte("MEMORY.md not synthesized yet.")
	}
	var b strings.Builder
	b.WriteString(`<div class="eyebrow"><span class="pulse"></span>FILE / MEMORY · TEAM CONTEXT</div><h1>Team Memory</h1>`)
	b.WriteString("<pre style='white-space:pre-wrap;margin-top:18px'>")
	b.WriteString(html.EscapeString(string(raw)))
	b.WriteString("</pre>")
	_, _ = w.Write([]byte(xbPageActor("Memory", "memory", b.String(), u.Config.Actor)))
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var out []rune
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		default:
			if !lastDash && len(out) > 0 {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}

// handleAgents is the "Connect an agent" page: the exact MCP configuration
// for the common AI clients, ready to copy, plus the HTTP endpoint.
func (u *UI) handleAgents(w http.ResponseWriter, r *http.Request) {
	bin := u.BinaryPath
	if bin == "" {
		bin = "leetd"
	}
	cfgFlag := ""
	if u.CfgPath != "" {
		cfgFlag = `","--config","` + u.CfgPath
	}
	snippet := fmt.Sprintf(`{
  "mcpServers": {
    "leetoffice": {
      "command": %q,
      "args": ["mcp%s", "--actor", "agent:NAME-YOUR-AGENT"]
    }
  }
}`, bin, cfgFlag)

	var b strings.Builder
	b.WriteString(`<div class="eyebrow"><span class="pulse"></span>FILE / AGENTS · MCP ACCESS</div>`)
	b.WriteString("<h1>Connect an agent</h1>")
	b.WriteString("<p class=meta>Any MCP-capable client — Claude Code, Codex, Hermes, Cursor — can work in this store. Every write is attributed to the actor you name here.</p>")

	b.WriteString(`<div class="card"><h3 style="margin-top:0">Stdio (Claude Code, Codex, most clients)</h3>`)
	b.WriteString(`<p>Add this to your client's MCP configuration (<code>claude mcp add</code>, <code>.mcp.json</code>, or your client's settings):</p>`)
	fmt.Fprintf(&b, `<pre id="snippet">%s</pre>`, html.EscapeString(snippet))
	b.WriteString(`<p><button onclick="navigator.clipboard.writeText(document.getElementById('snippet').textContent).then(()=>this.textContent='copied ✓')">copy configuration</button></p>`)

	b.WriteString(`</div><div class="card"><h3 style="margin-top:0">Inbox</h3>
<p class="meta">Agents call <code>subscribe</code> then poll <code>inbox</code> for <code>@agent:&lt;id&gt;</code> mentions. Unaddressed chat is not delivered. <code>mark_read</code> stores the cursor in this node's config.</p>`)
	fmt.Fprintf(&b, `<p class="meta">HTTP: <code>GET http://%s/api/agent/inbox?actor=agent:hermes</code></p>`, html.EscapeString(u.Config.Listen.HTTP))

	b.WriteString(`</div><div class="card"><h3 style="margin-top:0">HTTP</h3>`)
	fmt.Fprintf(&b, "<p>Point an MCP HTTP client at <code>http://%s/mcp</code>.</p>", u.Config.Listen.HTTP)

	b.WriteString(`</div><div class="card"><h3 style="margin-top:0">CLI (no MCP)</h3>`)
	b.WriteString("<p>Agents or scripts can also drive the store directly: <code>leetd doc</code>, <code>leetd audit</code>, and the store-backed MCP via <code>leetd mcp</code>.</p></div>")

	_, _ = w.Write([]byte(xbPageActor("Agents", "agents", b.String(), u.Config.Actor)))
}
