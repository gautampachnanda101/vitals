package dashboard

import (
	"fmt"
	"html"
	"strings"

	"vitals/internal/diag"
)

// pageShell is the whole document — system fonts only, every color inline,
// no external stylesheet or font: `vitals dashboard` has to render
// correctly with no network reachable at all, the same offline promise
// `vitals guide --web` already makes. %s: title, nav, body.
const pageShell = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s · vitals</title>
<style>
:root{
  --bg:#fbfaf7; --surface:#fff; --surface-2:#f3f1ea; --ink:#1b1f23; --muted:#6b6f76; --line:#e3e1db;
  --accent:#2b5d53; --ok:#2b5d53; --ok-bg:#e2eeea; --warn:#9a6b08; --warn-bg:#fbf1dc; --crit:#b3401f; --crit-bg:#fbe9e3;
}
@media (prefers-color-scheme: dark){
  :root{ --bg:#14171a; --surface:#1b1f23; --surface-2:#1f242a; --ink:#e9eaea; --muted:#9aa0a6; --line:#2a2e33;
  --accent:#6fbfa8; --ok:#6fbfa8; --ok-bg:#1b2b26; --warn:#d9a441; --warn-bg:#362b18; --crit:#e2694a; --crit-bg:#3a241f; }
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.6 -apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
code,.mono{font:0.9em ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
a{color:var(--accent)}
.wrap{max-width:840px;margin:0 auto;padding:1.6rem 1.4rem 3rem}
header.top{display:flex;align-items:baseline;justify-content:space-between;border-bottom:3px solid var(--accent);padding-bottom:.9rem;margin-bottom:1rem;flex-wrap:wrap;gap:.6rem}
header.top b{font-size:1.15rem;font-weight:800}
nav{display:flex;gap:.3rem;flex-wrap:wrap;margin-bottom:1.4rem}
nav a{font-size:.8rem;padding:.35rem .7rem;border-radius:20px;text-decoration:none;color:var(--muted);border:1px solid var(--line)}
nav a.active{color:var(--accent);border-color:var(--accent);font-weight:600}
.card{background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:1.1rem 1.3rem;margin-bottom:1rem}
.verdict{display:flex;align-items:center;gap:.8rem;border-radius:10px;padding:.9rem 1.1rem;margin-bottom:1.2rem;border:1px solid}
.verdict.ok{background:var(--ok-bg);border-color:var(--ok)} .verdict.warning{background:var(--warn-bg);border-color:var(--warn)} .verdict.critical{background:var(--crit-bg);border-color:var(--crit)}
.dot{width:.65rem;height:.65rem;border-radius:50%%;flex:0 0 auto}
.verdict.ok .dot{background:var(--ok)} .verdict.warning .dot{background:var(--warn)} .verdict.critical .dot{background:var(--crit)}
.summary{font:0.85em ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--muted)}
.finding{border-left:3px solid var(--line);padding:.3rem 0 .3rem .8rem;margin-bottom:.7rem}
.finding.warning{border-color:var(--warn)} .finding.critical{border-color:var(--crit)}
.finding .title{font-weight:700}
.finding .detail{color:var(--muted);font-size:.9em}
.finding .fix{font-size:.85em;margin-top:.2rem}
.row{display:flex;justify-content:space-between;padding:.4rem 0;border-bottom:1px solid var(--line)}
.row:last-child{border-bottom:none}
.row .k{color:var(--muted)}
.unavailable{color:var(--muted);font-style:italic}
footer{color:var(--muted);font-size:.78rem;margin-top:2rem;text-align:center}
</style>
</head>
<body>
<div class="wrap">
<header class="top"><b>vitals</b><span class="mono" style="color:var(--muted)">local dashboard — nothing leaves this machine</span></header>
<nav>%s</nav>
%s
<footer>Served locally by the vitals binary — press Ctrl+C in the terminal that launched it to stop.</footer>
</div>
</body>
</html>`

// layout wraps body in the shared page shell with a nav bar built from
// available — the plugin list this PageContext can actually offer — with
// activeSlug highlighted.
func layout(title, activeSlug string, available []Module, body string) string {
	var nav strings.Builder
	for _, m := range available {
		class := ""
		if m.Slug == activeSlug {
			class = ` class="active"`
		}
		href := "/" + m.Slug
		fmt.Fprintf(&nav, `<a href="%s"%s>%s</a>`, href, class, html.EscapeString(m.NavLabel))
	}
	return fmt.Sprintf(pageShell, html.EscapeString(title), nav.String(), body)
}

// verdictBanner renders the overall or per-resource verdict as a colored
// banner with the at-a-glance summary line beneath it.
func verdictBanner(headline, summary string, worst diag.Severity) string {
	return fmt.Sprintf(`<div class="verdict %s"><div class="dot"></div><div><div><b>%s</b></div><div class="summary">%s</div></div></div>`,
		worst.String(), html.EscapeString(headline), html.EscapeString(summary))
}

// findingsList renders a ranked finding list — title, detail, fixes — or a
// friendly "nothing to report" line when there are none.
func findingsList(findings []diag.Finding) string {
	if len(findings) == 0 {
		return `<p class="unavailable">No findings — this looks healthy.</p>`
	}
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, `<div class="finding %s"><div class="title">%s</div>`, f.Severity.String(), html.EscapeString(f.Title))
		if f.Detail != "" {
			fmt.Fprintf(&b, `<div class="detail">%s</div>`, html.EscapeString(f.Detail))
		}
		for _, fix := range f.Fixes {
			fmt.Fprintf(&b, `<div class="fix">→ %s</div>`, html.EscapeString(fix))
		}
		b.WriteString(`</div>`)
	}
	return b.String()
}

// row renders one label/value line inside a .card.
func row(label, value string) string {
	return fmt.Sprintf(`<div class="row"><span class="k">%s</span><span>%s</span></div>`, html.EscapeString(label), html.EscapeString(value))
}

// unavailablePage explains why a module a machine doesn't currently offer
// still 404s cleanly instead of a bare "not found" — e.g. no LLM reachable,
// no GPU detected.
func unavailablePage(navLabel, reason string) string {
	return fmt.Sprintf(`<div class="card"><p class="unavailable">%s isn't available on this machine right now: %s</p></div>`,
		html.EscapeString(navLabel), html.EscapeString(reason))
}
