// GUI surfaces for git history and node settings: the audit trail rendered
// as a timeline (§7.1 as a page, not a command), and the control panel for
// identity, team invite codes, sync cadence, and the always-on service —
// everything an operator needed a terminal for.
package httpui

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"leetoffice/internal/buildinfo"
	"leetoffice/internal/chat"
	leetNet "leetoffice/internal/net"
	"leetoffice/internal/store"
	"leetoffice/internal/update"
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
	_, _ = w.Write([]byte(xbPageActor("History", "audit", b.String(), u.Config.Actor)))
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
	fmt.Fprintf(&b, `<dt>Version</dt><dd class="mono">%s</dd>`, html.EscapeString(buildinfo.Full()))
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

	u.writeUpdateCard(&b)

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

	u.writeTeamRoster(&b)

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

	_, _ = w.Write([]byte(xbPageActor("Settings", "settings", b.String(), u.Config.Actor)))
}

func (u *UI) writeTeamRoster(b *strings.Builder) {
	b.WriteString(`<div class="card"><h3 style="margin-top:0">Team members</h3>`)
	b.WriteString(`<p class="meta">There is no pending-join queue. Anyone with the current invite code is issued a certificate immediately. Already-enrolled nodes keep their certs if you regenerate the code.</p>`)

	b.WriteString(`<h3>Pending</h3>
<p class="meta">None — joins are not held for approval.</p>`)

	online := map[string]bool{}
	for _, id := range onlineNodes() {
		online[id] = true
	}
	issued := leetNet.ListIssued(filepath.Join(u.Config.IdentityDir, leetNet.IssuedDir))
	recent := chat.RecentActors(u.Repo, 24*time.Hour)

	type row struct{ Name, Kind, Status string }
	seen := map[string]bool{}
	var rows []row
	add := func(name, kind, status string) {
		key := name + "|" + kind
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		rows = append(rows, row{name, kind, status})
	}
	for _, m := range issued {
		st := "accepted"
		if online[m.NodeID] {
			st = "online"
		}
		add(m.NodeID, "node", st)
	}
	for id := range online {
		add(id, "node", "online")
	}
	for _, a := range recent {
		st := "recent"
		if strings.HasPrefix(a, "agent:") {
			add(a, "agent", st)
			continue
		}
		add(a, "actor", st)
	}
	add(u.Config.NodeID, "node", "this node")
	add(u.Config.Actor, "actor", "you")

	b.WriteString(`<h3>Accepted</h3>
<table><tr><th>who</th><th>kind</th><th>status</th></tr>`)
	if len(rows) == 0 {
		b.WriteString(`<tr><td colspan="3" class="muted">No members yet — share the invite code.</td></tr>`)
	}
	for _, r := range rows {
		fmt.Fprintf(b, `<tr><td class="mono">%s</td><td class="mono">%s</td><td>%s</td></tr>`,
			html.EscapeString(r.Name), html.EscapeString(r.Kind), html.EscapeString(r.Status))
	}
	b.WriteString(`</table></div>`)
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
	oldEvery := u.Config.SyncEverySec
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
	if u.RescheduleSync != nil && u.Config.SyncEverySec != oldEvery {
		u.RescheduleSync()
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
	if u.RotateEnrollment != nil {
		u.RotateEnrollment(u.Config.EnrollmentSecret)
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (u *UI) writeUpdateCard(b *strings.Builder) {
	b.WriteString(`<div class="card" id="update"><h3 style="margin-top:0">Updates</h3>
<p class="meta">GitHub is contacted only when you click <b>check for update</b> or <b>install</b>. The daemon never phones home on its own.</p>`)

	u.updateMu.Lock()
	last := u.lastCheck
	applied := u.lastApply
	err := u.updateErr
	u.updateMu.Unlock()

	if err != nil {
		fmt.Fprintf(b, `<p class="meta red">%s</p>`, html.EscapeString(err.Error()))
	}
	if applied != nil {
		fmt.Fprintf(b, `<p class="meta">Installed <span class="mono">%s</span> at <span class="mono">%s</span>. Restart LeetOffice to run it (if always-on: undo then make always-on, or reboot).</p>`,
			html.EscapeString(applied.Version), html.EscapeString(applied.Path))
	}
	if last != nil && applied == nil {
		if last.Newer {
			fmt.Fprintf(b, `<p class="meta"><span class="mono">%s</span> is available (this build is <span class="mono">%s</span>). Install verifies the SHA-256 checksum, then replaces this binary.</p>`,
				html.EscapeString(last.Latest), html.EscapeString(last.Current))
		} else {
			fmt.Fprintf(b, `<p class="meta">You're on the latest release (<span class="mono">%s</span>).</p>`,
				html.EscapeString(last.Latest))
		}
	}

	b.WriteString(`<div class="row">
<form method="post" action="/settings/update/check"><button class="ghost">check for update</button></form>`)
	if last != nil && last.Newer && applied == nil {
		b.WriteString(`<form method="post" action="/settings/update/apply"><button>install `)
		b.WriteString(html.EscapeString(last.Latest))
		b.WriteString(`</button></form>`)
	}
	b.WriteString(`</div></div>`)
}

func (u *UI) updater() *update.Client {
	if u.Updater != nil {
		return u.Updater
	}
	return update.Default()
}

func (u *UI) updateDest() string {
	if u.BinaryPath != "" {
		return u.BinaryPath
	}
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	return p
}

func (u *UI) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	res, err := u.updater().Check(ctx, buildinfo.Version)
	u.updateMu.Lock()
	u.lastApply = nil
	if err != nil {
		u.updateErr = err
		u.lastCheck = nil
	} else {
		u.updateErr = nil
		u.lastCheck = res
	}
	u.updateMu.Unlock()
	http.Redirect(w, r, "/settings#update", http.StatusSeeOther)
}

func (u *UI) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	c := u.updater()
	u.updateMu.Lock()
	var rel *update.Release
	if u.lastCheck != nil {
		rel = u.lastCheck.Release
	}
	u.updateMu.Unlock()

	check, err := c.Check(ctx, buildinfo.Version)
	if err != nil {
		u.updateMu.Lock()
		u.updateErr = err
		u.lastApply = nil
		u.updateMu.Unlock()
		http.Redirect(w, r, "/settings#update", http.StatusSeeOther)
		return
	}
	if !check.Newer {
		u.updateMu.Lock()
		u.updateErr = nil
		u.lastCheck = check
		u.lastApply = nil
		u.updateMu.Unlock()
		http.Redirect(w, r, "/settings#update", http.StatusSeeOther)
		return
	}
	if rel == nil || rel.Tag != check.Latest {
		rel = check.Release
	}
	applied, err := c.Apply(ctx, u.updateDest(), rel)
	u.updateMu.Lock()
	u.lastCheck = check
	if err != nil {
		u.updateErr = err
		u.lastApply = nil
	} else {
		u.updateErr = nil
		u.lastApply = applied
	}
	u.updateMu.Unlock()
	http.Redirect(w, r, "/settings#update", http.StatusSeeOther)
}
