package dashboard

import (
	"fmt"
	"html/template"
	"strings"

	"vitals/internal/diag"
)

// Nav icons: the standard Lucide (lucide.dev) line-icon set, 24x24
// viewBox, 2px round strokes — the same vocabulary VS Code, GitHub and
// most modern dashboards use, so each glyph reads at a glance. Inlined as
// path data, not a dependency (consistent with this repo's "inline the
// small surface you actually use" rule — see AGENTS.md). Deliberately not
// emoji. Rendered in the accent colour, not the label's muted grey, so
// the sidebar stays scannable in dark mode (see .navgroup a svg below).
//
// Identifier -> Lucide name: iconOverview=layout-dashboard, iconCPU=cpu,
// iconProcesses=list, iconMemory=memory-stick, iconDisk=hard-drive,
// iconNetwork=network, iconPower=battery-medium, iconContainers=container,
// iconGPU=microchip, iconAdvice=lightbulb, iconLLM=bot, iconClean=sparkles,
// iconDuplicates=copy, iconSystem=settings.
const (
	iconOverview   = template.HTML(`<rect width="7" height="9" x="3" y="3" rx="1"/><rect width="7" height="5" x="14" y="3" rx="1"/><rect width="7" height="9" x="14" y="12" rx="1"/><rect width="7" height="5" x="3" y="16" rx="1"/>`)
	iconCPU        = template.HTML(`<rect width="16" height="16" x="4" y="4" rx="2"/><rect width="6" height="6" x="9" y="9" rx="1"/><path d="M15 2v2"/><path d="M15 20v2"/><path d="M2 15h2"/><path d="M2 9h2"/><path d="M20 15h2"/><path d="M20 9h2"/><path d="M9 2v2"/><path d="M9 20v2"/>`)
	iconMemory     = template.HTML(`<path d="M6 19v-3"/><path d="M10 19v-3"/><path d="M14 19v-3"/><path d="M18 19v-3"/><path d="M8 11V9"/><path d="M16 11V9"/><path d="M12 11V9"/><path d="M2 15h20"/><path d="M2 7a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v1.1a2 2 0 0 0 0 3.837V17a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-5.1a2 2 0 0 0 0-3.837Z"/>`)
	iconDisk       = template.HTML(`<line x1="22" x2="2" y1="12" y2="12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" x2="6.01" y1="16" y2="16"/><line x1="10" x2="10.01" y1="16" y2="16"/>`)
	iconNetwork    = template.HTML(`<rect x="16" y="16" width="6" height="6" rx="1"/><rect x="2" y="16" width="6" height="6" rx="1"/><rect x="9" y="2" width="6" height="6" rx="1"/><path d="M5 16v-3a1 1 0 0 1 1-1h12a1 1 0 0 1 1 1v3"/><path d="M12 12V8"/>`)
	iconPower      = template.HTML(`<rect width="16" height="10" x="2" y="7" rx="2" ry="2"/><line x1="22" x2="22" y1="11" y2="13"/><line x1="6" x2="6" y1="11" y2="13"/><line x1="10" x2="10" y1="11" y2="13"/>`)
	iconGPU        = template.HTML(`<path d="M18 12h2"/><path d="M18 16h2"/><path d="M18 8h2"/><path d="M4 12h2"/><path d="M4 16h2"/><path d="M4 8h2"/><path d="M6 5a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2z"/><path d="M9 9h6v6H9z"/>`)
	iconAdvice     = template.HTML(`<path d="M15 14c.2-1 .7-1.7 1.5-2.5 1-.9 1.5-2.2 1.5-3.5A6 6 0 0 0 6 8c0 1 .2 2.2 1.5 3.5.7.7 1.3 1.5 1.5 2.5"/><path d="M9 18h6"/><path d="M10 22h4"/>`)
	iconLLM        = template.HTML(`<path d="M12 8V4H8"/><rect width="16" height="12" x="4" y="8" rx="2"/><path d="M2 14h2"/><path d="M20 14h2"/><path d="M15 13v2"/><path d="M9 13v2"/>`)
	iconClean      = template.HTML(`<path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .962 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.962 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/><path d="M4 17v2"/><path d="M5 18H3"/>`)
	iconDuplicates = template.HTML(`<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`)
	iconProcesses  = template.HTML(`<path d="M3 12h.01"/><path d="M3 18h.01"/><path d="M3 6h.01"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M8 6h13"/>`)
	iconContainers = template.HTML(`<path d="M22 7.7c0-.6-.4-1.2-.8-1.5l-6.3-3.9a1.72 1.72 0 0 0-1.7 0l-10.3 6c-.5.2-.9.8-.9 1.4v6.6c0 .5.4 1.2.8 1.5l6.3 3.9a1.72 1.72 0 0 0 1.7 0l10.3-6c.5-.3.9-1 .9-1.5Z"/><path d="M10 21.9V14L2.1 9.1"/><path d="m10 14 11.9-6.9"/><path d="M14 19.8v-8.1"/><path d="M18 17.5V9.4"/>`)
	// iconKubernetes: Lucide ship-wheel — reads as the k8s helm mark, and
	// visually distinct from the plain container/box glyph.
	iconKubernetes = template.HTML(`<circle cx="12" cy="12" r="8"/><path d="M12 2v7.5"/><path d="m19 5-5.23 5.23"/><path d="M22 12h-7.5"/><path d="m19 19-5.23-5.23"/><path d="M12 14.5V22"/><path d="M10.23 13.77 5 19"/><path d="M9.5 12H2"/><path d="M10.23 10.23 5 5"/><circle cx="12" cy="12" r="2.5"/>`)
	iconSystem     = template.HTML(`<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`)
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
  --accent:#2b5d53; --ok:#2b5d53; --ok-bg:#e2eeea; --warn:#9a6b08; --warn-bg:#fbf1dc; --crit:#b3401f; --crit-bg:#fbe9e3; --k8s:#2f6fb0; --k8s-bg:#e6eff8;
}
@media (prefers-color-scheme: dark){
  :root{ --bg:#14171a; --surface:#1b1f23; --surface-2:#1f242a; --ink:#e9eaea; --muted:#b4b9bd; --line:#2a2e33;
  --accent:#6fbfa8; --ok:#6fbfa8; --ok-bg:#1b2b26; --warn:#d9a441; --warn-bg:#362b18; --crit:#e2694a; --crit-bg:#3a241f; --k8s:#6ea8e0; --k8s-bg:#1c2b3a; }
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
.navgroup a{display:flex;align-items:center;gap:.65rem;padding:.5rem .55rem;border-radius:8px;color:var(--muted);font-size:.86rem;font-weight:500;text-decoration:none;margin-bottom:.05rem}
.navgroup a svg{width:17px;height:17px;flex:0 0 auto;stroke:var(--accent);fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.navgroup a:hover{color:var(--ink);background:var(--surface-2)}
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
.rescard .spark{display:block;width:100%;height:24px;margin:.1rem 0 .5rem;opacity:.85}
.rescard .detail{font-size:.76rem;color:var(--muted)}
.qa{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:.7rem}
.qa a{display:flex;flex-direction:column;gap:.3rem;background:var(--surface);border:1px solid var(--line);border-radius:10px;padding:.85rem .9rem;text-decoration:none;color:inherit}
.qa .qname{font-weight:700;color:var(--ink);font-size:.86rem}
.qa .qdesc{font-size:.74rem;color:var(--muted);line-height:1.35}
.sectiontitle{font-size:.74rem;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);font-weight:700;margin:1.3rem 0 .6rem}
.sectiontitle:first-child{margin-top:0}
.caption{font-size:.76rem;color:var(--muted);line-height:1.4;margin:-.3rem 0 .6rem}
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
.pill.muted{color:var(--muted);background:var(--surface-2)}
.rtsection{border:1px solid var(--line);border-left:4px solid var(--accent);border-radius:12px;padding:1rem 1.15rem;margin-bottom:1.4rem}
.rtsection.k8s{border-left-color:var(--k8s)}
.rtsection-head{display:flex;align-items:center;gap:.5rem;font-weight:800;font-size:.95rem;color:var(--accent);margin-bottom:.7rem}
.rtsection.k8s .rtsection-head{color:var(--k8s)}
.rtsection-head svg{width:18px;height:18px;stroke:currentColor;fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round;flex:0 0 auto}
.rtsection-head .ep{font-weight:500;color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.8rem}
.rtchip{font-size:.62rem;font-weight:800;letter-spacing:.04em;text-transform:uppercase;border-radius:5px;padding:.1rem .4rem;background:var(--ok-bg);color:var(--accent)}
.rtchip.k8s{background:var(--k8s-bg);color:var(--k8s)}
.rttoggle{display:flex;align-items:center;gap:.4rem;margin:0 0 1.1rem}
.rtlabel{font-size:.7rem;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);font-weight:700;margin-right:.3rem}
.rtpill{font-size:.8rem;font-weight:600;text-decoration:none;color:var(--muted);border:1px solid var(--line);border-radius:20px;padding:.25rem .8rem}
.rtpill:hover{border-color:var(--accent);color:var(--accent)}
.rtpill.on{background:var(--ok-bg);border-color:var(--accent);color:var(--accent)}
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
.reftoggle{font:inherit;font-size:.76rem;color:var(--muted);background:transparent;border:1px solid var(--line);border-radius:20px;padding:.15rem .7rem;cursor:pointer}
.reftoggle:hover{border-color:var(--accent);color:var(--accent)}
#vitals-main.refreshing{animation:vitalsPulse .3s ease-out}
@keyframes vitalsPulse{from{opacity:.55}to{opacity:1}}
@media (prefers-reduced-motion:reduce){#vitals-main.refreshing{animation:none}}
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
<main id="vitals-main"{{if .Live}} data-live="1"{{end}}>{{.Body}}</main>
<footer>vitals {{.Version}} — served locally, nothing leaves this machine. Press Ctrl+C in the terminal that launched it to stop.{{if .Live}} · <button id="vitals-refresh-toggle" class="reftoggle" type="button">auto-refresh: on</button>{{end}}<br>Issues or feedback: <a href="https://github.com/gautampachnanda101/vitals">github.com/gautampachnanda101/vitals</a></footer>
</div>
</div>
<script>
/* Live refresh: re-fetch this same page every {{.RefreshSeconds}}s and swap in
   the <main> content, so a left-open dashboard stays current without a
   manual reload. Pure vanilla, no dependency. Pages with interactive
   state (Clean, Duplicates) opt out by omitting data-live. */
(function(){
  var main=document.getElementById('vitals-main');
  if(!main||main.dataset.live!=="1"||!window.fetch||!window.DOMParser)return;
  var PERIOD={{.RefreshSeconds}}*1000, KEY='vitals.autorefresh';
  var toggle=document.getElementById('vitals-refresh-toggle');
  function off(){ try{return localStorage.getItem(KEY)==='off';}catch(e){return false;} }
  function paint(){ if(toggle) toggle.textContent='auto-refresh: '+(off()?'off':'on'); }
  function setOff(v){ try{localStorage.setItem(KEY,v?'off':'on');}catch(e){} paint(); }
  function tick(){
    if(off()||document.visibilityState!=='visible')return;
    fetch(location.pathname+location.search,{headers:{'X-Vitals-Refresh':'1'},cache:'no-store'})
      .then(function(r){ if(!r.ok) throw 0; return r.text(); })
      .then(function(html){
        var next=new DOMParser().parseFromString(html,'text/html').getElementById('vitals-main');
        if(next && next.dataset.live==="1" && !main.querySelector(':focus')){
          main.innerHTML=next.innerHTML;
          main.classList.remove('refreshing'); void main.offsetWidth; main.classList.add('refreshing');
        }
      })
      .catch(function(){ /* server gone or a transient blip — retry next tick */ });
  }
  if(toggle) toggle.addEventListener('click',function(){ setOff(!off()); });
  paint();
  setInterval(tick,PERIOD);
})();
</script>
</body>
</html>`))

type pageShellData struct {
	Title          string
	Nav            template.HTML
	Body           template.HTML
	Version        string
	Live           bool
	RefreshSeconds int
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
		`{{.Spark}}` +
		`<div class="detail">{{.Detail}}</div></a>`))

type resourceCardData struct {
	Slug     string
	Label    string
	Icon     template.HTML
	Value    string
	Pct      float64       // 0-100, clamped by the caller — the bar's fill width
	Severity string        // "ok" | "warning" | "critical", matching diag.Severity.String()
	Spark    template.HTML // optional trend sparkline (sparkline()); "" renders nothing
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
// liveRefreshSeconds is how often a left-open dashboard page re-fetches
// itself (client-side, see pageShellTmpl's script). Matches the
// snapshotCache TTL's order of magnitude — often enough to feel live,
// rare enough that it's no real load — and the page only polls while its
// tab is actually visible.
const liveRefreshSeconds = 10

// noLiveRefresh names the pages that must NOT auto-swap their content:
// they carry interactive client state (a Preview/Apply flow, its
// in-progress result) that an innerHTML replacement would destroy.
var noLiveRefresh = map[string]bool{"clean": true, "dupes": true}

func layout(title, activeSlug, version string, available []Module, body string) string {
	return layoutLive(title, activeSlug, version, available, body, !noLiveRefresh[activeSlug])
}

// layoutLive is layout with an explicit live-refresh decision — the
// not-found and unavailable pages pass false (nothing on them changes),
// a rendered module page passes whether its slug is in noLiveRefresh.
func layoutLive(title, activeSlug, version string, available []Module, body string, live bool) string {
	if version == "" {
		version = "dev"
	}
	return mustExecute(pageShellTmpl, pageShellData{
		Title: title,
		// Nav/Body are already-rendered, already-safe HTML from this
		// package's own templates — never raw user input — so marking
		// them template.HTML (skip re-escaping) is safe here, unlike
		// anywhere a genuinely untrusted string would need this type.
		Nav:            template.HTML(mustExecute(navTmpl, navGroups(available, activeSlug))),
		Body:           template.HTML(body),
		Version:        version,
		Live:           live,
		RefreshSeconds: liveRefreshSeconds,
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
