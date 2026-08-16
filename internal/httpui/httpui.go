// Package httpui is the human client served by the daemon over localhost
// (M17, D14): the bundled desktop app wraps this UI; any browser can render
// the store's tabbed HTML files as a read-only fallback. Edits here go through
// the store (embedded JSON) and land as attributed git commits.
package httpui

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"leetoffice/internal/config"
	"leetoffice/internal/store"
	leetSync "leetoffice/internal/sync"
)

// UI serves the editor. Writes are attributed to the node's human actor.
type UI struct {
	Store  *store.Store
	Repo   *leetSync.Repo
	Config *config.Config
}

// Handler builds the HTTP routes.
func (u *UI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", u.handleHome)
	mux.HandleFunc("/doc/new", u.handleNew)
	mux.HandleFunc("/doc/", u.handleDoc)
	mux.HandleFunc("/sync", u.handleSync)
	mux.HandleFunc("/memory", u.handleMemory)
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
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	docs, err := u.Store.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta charset=utf-8><title>LeetOffice</title>" + pageStyle + "</head><body><nav><b>LeetOffice</b>")
	fmt.Fprintf(&b, "<a href='/'>docs</a> <a href='/memory'>memory</a> ")
	fmt.Fprintf(&b, "<a href='#' onclick=\"fetch('/sync',{method:'POST'}).then(()=>location.reload());return false\">sync now</a></nav>")

	fmt.Fprintf(&b, "<p class=meta>node %s · role %s · store %s · actor %s</p>",
		html.EscapeString(u.Config.NodeID), u.Config.Role, html.EscapeString(u.Config.StoreDir), html.EscapeString(u.Config.Actor))

	b.WriteString("<table><tr><th>slug</th><th>type</th><th>title</th><th>updated</th><th>links</th></tr>")
	for _, d := range docs {
		fmt.Fprintf(&b, "<tr><td><a href='/doc/%s'>%s</a></td><td>%s</td><td>%s</td><td>%s</td><td>%d</td></tr>",
			d.Slug, html.EscapeString(d.Slug), d.Type, html.EscapeString(d.Title), d.Updated, linkTotal(d))
	}
	b.WriteString("</table>")

	b.WriteString(`<h3>New document</h3><form method="post" action="/doc/new">
<select name="type"><option>doc</option><option>task</option><option>contact</option><option>channel</option><option>company</option><option>email</option></select>
<input name="slug" placeholder="slug" required size=14>
<input name="title" placeholder="title" required size=32>
<textarea name="body" placeholder="first paragraph (optional)"></textarea>
<button>create</button></form>`)
	b.WriteString("</body></html>")
	io.WriteString(w, b.String())
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
	b.WriteString("<!DOCTYPE html><html><head><meta charset=utf-8><title>" + html.EscapeString(d.Title) + "</title>" + pageStyle + "</head><body>")
	fmt.Fprintf(&b, "<nav><b>LeetOffice</b><a href='/'>docs</a> <a href='/memory'>memory</a></nav>")
	fmt.Fprintf(&b, "<h1>%s</h1><p class=meta>%s · v%d · updated %s · last editor <code>%s</code></p>",
		html.EscapeString(d.Title), d.Type, d.Version, d.Updated, html.EscapeString(d.Audit.LastEditor))

	b.WriteString(`<form method="post">`)
	for i := range d.Blocks {
		blk := &d.Blocks[i]
		if _, flagged := blk.Meta["conflict"]; flagged {
			b.WriteString(`<blockquote class="conflict">⚠ conflicting edit — both versions retained (merge D6)</blockquote>`)
		}
		fmt.Fprintf(&b, "<textarea name='block_%s'>%s</textarea>", blk.ID, html.EscapeString(blk.Content))
	}
	b.WriteString(`<h3>Add block</h3>
<select name="new_type"><option>paragraph</option><option>heading</option><option>task-item</option><option>list-item</option><option>code</option><option>field</option><option>divider</option></select>
<textarea name="new_content" placeholder="new block content"></textarea>
<button>save</button></form>`)
	b.WriteString("</body></html>")
	io.WriteString(w, b.String())
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
	b.WriteString("<!DOCTYPE html><html><head><meta charset=utf-8><title>Memory</title>" + pageStyle + "</head><body>")
	b.WriteString("<nav><b>LeetOffice</b><a href='/'>docs</a> <a href='/memory'>memory</a></nav><h1>Team Memory</h1><pre style='white-space:pre-wrap'>")
	b.WriteString(html.EscapeString(string(raw)))
	b.WriteString("</pre></body></html>")
	io.WriteString(w, b.String())
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
