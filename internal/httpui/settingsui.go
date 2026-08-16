// GUI surfaces for git history and node settings: the audit trail rendered
// as a timeline (§7.1 as a page, not a command), and the control panel for
// identity, team invite codes, sync cadence, and the always-on service —
// everything an operator needed a terminal for.
package httpui

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"leetoffice/internal/store"
)

// handleAudit renders git history — the audit trail every change lands in
// (D3/§7.1): when, who, what, and the files touched, newest first.
func (u *UI) handleAudit(w http.ResponseWriter, r *http.Request) {
	actor := r.URL.Query().Get("actor")
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 500 {
		limit = n
	}
	entries, err := u.Repo.AuditLog("", time.Time{}, actor, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// distinct actors for the filter row
	all, _ := u.Repo.AuditLog("", time.Time{}, "", 500)
	seen := map[string]bool{}
	var actors []string
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Actor != "" && !seen[all[i].Actor] {
			seen[all[i].Actor] = true
			actors = append(actors, all[i].Actor)
		}
	}

	var b strings.Builder
	b.WriteString(`<div class="eyebrow"><span class="pulse"></span>FILE / AUDIT · GIT HISTORY</div>`)
	b.WriteString("<h1>History</h1>")
	b.WriteString(`<p class="meta">Every change to this workspace — attributed and timestamped in git. This is the audit trail (§7.1).</p>`)

	b.WriteString(`<div class="row" style="margin:14px 0">`)
	b.WriteString(`<a class="btn ghost" href="/audit">everyone</a>`)
	for _, a := range actors {
		mark := ""
		if a == actor {
			mark = ` style="background:var(--red);color:#fff;border-color:transparent"`
		}
		fmt.Fprintf(&b, `<a class="btn ghost" href="/audit?actor=%s"%s>%s</a>`, url.QueryEscape(a), mark, html.EscapeString(a))
	}
	b.WriteString(`</div>`)

	b.WriteString("<table><tr><th>when</th><th>who</th><th>change</th><th>files</th><th>commit</th></tr>")
	for _, e := range entries {
		fmt.Fprintf(&b, `<tr><td class="mono" style="white-space:nowrap;color:var(--muted)">%s</td><td class="mono">%s</td><td>%s</td>`,
			e.When.Local().Format("Jan 2 15:04"), html.EscapeString(e.Actor), html.EscapeString(strings.TrimSpace(e.Msg)))
		files := e.Files
		if len(files) > 3 {
			files = append(append([]string{}, files[:3]...), fmt.Sprintf("+%d more", len(files)-3))
		}
		fmt.Fprintf(&b, `<td class="mono" style="color:var(--muted);font-size:.78rem">%s</td>`, html.EscapeString(strings.Join(files, "<br>")))
		fmt.Fprintf(&b, `<td class="mono" style="color:var(--faint)">%.7s</td></tr>`, e.Commit)
	}
	if len(entries) == 0 {
		b.WriteString(`<tr><td colspan="5" class="muted">No history yet.</td></tr>`)
	}
	b.WriteString("</table>")
	_, _ = w.Write([]byte(xbPage("History", "audit", b.String())))
}

