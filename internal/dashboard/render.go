package dashboard

import (
	"fmt"
	"html/template"
	"strings"

	"vitals/internal/diag"
)

// Nav icons: small, stroke-based, 24x24-viewBox glyphs — one per Module,
// set as its own Icon field at registration. Deliberately not emoji
// (AGENTS.md's own "avoid AI slop tropes" guidance, applied here): a
// consistent line-icon vocabulary scales and recolors with the theme
// (stroke="currentColor" inherits .navgroup a's own color, so the active
// state's accent color and dark-mode's swap both apply for free).
const (
	iconOverview   = template.HTML(`<path d="M3 12l9-8 9 8M5 10v10h14V10"/>`)
	iconCPU        = template.HTML(`<rect x="5" y="5" width="14" height="14" rx="1.5"/><path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3"/>`)
	iconMemory     = template.HTML(`<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 4v4M16 4v4M8 16v4M16 16v4"/>`)
	iconDisk       = template.HTML(`<ellipse cx="12" cy="6" rx="8" ry="3"/><path d="M4 6v6c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6"/>`)
	iconNetwork    = template.HTML(`<path d="M4 18h16M4 18V9l8-5 8 5v9"/><path d="M9 18v-5h6v5"/>`)
	iconPower      = template.HTML(`<rect x="3" y="7" width="16" height="10" rx="1.5"/><path d="M19 10v4"/>`)
	iconGPU        = template.HTML(`<rect x="3" y="4" width="18" height="13" rx="1.5"/><path d="M8 20h8M12 17v3"/>`)
	iconAdvice     = template.HTML(`<path d="M12 3a5 5 0 00-5 5v2a4 4 0 00-2 3.5A3.5 3.5 0 008.5 17H9v3h6v-3h.5a3.5 3.5 0 003.5-3.5A4 4 0 0017 10V8a5 5 0 00-5-5z"/>`)
	iconLLM        = template.HTML(`<rect x="4" y="4" width="16" height="16" rx="3"/><circle cx="9" cy="10" r="1.2"/><circle cx="15" cy="10" r="1.2"/><path d="M8 15h8"/>`)
	iconClean      = template.HTML(`<path d="M14.7 6.3a4 4 0 01-5.4 5.4L4 17l3 3 5.3-5.3a4 4 0 015.4-5.4l-3 3-2-2z"/>`)
	iconDuplicates = template.HTML(`<path d="M9 4H4v6M4 4l7 7M15 4h5v6M20 4l-7 7M9 20H4v-6M4 20l7-7M15 20h5v-6M20 20l-7-7"/>`)
	iconProcesses  = template.HTML(`<circle cx="6" cy="6" r="2"/><circle cx="6" cy="12" r="2"/><circle cx="6" cy="18" r="2"/><path d="M11 6h9M11 12h9M11 18h9"/>`)
	iconSystem     = template.HTML(`<circle cx="12" cy="12" r="8"/><path d="M12 8v4l3 2"/>`)
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
.app{display:flex;min-height:100vh}
.sidebar{width:216px;flex:0 0 216px;background:var(--surface);border-right:1px solid var(--line);padding:1.3rem .9rem;display:flex;flex-direction:column;gap:1.4rem}
.brand{padding:0 .5rem .9rem;border-bottom:1px solid var(--line);margin-bottom:.2rem}
.brand b{font-size:1.05rem;font-weight:800}
.brand span{display:block;font-size:.68rem;color:var(--muted);margin-top:.15rem}
.navgroup h4{font-size:.66rem;text-transform:uppercase;letter-spacing:.07em;color:var(--muted);margin:0 0 .35rem .55rem;font-weight:700}
.navgroup a{display:flex;align-items:center;gap:.6rem;padding:.46rem .55rem;border-radius:8px;color:var(--muted);font-size:.86rem;font-weight:500;text-decoration:none;margin-bottom:.05rem}
.navgroup a svg{width:15px;height:15px;flex:0 0 auto;stroke:currentColor;fill:none;stroke-width:1.8}
.navgroup a[aria-current="page"]{background:var(--ok-bg);color:var(--accent);font-weight:700}
.main{flex:1;min-width:0;padding:1.5rem 2rem 2.6rem;max-width:1000px}
header.top{margin-bottom:1.2rem}
header.top h1{font-size:1.28rem;margin:0;font-weight:800}
.card{background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:1.1rem 1.3rem;margin-bottom:1rem}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:.85rem;margin-bottom:1rem}
.rescard{background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:.95rem 1.05rem;text-decoration:none;color:inherit;display:block}
.rescard h3{font-size:.74rem;margin:0 0 .5rem;color:var(--muted);font-weight:700;text-transform:uppercase;letter-spacing:.03em}
.rescard .val{font-size:1.5rem;font-weight:800;line-height:1}
.rescard .val.warn{color:var(--warn)}
.rescard .val.crit{color:var(--crit)}
.bar{height:6px;border-radius:4px;background:var(--surface-2);overflow:hidden;margin:.55rem 0 .5rem}
.bar>span{display:block;height:100%;background:var(--accent);border-radius:4px}
.bar.warn>span{background:var(--warn)}
.bar.crit>span{background:var(--crit)}
.rescard .detail{font-size:.76rem;color:var(--muted)}
.qa{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:.7rem}
.qa a{display:flex;flex-direction:column;gap:.3rem;background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:.85rem .9rem;text-decoration:none;color:inherit}
.qa .qname{font-weight:700;color:var(--ink);font-size:.86rem}
.qa .qdesc{font-size:.74rem;color:var(--muted);line-height:1.35}
.sectiontitle{font-size:.74rem;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);font-weight:700;margin:1.3rem 0 .6rem}
.sectiontitle:first-child{margin-top:0}
.modelcard{display:flex;justify-content:space-between;align-items:center;gap:1rem;background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:.85rem 1rem;margin-bottom:.6rem}
.modelcard .mname{font-weight:700;font-size:.88rem}
.modelcard .msub{font-size:.76rem;color:var(--muted);margin-top:.15rem}
.toolgrid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:.7rem}
.toolcard{background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:.8rem .95rem}
.toolhead{display:flex;justify-content:space-between;align-items:center;margin-bottom:.25rem;gap:.5rem}
.toolname{font-weight:700;font-size:.88rem}
.toolcat{font-size:.72rem;color:var(--muted);margin-bottom:.35rem}
.tooldesc{font-size:.78rem;color:var(--muted);line-height:1.4}
.pill{font-size:.7rem;font-weight:700;border-radius:20px;padding:.2rem .6rem;white-space:nowrap}
.pill.ok{color:var(--ok);background:var(--ok-bg)}
.pill.warn{color:var(--warn);background:var(--warn-bg)}
.pill.crit{color:var(--crit);background:var(--crit-bg)}
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
.path{padding:.35rem 0 .35rem 1rem;border-bottom:1px solid var(--line)}
.path:last-child{border-bottom:none}
.path .dir{color:var(--muted);font-size:.8em;overflow-wrap:anywhere}
.path .name{font-weight:600;overflow-wrap:anywhere}
.unavailable{color:var(--muted);font-style:italic}
.btn{font:inherit;font-weight:600;color:var(--accent);background:transparent;border:1px solid var(--accent);border-radius:8px;padding:.5rem 1.1rem;cursor:pointer}
.btn:hover{background:var(--bg)}
.btn:disabled{opacity:.6;cursor:default}
footer{color:var(--muted);font-size:.78rem;margin-top:2rem;text-align:center}
</style>
</head>
<body>
<div class="app">
<div class="sidebar">
<div class="brand"><b>vitals</b><span>local dashboard — nothing leaves this machine</span></div>
<nav aria-label="Primary">{{.Nav}}</nav>
</div>
<div class="main">
<header class="top"><h1>{{.Title}}</h1></header>
<main>{{.Body}}</main>
<footer>vitals {{.Version}} — served locally, nothing leaves this machine. Press Ctrl+C in the terminal that launched it to stop.<br>Issues or feedback: <a href="https://github.com/gautampachnanda101/vitals">github.com/gautampachnanda101/vitals</a></footer>
</div>
</div>
</body>
</html>`))

type pageShellData struct {
	Title   string
	Nav     template.HTML
	Body    template.HTML
	Version string
}

// navTmpl renders the sidebar as one section per group, in navGroupOrder —
// aria-current is both the styling hook (see ".navgroup a[aria-current]"
// above) and what tells assistive tech which nav item is the current
// page, one attribute doing both jobs instead of a class plus an aria
// attribute that could drift out of sync.
var navTmpl = template.Must(template.New("nav").Parse(
	`{{range .}}<div class="navgroup"><h4>{{.Title}}</h4>{{range .Items}}<a href="/{{.Slug}}"{{if .Active}} aria-current="page"{{end}}>{{if .Icon}}<svg viewBox="0 0 24 24">{{.Icon}}</svg>{{end}}{{.NavLabel}}</a>{{end}}</div>{{end}}`))

type navItem struct {
	Slug     string
	NavLabel string
	Icon     template.HTML
	Active   bool
}

type navGroup struct {
	Title string
	Items []navItem
}

// navGroupOrder is the sidebar's fixed section order. A module whose
// Group doesn't match any of these (a typo, or simply unset) still
// appears — grouped into a trailing "Other" section — rather than
// silently vanishing from the nav, so the mistake is visible as an
// oddly-placed link instead of an unreachable page.
var navGroupOrder = []string{"Overview", "Resources", "Intelligence", "Tools", "System"}

// navGroups buckets available into navGroupOrder's sections (plus a
// trailing "Other" for anything unmatched), preserving each module's own
// relative order (already Order-sorted by availableModules) within its
// section. Empty sections are omitted.
func navGroups(available []Module, activeSlug string) []navGroup {
	byTitle := map[string][]navItem{}
	var order []string
	seen := map[string]bool{}
	for _, m := range available {
		title := m.Group
		if !seenGroupTitle(title) {
			title = "Other"
		}
		if !seen[title] {
			seen[title] = true
			order = append(order, title)
		}
		byTitle[title] = append(byTitle[title], navItem{Slug: m.Slug, NavLabel: m.NavLabel, Icon: m.Icon, Active: m.Slug == activeSlug})
	}
	// Prefer navGroupOrder's canonical ordering over discovery order,
	// then append anything else (just "Other", in practice) at the end.
	var out []navGroup
	placed := map[string]bool{}
	for _, title := range navGroupOrder {
		if items, ok := byTitle[title]; ok {
			out = append(out, navGroup{Title: title, Items: items})
			placed[title] = true
		}
	}
	for _, title := range order {
		if !placed[title] {
			out = append(out, navGroup{Title: title, Items: byTitle[title]})
		}
	}
	return out
}

func seenGroupTitle(title string) bool {
	for _, g := range navGroupOrder {
		if g == title {
			return true
		}
	}
	return false
}

// resourceCardTmpl renders one Overview summary card: a big headline
// value, a proportional bar, and a one-line detail — clicking through to
// the resource's own full page. severity drives both the value's and the
// bar's color, matching diag.Severity's own "ok"/"warning"/"critical"
// vocabulary, mapped to this file's .pill classes ("ok" needs no class
// at all — the default --ink/--accent already reads as "nothing wrong").
var resourceCardTmpl = template.Must(template.New("resourceCard").Parse(
	`<a class="rescard" href="/{{.Slug}}"><h3>{{if .Icon}}<svg viewBox="0 0 24 24" style="width:13px;height:13px;stroke:currentColor;fill:none;stroke-width:1.8;vertical-align:-2px;margin-right:.3rem">{{.Icon}}</svg>{{end}}{{.Label}}</h3>` +
		`<div class="val{{if eq .Severity "warning"}} warn{{else if eq .Severity "critical"}} crit{{end}}">{{.Value}}</div>` +
		`<div class="bar{{if eq .Severity "warning"}} warn{{else if eq .Severity "critical"}} crit{{end}}"><span style="width:{{.Pct}}%"></span></div>` +
		`<div class="detail">{{.Detail}}</div></a>`))

type resourceCardData struct {
	Slug     string
	Label    string
	Icon     template.HTML
	Value    string
	Pct      float64 // 0-100, clamped by the caller — the bar's fill width
	Severity string  // "ok" | "warning" | "critical", matching diag.Severity.String()
	Detail   string
}

func resourceCard(d resourceCardData) string {
	if d.Pct < 0 {
		d.Pct = 0
	} else if d.Pct > 100 {
		d.Pct = 100
	}
	return mustExecute(resourceCardTmpl, d)
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
	if version == "" {
		version = "dev"
	}
	return mustExecute(pageShellTmpl, pageShellData{
		Title: title,
		// Nav/Body are already-rendered, already-safe HTML from this
		// package's own templates — never raw user input — so marking
		// them template.HTML (skip re-escaping) is safe here, unlike
		// anywhere a genuinely untrusted string would need this type.
		Nav:     template.HTML(mustExecute(navTmpl, navGroups(available, activeSlug))),
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

// findingsList renders findings via findingsListTmpl, or "" when there
// are none — every caller already leads with a verdictBanner whose own
// headline (reportHeadline's healthy-fallback text) says "nothing's
// wrong" first; a second "No findings — this looks healthy" card right
// below it repeated the same fact a reader had just read, the exact
// "stating a fact the reader already knows is not an insight" case
// AGENTS.md's own Non-negotiable principles rule out. Callers check for
// "" and skip the wrapping card/section entirely in that case, rather
// than rendering an empty box.
func findingsList(findings []diag.Finding) string {
	if len(findings) == 0 {
		return ""
	}
	data := make([]findingData, len(findings))
	for i, f := range findings {
		data[i] = findingData{Severity: f.Severity.String(), Title: f.Title, Detail: f.Detail, Fixes: f.Fixes}
	}
	return mustExecute(findingsListTmpl, data)
}

// findingsCard wraps findingsList's output in a .card, or returns ""
// when there's nothing to show — the shared "skip the redundant empty
// box" logic every findingsList caller needs, so a resource/advice/
// overview page's own healthy verdict banner isn't immediately followed
// by an empty or "nothing to report" card repeating it.
func findingsCard(findings []diag.Finding) string {
	if list := findingsList(findings); list != "" {
		return `<div class="card">` + list + `</div>`
	}
	return ""
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
