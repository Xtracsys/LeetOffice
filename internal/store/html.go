package store

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// RenderDoc produces the tabbed HTML file for a document (D1). The canonical
// JSON payload is embedded in a <script id="leet-doc"> element; the visible
// tabs (Content, Links) are a pure rendering of that JSON.
func RenderDoc(d *Doc) ([]byte, error) {
	payload, err := d.Bytes()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<title>" + html.EscapeString(d.Title) + "</title>\n")
	b.WriteString(`<style>
body{font-family:-apple-system,system-ui,sans-serif;max-width:820px;margin:2rem auto;padding:0 1rem;color:#1a1a1a}
.tabs{display:flex;gap:.5rem;margin-bottom:1rem}
.tab{padding:.4rem .9rem;border:1px solid #ccc;border-radius:6px 6px 0 0;cursor:default}
.tab.active{background:#1a1a1a;color:#fff}
.panel{display:none}.panel.active{display:block}
h2{border-bottom:1px solid #eee;padding-bottom:.3rem}
code,pre{background:#f4f4f4}
pre{padding:.6rem;border-radius:6px;overflow-x:auto}
.field{display:grid;grid-template-columns:12rem 1fr;gap:.4rem}
.field dt{font-weight:600}
.conflict{border-left:4px solid #c0392b;padding-left:.8rem;background:#fdf0ee}
blockquote.conflict-note{color:#c0392b}
a.blocklink{text-decoration:none;border-bottom:1px dotted #06c}
.meta{color:#666;font-size:.85rem}
</style>
</head>
<body>
`)
	fmt.Fprintf(&b, "<h1>%s</h1>\n<p class=\"meta\">%s · %s · v%d · updated %s", html.EscapeString(d.Title), d.Type, html.EscapeString(d.Slug), d.Version, d.Updated)
	if len(d.Tags) > 0 {
		b.WriteString(" · " + html.EscapeString(strings.Join(d.Tags, ", ")))
	}
	b.WriteString("</p>\n")

	b.WriteString(`<div class="tabs"><div class="tab active" data-tab="content">Content</div><div class="tab" data-tab="links">Links</div></div>
<div class="panel active" id="panel-content">
`)
	for _, blk := range d.Blocks {
		writeBlockHTML(&b, &blk)
	}
	b.WriteString("\n</div>\n<div class=\"panel\" id=\"panel-links\">\n<h2>Links</h2>\n")
	if linkCount(d) == 0 {
		b.WriteString("<p class=\"meta\">No links.</p>\n")
	} else {
		b.WriteString("<ul>\n")
		for i := range d.Blocks {
			for _, l := range d.Blocks[i].Links {
				dirArrow := "→"
				if l.Dir == "in" {
					dirArrow = "←"
				}
				label := html.EscapeString(l.Label)
				if label == "" {
					label = "link"
				}
				fmt.Fprintf(&b, "<li><code>%s</code> %s doc <code>%s</code> block <code>%s</code></li>\n",
					label, dirArrow, html.EscapeString(shortID(l.TargetDoc)), html.EscapeString(shortID(l.TargetBlock)))
			}
		}
		b.WriteString("</ul>\n")
	}
	b.WriteString("</div>\n")

	b.WriteString(`<script>
document.querySelectorAll('.tab').forEach(t=>t.addEventListener('click',()=>{
 document.querySelectorAll('.tab').forEach(x=>x.classList.toggle('active',x===t));
 document.querySelectorAll('.panel').forEach(p=>p.classList.toggle('active',p.id==='panel-'+t.dataset.tab));
}));
</script>
`)
	// The embedded JSON is the single source of truth; it must be the last
	// element so extraction is unambiguous.
	b.WriteString(`<script type="application/json" id="leet-doc">`)
	b.Write(payload) //nolint — MarshalIndent output has no "</script>" risk from json escaping
	b.WriteString("</script>\n</body>\n</html>\n")
	return []byte(b.String()), nil
}

func writeBlockHTML(b *strings.Builder, blk *Block) {
	content := html.EscapeString(blk.Content)
	if _, ok := blk.Meta["conflict"].(bool); ok && blk.Meta["conflict"] == true {
		b.WriteString(`<div class="conflict"><blockquote class="conflict-note">conflicting edit — both versions retained</blockquote>`)
		defer b.WriteString("</div>")
	}
	switch blk.Type {
	case BlockHeading:
		fmt.Fprintf(b, "<h%d>%s</h%d>\n", headingLevel(blk.Level), content, headingLevel(blk.Level))
	case BlockCode:
		fmt.Fprintf(b, "<pre><code>%s</code></pre>\n", content)
	case BlockTaskItem:
		if blk.Meta["done"] == true {
			fmt.Fprintf(b, "<p><input type=\"checkbox\" checked disabled> %s</p>\n", content)
		} else {
			fmt.Fprintf(b, "<p><input type=\"checkbox\" disabled> %s</p>\n", content)
		}
	case BlockListItem:
		fmt.Fprintf(b, "<p>• %s</p>\n", content)
	case BlockField:
		name, _ := blk.Meta["name"].(string)
		fmt.Fprintf(b, "<div class=\"field\"><dt>%s</dt><dd>%s</dd></div>\n", html.EscapeString(name), content)
	case BlockDivider:
		b.WriteString("<hr>\n")
	default:
		fmt.Fprintf(b, "<p>%s</p>\n", content)
	}
}

func headingLevel(level int) int {
	if level < 1 || level > 6 {
		return 2
	}
	return level
}

func linkCount(d *Doc) int {
	n := 0
	for i := range d.Blocks {
		n += len(d.Blocks[i].Links)
	}
	return n
}

func shortID(id string) string {
	if len(id) <= 10 {
		return id
	}
	return id[:10] + "…"
}

var (
	scriptRe   = regexp.MustCompile(`(?s)<script type="application/json" id="leet-doc">(.*?)</script>`)
	commentRe  = regexp.MustCompile(`(?s)<!--.*?-->`)
	nonSpaceRe = regexp.MustCompile(`\S`)
)

// ExtractDoc parses the canonical JSON payload out of a rendered HTML file.
func ExtractDoc(page []byte) (*Doc, error) {
	page = commentRe.ReplaceAll(page, nil)
	m := scriptRe.FindSubmatch(page)
	if m == nil {
		return nil, errors.New("no embedded leet-doc JSON found")
	}
	payload := bytes.TrimSpace(m[1])
	if !nonSpaceRe.Match(payload) {
		return nil, errors.New("empty embedded leet-doc JSON")
	}
	return ParseDoc(payload)
}