// handleSettings is the control panel: node identity, team invite code
// (coordinator), sync cadence, and the always-on service.
func (u *UI) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		u.saveSettings(w, r)
		return
	}
	var b strings.Builder
	b.WriteString(`<div class="eyebrow"><span class="pulse"></span>FILE / SETTINGS · NODE CONTROL</div>`)
	b.WriteString("<h1>Settings</h1>")

	// identity + node facts
	b.WriteString(`<div class="card surface"><h3 style="margin-top:0">This node</h3><dl class="dl">`)
	fmt.Fprintf(&b, `<dt>Node</dt><dd class="mono">%s</dd>`, html.EscapeString(u.Config.NodeID))
	fmt.Fprintf(&b, `<dt>Role</dt><dd class="mono">%s</dd>`, html.EscapeString(u.Config.Role))
	fmt.Fprintf(&b, `<dt>Store</dt><dd class="mono">%s</dd>`, html.EscapeString(u.Config.StoreDir))
	if u.Config.MainShare != "" {
		fmt.Fprintf(&b, `<dt>Team share</dt><dd class="mono">%s</dd>`, html.EscapeString(u.Config.MainShare))
	}
	if u.CfgPath != "" {
		fmt.Fprintf(&b, `<dt>Config</dt><dd class="mono">%s</dd>`, html.EscapeString(u.CfgPath))
	}
	b.WriteString(`</dl></div>`)

	// team invite (coordinator)
	if u.Config.IsCoordinator() {
		enrollAddr := "this-machine:7443"
		b.WriteString(`<div class="card"><h3 style="margin-top:0">Team invite</h3>
<p class="meta">Share the code out of band (it is also how a second machine joins: run LeetOffice there, choose <b>Join a team</b>).</p>`)
		fmt.Fprintf(&b, `<div class="termcard" style="margin:12px 0"><div class="tl">ENROLLMENT · %s</div><div style="font-size:1.6rem;letter-spacing:.12em">%s</div></div>`,
			html.EscapeString(enrollAddr), html.EscapeString(u.Config.EnrollmentSecret))
		b.WriteString(`<form method="post" action="/settings/invite" class="row"><button class="ghost">regenerate invite code</button>
<span class="meta">invalidates the old code — already-enrolled nodes are unaffected (their certificates still work)</span></form>`)
		b.WriteString(`</div>`)
	}

	// editable identity + cadence
	b.WriteString(`<div class="card"><h3 style="margin-top:0">Identity &amp; sync</h3>
<form method="post" action="/settings">`)
	fmt.Fprintf(&b, `<label class="field"><span>Your name (attribution on every change)</span>
<input type="text" name="actor" value="%s" required></label>`, html.EscapeString(strings.TrimPrefix(u.Config.Actor, "human:")))
	fmt.Fprintf(&b, `<label class="field"><span>Sync every (seconds)</span>
<input type="text" name="sync_every_sec" value="%d" inputmode="numeric"></label>`, u.Config.SyncEverySec)
	fmt.Fprintf(&b, `<label class="field"><span>Ollama URL (semantic search; leave as-is for keyword search)</span>
<input type="text" name="ollama_base" value="%s"></label>`, html.EscapeString(u.Config.Ollama.BaseURL))
	fmt.Fprintf(&b, `<label class="field"><span>Embedding model</span>
<input type="text" name="ollama_model" value="%s"></label>`, html.EscapeString(u.Config.Ollama.Model))
	b.WriteString(`<div class="row"><button>save settings</button><span class="meta" id="saved"></span></div></form></div>`)

	// always-on service
	b.WriteString(`<div class="card"><h3 style="margin-top:0">Always-on</h3>
<p class="meta">Register LeetOffice as a login service so it starts automatically and keeps running (launchd on macOS, systemd on Linux).</p>
<div class="row">
<form method="post" action="/service/install"><button>make always-on</button></form>
<form method="post" action="/service/uninstall"><button class="ghost">undo</button></form>
<form method="post" action="/sync"><button class="ghost">sync now</button></form>
</div></div>`)

	_, _ = w.Write([]byte(xbPage("Settings", "settings", b.String())))
}

func (u *UI) saveSettings(w http.ResponseWriter, r *http.Request) {
	if u.CfgPath == "" {
		http.Error(w, "settings cannot be saved in this context", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("actor"))
	name = strings.TrimPrefix(name, "human:")
	if name != "" {
		u.Config.Actor = "human:" + name
	}
	if n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("sync_every_sec"))); err == nil && n >= 1 && n <= 600 {
		u.Config.SyncEverySec = n
	}
	if v := strings.TrimSpace(r.FormValue("ollama_base")); v != "" {
		u.Config.Ollama.BaseURL = v
	}
	if v := strings.TrimSpace(r.FormValue("ollama_model")); v != "" {
		u.Config.Ollama.Model = v
	}
	if err := u.Config.Save(u.CfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// handleInviteRegen rotates the enrollment secret (coordinator).
func (u *UI) handleInviteRegen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !u.Config.IsCoordinator() {
		http.Error(w, "only the coordinator issues invites", http.StatusForbidden)
		return
	}
	if u.CfgPath == "" {
		http.Error(w, "invite cannot rotate in this context", http.StatusBadRequest)
		return
	}
	u.Config.EnrollmentSecret = store.NewID()[:12]
	if err := u.Config.Save(u.CfgPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
