package dashboard

import (
	"fmt"
	"html/template"
	"strings"

	"vitals/internal/diag"
)

// pageShellTmpl is the whole document — system fonts only, every color
// inline, no external stylesheet or font: `vitals dashboard` has to
// render correctly with no network reachable at all, the same offline
// promise `vitals guide --web` already makes.
//
// Every render function in this file uses html/template, not manual
// html.EscapeString calls, specifically so a future addition (a chart, a
// machine-info table) can't reach the page unescaped by simply forgetting
// to call an escaping helper — auto-escaping is the only path a
// template.Execute call has. Nav/Body are the one deliberate exception:
// they're template.HTML, because they're already-rendered, already-safe
// HTML built by this same package's own templates, not untrusted input —
// re-escaping them would turn every "<div>" into visible text.
var pageShellTmpl = template.Must(template.New("pageShell").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · vitals</title>
<style>
:root{
  /* WCAG 2.2 Level AAA: every color used as text (not a border or a
     decorative dot) holds >=7:1 against every background it can appear
     on in this file — --muted is checked against bg/surface/surface-2/
     ok-bg/warn-bg/crit-bg (it renders on all of them: nav links, the
     verdict summary line, finding details, table rows, the footer), not
     just its most common one. --warn/--crit are never used as text color
     here (only border-color and the status dot's background), so they
     stay at their original, more saturated values — non-text contrast
     is a 3:1 requirement (AA), comfortably met already. Re-run the ratio
     check (see internal/dashboard/render_test.go) before changing any of
     these, not just eyeballing it. */
  --bg:#fbfaf7; --surface:#fff; --surface-2:#f3f1ea; --ink:#1b1f23; --muted:#4b4e53; --line:#e3e1db;
  --accent:#2b5d53; --ok:#2b5d53; --ok-bg:#e2eeea; --warn:#9a6b08; --warn-bg:#fbf1dc; --crit:#b3401f; --crit-bg:#fbe9e3;
}
@media (prefers-color-scheme: dark){
  :root{ --bg:#14171a; --surface:#1b1f23; --surface-2:#1f242a; --ink:#e9eaea; --muted:#b4b9bd; --line:#2a2e33;
  --accent:#6fbfa8; --ok:#6fbfa8; --ok-bg:#1b2b26; --warn:#d9a441; --warn-bg:#362b18; --crit:#e2694a; --crit-bg:#3a241f; }
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.6 -apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif}
code,.mono{font:0.9em ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
a{color:var(--accent)}
h1,h2,h3{color:var(--accent);line-height:1.3;margin:1.1rem 0 .5rem}
h1:first-child,h2:first-child,h3:first-child{margin-top:0}
:focus-visible{outline:3px solid var(--accent);outline-offset:2px}
.wrap{max-width:840px;margin:0 auto;padding:1.6rem 1.4rem 3rem}
header.top{display:flex;align-items:baseline;justify-content:space-between;border-bottom:3px solid var(--accent);padding-bottom:.9rem;margin-bottom:1rem;flex-wrap:wrap;gap:.6rem}
header.top b{font-size:1.15rem;font-weight:800}
nav{display:flex;gap:.3rem;flex-wrap:wrap;margin-bottom:1.4rem}
nav a{font-size:.8rem;padding:.35rem .7rem;border-radius:20px;text-decoration:none;color:var(--muted);border:1px solid var(--line)}
nav a[aria-current="page"]{color:var(--accent);border-color:var(--accent);font-weight:600}
.card{background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:1.1rem 1.3rem;margin-bottom:1rem}
.verdict{display:flex;align-items:center;gap:.8rem;border-radius:10px;padding:.9rem 1.1rem;margin-bottom:1.2rem;border:1px solid}
.verdict.ok{background:var(--ok-bg);border-color:var(--ok)} .verdict.warning{background:var(--warn-bg);border-color:var(--warn)} .verdict.critical{background:var(--crit-bg);border-color:var(--crit)}
.dot{width:.65rem;height:.65rem;border-radius:50%;flex:0 0 auto}
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
.btn{font:inherit;font-weight:600;color:var(--accent);background:transparent;border:1px solid var(--accent);border-radius:8px;padding:.5rem 1.1rem;cursor:pointer}
.btn:hover{background:var(--bg)}
.btn:disabled{opacity:.6;cursor:default}
footer{color:var(--muted);font-size:.78rem;margin-top:2rem;text-align:center}
</style>
</head>
<body>
<div class="wrap">
<header class="top"><b>vitals</b><span class="mono" style="color:var(--muted)">local dashboard — nothing leaves this machine</span></header>
<nav aria-label="Primary">{{.Nav}}</nav>
<main>{{.Body}}</main>
<footer>vitals {{.Version}} — served locally, nothing leaves this machine. Press Ctrl+C in the terminal that launched it to stop.<br>Issues or feedback: <a href="https://github.com/gautampachnanda101/vitals">github.com/gautampachnanda101/vitals</a></footer>
</div>
</body>
</html>`))

type pageShellData struct {
	Title   string
	Nav     template.HTML
	Body    template.HTML
	Version string
}

// navTmpl renders one nav bar from a []navItem — aria-current is both the
// styling hook (see "nav a[aria-current]" above) and what tells assistive
// tech which nav item is the current page, one attribute doing both jobs
// instead of a class plus an aria attribute that could drift out of sync.
var navTmpl = template.Must(template.New("nav").Parse(
	`{{range .}}<a href="/{{.Slug}}"{{if .Active}} aria-current="page"{{end}}>{{.NavLabel}}</a>{{end}}`))

type navItem struct {
	Slug     string
	NavLabel string
	Active   bool
}

// mustExecute runs t against data and returns the result. Execute can
// only fail here from a template/data mismatch — a coding bug, not
// anything a request can trigger — so this panics rather than silently
// returning broken HTML; net/http recovers a panic per-request without
// taking the rest of the server down with it.
func mustExecute(t *template.Template, data any) string {
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		panic(fmt.Sprintf("dashboard: template %s: %v", t.Name(), err))
	}
	return b.String()
}

// layout wraps body in the shared page shell with a nav bar built from
// available — the plugin list this PageContext can actually offer — with
// activeSlug highlighted. version is whatever main.version holds ("dev"
// outside a tagged release build); shown in the footer so a bug report
// can include it without the reporter having to also run `vitals version`.
func layout(title, activeSlug, version string, available []Module, body string) string {
	items := make([]navItem, len(available))
	for i, m := range available {
		items[i] = navItem{Slug: m.Slug, NavLabel: m.NavLabel, Active: m.Slug == activeSlug}
	}
	if version == "" {
		version = "dev"
	}
	return mustExecute(pageShellTmpl, pageShellData{
		Title: title,
		// Nav/Body are already-rendered, already-safe HTML from this
		// package's own templates — never raw user input — so marking
		// them template.HTML (skip re-escaping) is safe here, unlike
		// anywhere a genuinely untrusted string would need this type.
		Nav:     template.HTML(mustExecute(navTmpl, items)),
		Body:    template.HTML(body),
		Version: version,
	})
}

// verdictBannerTmpl renders the overall or per-resource verdict as a
// colored banner with the at-a-glance summary line beneath it. Worst is
// one of diag.Severity's fixed String() values ("ok"/"warning"/
// "critical"), which double as the CSS classnames above.
var verdictBannerTmpl = template.Must(template.New("verdictBanner").Parse(
	`<div class="verdict {{.Worst}}"><div class="dot"></div><div><div><b>{{.Headline}}</b></div><div class="summary">{{.Summary}}</div></div></div>`))

func verdictBanner(headline, summary string, worst diag.Severity) string {
	return mustExecute(verdictBannerTmpl, struct{ Worst, Headline, Summary string }{worst.String(), headline, summary})
}

// findingsListTmpl renders a ranked finding list — title, detail, fixes.
var findingsListTmpl = template.Must(template.New("findingsList").Parse(
	`{{range .}}<div class="finding {{.Severity}}"><div class="title">{{.Title}}</div>{{if .Detail}}<div class="detail">{{.Detail}}</div>{{end}}{{range .Fixes}}<div class="fix">→ {{.}}</div>{{end}}</div>{{end}}`))

type findingData struct {
	Severity string
	Title    string
	Detail   string
	Fixes    []string
}

// findingsList renders findings via findingsListTmpl, or a friendly
// "nothing to report" line when there are none — fully static, so it
// stays a plain string rather than its own template.
func findingsList(findings []diag.Finding) string {
	if len(findings) == 0 {
		return `<p class="unavailable">No findings — this looks healthy.</p>`
	}
	data := make([]findingData, len(findings))
	for i, f := range findings {
		data[i] = findingData{Severity: f.Severity.String(), Title: f.Title, Detail: f.Detail, Fixes: f.Fixes}
	}
	return mustExecute(findingsListTmpl, data)
}

// reportHeadline is the one-line summary a verdict banner leads with: the
// worst finding's title when there is one, or healthyText otherwise.
// Shared by the overview and every resource page so the two can't drift
// into subtly different wording for the same "what's the one thing wrong
// here" question.
func reportHeadline(report diag.Report, healthyText string) string {
	if len(report.Findings) == 0 {
		return healthyText
	}
	return report.SortedBySeverity()[0].Title
}

// rowTmpl renders one label/value line inside a .card.
var rowTmpl = template.Must(template.New("row").Parse(
	`<div class="row"><span class="k">{{.Label}}</span><span>{{.Value}}</span></div>`))

func row(label, value string) string {
	return mustExecute(rowTmpl, struct{ Label, Value string }{label, value})
}

// unavailablePageTmpl explains why a module a machine doesn't currently
// offer still 404s cleanly instead of a bare "not found" — e.g. no LLM
// reachable, no GPU detected.
var unavailablePageTmpl = template.Must(template.New("unavailablePage").Parse(
	`<div class="card"><p class="unavailable">{{.NavLabel}} isn't available on this machine right now: {{.Reason}}</p></div>`))

func unavailablePage(navLabel, reason string) string {
	return mustExecute(unavailablePageTmpl, struct{ NavLabel, Reason string }{navLabel, reason})
}
