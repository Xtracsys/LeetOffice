// The XtracBox design system (from xtracbox.com's own CSS), applied to every
// LeetOffice interface: paper background, warm surface, near-black ink, one
// signal red, JetBrains Mono for labels (system fallbacks — the product never
// fetches remote fonts, P1 no-egress), hairline borders, and the dark
// terminal card as the signature element.
package httpui

import "html"

const xbCSS = `
:root{
 --red:#d92a2a; --bg:#fff; --surface:#f8f5eb; --text:#111; --muted:#5b5b57; --faint:#8a8a85;
 --line:rgba(17,17,17,.12); --line2:rgba(17,17,17,.24); --wash:rgba(17,17,17,.05);
 --term:#141414; --termline:#2a2a2a; --termtext:#f8f5eb;
 --sans:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;
 --mono:'JetBrains Mono',ui-monospace,'SF Mono',SFMono-Regular,Menlo,Consolas,monospace;
}
*{box-sizing:border-box;margin:0;padding:0}
html{scroll-padding-top:64px}
body{background:var(--bg);color:var(--text);font-family:var(--sans);font-size:15px;line-height:1.6;-webkit-font-smoothing:antialiased}
header.topnav{position:sticky;top:0;z-index:100;background:rgba(255,255,255,.86);
 -webkit-backdrop-filter:blur(12px);backdrop-filter:blur(12px);border-bottom:1px solid var(--line)}
.navwrap{max-width:1080px;margin:0 auto;padding:0 24px;height:56px;display:flex;align-items:center;gap:24px}
a{color:inherit;text-decoration:none}
code{font-family:var(--mono);font-size:.84em;background:var(--wash);border:1px solid var(--line);padding:.05em .35em;border-radius:3px}
pre{font-family:var(--mono);font-size:.8rem;background:var(--term);color:var(--termtext);border:1px solid var(--termline);padding:1rem 1.1rem;overflow-x:auto;line-height:1.55;box-shadow:0 12px 40px rgba(17,17,17,.12)}
.wrap{max-width:1080px;margin:0 auto;padding:0 24px}
.navbrand{display:inline-flex;gap:9px;align-items:center;font-family:var(--mono);font-weight:700;font-size:13px;letter-spacing:.18em}
nav.bar{display:flex;align-items:center;gap:22px;flex:1}
nav.bar a{font-family:var(--mono);font-size:11px;letter-spacing:.12em;text-transform:uppercase;color:var(--muted);padding:4px 2px;border-bottom:2px solid transparent}
nav.bar a:hover{color:var(--text)}
nav.bar a.on{color:var(--text);border-bottom-color:var(--red)}
.navside{margin-left:auto;font-family:var(--mono);font-size:11px;letter-spacing:.1em;color:var(--faint);white-space:nowrap}
.eyebrow{display:inline-flex;gap:9px;align-items:center;font-family:var(--mono);font-size:11px;letter-spacing:.2em;text-transform:uppercase;color:var(--muted);border:1px solid var(--line);padding:6px 12px;margin-bottom:14px}
.pulse{width:6px;height:6px;border-radius:50%;background:var(--red);animation:xb-pulse 2s infinite}
@keyframes xb-pulse{0%,100%{opacity:1}50%{opacity:.35}}
h1{font-size:34px;font-weight:700;letter-spacing:-.03em;line-height:1.1;margin-bottom:10px}
h2{font-size:20px;font-weight:700;letter-spacing:-.02em;margin:30px 0 10px}
h3{font-size:15px;font-weight:600;margin:20px 0 6px}
p.meta{color:var(--muted);font-size:13.5px}
button,.btn{display:inline-flex;gap:8px;align-items:center;font-family:var(--mono);font-size:11px;font-weight:600;letter-spacing:.1em;text-transform:uppercase;padding:11px 20px;border:1px solid transparent;background:var(--text);color:#fff;cursor:pointer}
button:hover,.btn:hover{background:var(--red)}
button.ghost{background:transparent;color:var(--text);border-color:var(--line2)}
button.ghost:hover{background:var(--wash);color:var(--text)}
table{border-collapse:collapse;width:100%;font-size:13.5px}
th{font-family:var(--mono);font-size:10px;letter-spacing:.14em;text-transform:uppercase;color:var(--faint);text-align:left;padding:8px 10px;border-bottom:1px solid var(--line2)}
td{padding:8px 10px;border-bottom:1px solid var(--line);vertical-align:top}
.card{border:1px solid var(--line);background:var(--bg);padding:18px 20px;margin:14px 0}
.card.surface{background:var(--surface)}
.termcard{background:var(--term);color:var(--termtext);border:1px solid var(--termline);font-family:var(--mono);font-size:.82rem;padding:16px 18px;box-shadow:0 12px 40px rgba(17,17,17,.18)}
.termcard .tl{color:rgba(248,245,235,.5);font-size:10px;letter-spacing:.18em;text-transform:uppercase;margin-bottom:8px}
textarea,input[type=text],select{font:inherit;padding:9px 11px;border:1px solid var(--line2);background:var(--bg);width:100%}
textarea:focus,input:focus{outline:none;border-color:var(--text)}
.row{display:flex;gap:10px;flex-wrap:wrap;align-items:center}
.field{display:block;margin:10px 0}
.field span{display:block;font-family:var(--mono);font-size:10px;letter-spacing:.14em;text-transform:uppercase;color:var(--faint);margin-bottom:5px}
.conflict{border-left:3px solid var(--red);padding-left:10px;background:rgba(217,42,42,.05)}
.dl{display:grid;grid-template-columns:170px 1fr;gap:6px 14px;font-size:13.5px}
.dl dt{font-family:var(--mono);font-size:10px;letter-spacing:.14em;text-transform:uppercase;color:var(--faint);padding-top:3px}
.dl dd{color:var(--text);word-break:break-all}
.mono{font-family:var(--mono)}
.muted{color:var(--muted)}
.red{color:var(--red)}
`

// xbNav renders the shared top navigation. active marks the current page key.
// xbNav renders the persistent top menubar (XtracBox fixed-blur nav): brand,
// the six pages with the red active marker, and the actor on the right so
// attribution context is always visible. Every page — including the chat
// shell — renders it via xbPage or xbHeader.
func xbNav(active, actor string) string {
	type l struct{ key, path, label string }
	links := []l{
		{"chat", "/", "Chat"},
		{"docs", "/docs", "Docs"},
		{"memory", "/memory", "Memory"},
		{"audit", "/audit", "History"},
		{"agents", "/agents", "Agents"},
		{"settings", "/settings", "Settings"},
	}
	out := `<header class="topnav"><div class="navwrap">`
	out += `<span class="navbrand"><span class="pulse"></span>LEETOFFICE</span><nav class="bar">`
	for _, item := range links {
		cls := ""
		if item.key == active {
			cls = ` class="on"`
		}
		out += `<a href="` + item.path + `"` + cls + `>` + item.label + `</a>`
	}
	out += `</nav>`
	if actor != "" {
		out += `<span class="navside">` + html.EscapeString(actor) + `</span>`
	}
	out += `</div></header>`
	return out
}

func xbPage(title, active, body string) string {
	return xbPageActor(title, active, body, "")
}

func xbPageActor(title, active, body, actor string) string {
	return `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + title + ` — LeetOffice</title><style>` + xbCSS + `</style></head>
<body>` + xbNav(active, actor) + `<div class="wrap" style="padding-top:26px">` + body + `</div></body></html>`
}
