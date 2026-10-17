package dashboard

import (
	"fmt"
	"html/template"
	"strings"

	"vitals/internal/diag"
)

// Nav icons: the standard Lucide (lucide.dev) line-icon set — ISC/MIT,
// licence embedded as static/LICENSE-lucide.txt (see static.go) — 24x24
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

// Theme tokens. The palette follows the homebrew-tap site
// (gautampachnanda101.github.io/homebrew-tap): ink-navy chrome, warm cream
// page, brass-gold accent in light; slate surfaces with the same gold in
// dark. Values are adjusted where the tap's own colors fall short of
// WCAG 2.2 AAA (its #697178 muted text and #1769aa links are ~4.6:1).
//
// AAA rules every value below holds — re-run render_test.go's contrast
// checks before changing any of them, don't eyeball it:
//   - text tokens (--ink, --muted, --accent, --*-ink, --side-*) are
//     >=7:1 against every background they can be painted on;
//   - mark tokens (--ok/--warn/--crit/--k8s, --gold, --ctl, --c1..--c6)
//     are never used as text, only as bars, borders, dots and chart
//     slices, and hold >=3:1 (WCAG 1.4.11) against the surfaces they
//     sit on. --line/--line-strong are purely decorative dividers.
//
// Dark tokens are written once and applied two ways: by the OS
// preference (unless the user pinned light) and by an explicit
// data-theme="dark" pin from the theme toggle.
const lightTokens = `color-scheme:light;
  --bg:#f4f1ea;--surface:#fffdf8;--surface-2:#e9e4d8;--ink:#101c24;--muted:#3b454c;--accent:#0d4a75;--gold:#8b6915;
  --line:rgba(16,28,36,.12);--line-strong:rgba(16,28,36,.22);--ctl:#6f777d;
  --ok:#2e7d4f;--ok-bg:#e3ece4;--ok-ink:#1d4d2c;--warn:#a77b0e;--warn-bg:#f6ebcf;--warn-ink:#5c4400;
  --crit:#b3401f;--crit-bg:#f7e3dc;--crit-ink:#7e2410;--k8s:#2f6fb0;--k8s-bg:#e3ecf4;--k8s-ink:#174a7a;
  --side:#101c24;--side-hover:#1b2b35;--side-ink:#f4f1ea;--side-muted:#c9cfd1;--side-gold:#e1c979;--side-line:rgba(244,241,234,.12);
  --btn:#c8a64b;--btn-hover:#d7ba68;--btn-ink:#101c24;--topbar:rgba(244,241,234,.88);
  --shadow:0 1px 2px rgba(16,28,36,.05),0 2px 10px rgba(16,28,36,.05);--shadow-hover:0 2px 4px rgba(16,28,36,.07),0 12px 28px rgba(16,28,36,.10);
  --c1:#1769aa;--c2:#8b6915;--c3:#2e7d4f;--c4:#7a4fa0;--c5:#b3401f;--c6:#0f7c86;--c-other:#697178;`

const darkTokens = `color-scheme:dark;
  --bg:#10171c;--surface:#19232a;--surface-2:#22313a;--ink:#edf2f5;--muted:#b8c2c8;--accent:#9ed0ed;--gold:#e1c979;
  --line:rgba(237,242,245,.09);--line-strong:rgba(237,242,245,.18);--ctl:#7d8b93;
  --ok:#5fb985;--ok-bg:#16291f;--ok-ink:#9bd7b0;--warn:#d9a441;--warn-bg:#2e2612;--warn-ink:#e8c66f;
  --crit:#e2694a;--crit-bg:#33201b;--crit-ink:#f6b3a1;--k8s:#6ea8e0;--k8s-bg:#172634;--k8s-ink:#a9cdf0;
  --side:#0b1115;--side-hover:#18252c;--side-ink:#edf2f5;--side-muted:#c2cbd0;--side-gold:#e1c979;--side-line:rgba(237,242,245,.08);
  --btn:#d7ba68;--btn-hover:#e1c979;--btn-ink:#101c24;--topbar:rgba(16,23,28,.88);
  --shadow:0 1px 2px rgba(0,0,0,.28),0 2px 10px rgba(0,0,0,.22);--shadow-hover:0 2px 4px rgba(0,0,0,.32),0 12px 28px rgba(0,0,0,.34);
  --c1:#6fb3e0;--c2:#d7ba68;--c3:#5fb985;--c4:#b58fd9;--c5:#e2795c;--c6:#4fc1c9;--c-other:#8e9da1;`

// Lucide sun / moon for the theme toggle — same inline-path convention as
// the nav icons above.
const (
	iconSun  = `<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>`
	iconMoon = `<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`
)

// pageShellTmpl is the whole document — every color inline, no external
// stylesheet, font or script: `vitals dashboard` has to render correctly
// with no network reachable at all, the same offline promise
// `vitals guide --web` already makes. The tap site's typefaces (Space
// Grotesk, IBM Plex Mono) are embedded in the binary and served by this
// same local server (see static.go); the system UI font is only the
// fallback while they load.
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
<script>
/* Apply a pinned theme before first paint so there's no light/dark flash. */
(function(){try{var t=localStorage.getItem('vitals.theme');if(t==='light'||t==='dark')document.documentElement.setAttribute('data-theme',t);}catch(e){}})();
</script>
<style>
` + fontFaceCSS + `:root{` + lightTokens + `
  --font:"Space Grotesk",ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
  --mono:"IBM Plex Mono",ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  --radius:6px;--side-w:232px;
}
@media (prefers-color-scheme: dark){:root:not([data-theme="light"]){` + darkTokens + `}}
:root[data-theme="dark"]{` + darkTokens + `}
*{box-sizing:border-box}
/* Any display rule on a class (.btn is inline-flex) beats the browser's
   own [hidden]{display:none}, which once made the Clean page's
   destructive Apply button visible before a preview. Keep this first. */
[hidden]{display:none!important}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.6 var(--font);-webkit-font-smoothing:antialiased;-moz-osx-font-smoothing:grayscale;text-rendering:optimizeLegibility}
code,.mono{font-family:var(--mono);font-size:.88em;font-variant-numeric:tabular-nums}
a{color:var(--accent);text-underline-offset:.18em;text-decoration-thickness:1px}
a:hover{text-decoration-thickness:2px}
h1,h2,h3{color:var(--accent);line-height:1.25;letter-spacing:-.01em;margin:1.2rem 0 .5rem;font-weight:700}
h1:first-child,h2:first-child,h3:first-child{margin-top:0}
:focus-visible{outline:3px solid var(--accent);outline-offset:2px;border-radius:2px}
::selection{background:var(--btn);color:var(--btn-ink)}
.skip{position:absolute;left:1rem;top:-4rem;z-index:50;background:var(--btn);color:var(--btn-ink);font-weight:700;padding:.7rem 1rem;border-radius:var(--radius);text-decoration:none}
.skip:focus{top:1rem}

/* ---- app shell ---------------------------------------------------- */
.app{display:flex;min-height:100vh}
.sidebar{width:var(--side-w);flex:0 0 var(--side-w);background:var(--side);color:var(--side-ink);position:sticky;top:0;height:100vh;overflow-y:auto;padding:1.25rem .85rem 1.5rem;display:flex;flex-direction:column;gap:1.35rem;isolation:isolate}
.sidebar::before{content:"";position:absolute;z-index:-1;width:14rem;height:14rem;right:-10.5rem;top:-9.5rem;border:1px solid var(--side-gold);border-radius:50%;opacity:.35;pointer-events:none}
.sidebar :focus-visible{outline-color:var(--side-gold)}
.brand{display:flex;align-items:center;gap:.7rem;padding:.15rem .55rem 1.1rem;border-bottom:1px solid var(--side-line)}
.brand .mark{width:30px;height:30px;flex:0 0 30px;border-radius:50%;border:2px solid var(--side-gold);display:grid;place-items:center}
.brand .mark::after{content:"";width:10px;height:10px;border-radius:50%;background:var(--side-gold)}
.brand b{display:block;font-size:1.12rem;font-weight:700;letter-spacing:-.02em;line-height:1.1}
.brand span{display:block;font:500 .66rem/1.3 var(--mono);letter-spacing:.08em;text-transform:uppercase;color:var(--side-muted);margin-top:.2rem}
.sidebar nav{display:flex;flex-direction:column;gap:1.15rem}
.navgroup h4{font:600 .64rem/1 var(--mono);text-transform:uppercase;letter-spacing:.1em;color:var(--side-muted);margin:0 0 .45rem .7rem}
.navgroup a{position:relative;display:flex;align-items:center;gap:.7rem;min-height:44px;padding:.45rem .7rem;border-radius:var(--radius);color:var(--side-ink);font-size:.9rem;font-weight:500;text-decoration:none;transition:background-color .15s ease,color .15s ease}
.navgroup a svg{width:18px;height:18px;flex:0 0 auto;stroke:var(--side-gold);fill:none;stroke-width:1.9;stroke-linecap:round;stroke-linejoin:round}
.navgroup a:hover{background:var(--side-hover)}
.navgroup a[aria-current="page"]{background:var(--side-hover);color:var(--side-gold);font-weight:700}
.navgroup a[aria-current="page"]::before{content:"";position:absolute;left:-.85rem;top:.55rem;bottom:.55rem;width:3px;border-radius:0 3px 3px 0;background:var(--side-gold)}
.sidebar .sidefoot{margin-top:auto;padding:1rem .7rem 0;border-top:1px solid var(--side-line);font:500 .7rem/1.5 var(--mono);color:var(--side-muted)}
.shell{flex:1;min-width:0;display:flex;flex-direction:column}
header.top{position:sticky;top:0;z-index:10;display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:1rem 2.25rem;background:var(--topbar);backdrop-filter:saturate(1.4) blur(10px);-webkit-backdrop-filter:saturate(1.4) blur(10px);border-bottom:1px solid var(--line)}
header.top .crumb{display:block;font:600 .66rem/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--muted);margin-bottom:.35rem}
header.top h1{font-size:1.55rem;margin:0;font-weight:700;letter-spacing:-.025em;color:var(--ink);line-height:1.15}
.topactions{display:flex;align-items:center;gap:.5rem;flex:0 0 auto}
.chipbtn{display:inline-flex;align-items:center;gap:.5rem;min-height:44px;min-width:44px;justify-content:center;padding:0 .9rem;font:600 .78rem/1 var(--font);color:var(--ink);background:var(--surface);border:1px solid var(--ctl);border-radius:999px;cursor:pointer;transition:background-color .15s ease,border-color .15s ease}
.chipbtn:hover{background:var(--surface-2)}
.chipbtn svg{width:18px;height:18px;stroke:currentColor;fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}
.chipbtn.icon{padding:0;width:44px}
.livedot{width:8px;height:8px;border-radius:50%;background:var(--ok);box-shadow:0 0 0 0 var(--ok);animation:vitalsLive 2s ease-out infinite}
.chipbtn[aria-pressed="false"] .livedot{background:var(--ctl);animation:none}
@keyframes vitalsLive{0%{box-shadow:0 0 0 0 color-mix(in srgb,var(--ok) 55%,transparent)}100%{box-shadow:0 0 0 7px transparent}}
:root .theme-dark-icon{display:none}
@media (prefers-color-scheme: dark){:root:not([data-theme="light"]) .theme-dark-icon{display:block}:root:not([data-theme="light"]) .theme-light-icon{display:none}}
:root[data-theme="dark"] .theme-dark-icon{display:block}:root[data-theme="dark"] .theme-light-icon{display:none}
.main{width:100%;max-width:1120px;padding:1.75rem 2.25rem 3rem}
#vitals-main:focus{outline:none}

/* ---- content ------------------------------------------------------ */
.card{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1.2rem 1.4rem;margin-bottom:1rem;box-shadow:var(--shadow)}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:1rem;margin-bottom:1.25rem}
.rescard{position:relative;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:1.05rem 1.15rem;text-decoration:none;color:inherit;display:block;box-shadow:var(--shadow);transition:box-shadow .2s ease,transform .2s ease,border-color .2s ease}
a.rescard:hover{box-shadow:var(--shadow-hover);border-color:var(--line-strong);transform:translateY(-1px)}
.rescard h3{font:600 .68rem/1.2 var(--mono);margin:0 0 .6rem;color:var(--muted);text-transform:uppercase;letter-spacing:.09em}
.rescard .val{font-size:1.85rem;font-weight:700;line-height:1;letter-spacing:-.03em;font-variant-numeric:tabular-nums}
.rescard .val.warn{color:var(--warn-ink)}
.rescard .val.crit{color:var(--crit-ink)}
.bar{height:6px;border-radius:999px;background:var(--surface-2);overflow:hidden;margin:.7rem 0 .55rem}
.bar>span{display:block;height:100%;background:var(--accent);border-radius:999px}
.bar.warn>span{background:var(--warn)}
.bar.crit>span{background:var(--crit)}
.rescard .spark{display:block;width:100%;height:26px;margin:.15rem 0 .5rem}
.rescard .detail{font-size:.78rem;color:var(--muted);line-height:1.45;overflow-wrap:anywhere}
.qa{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:.75rem}
.qa a{display:flex;flex-direction:column;gap:.3rem;min-height:44px;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:.9rem 1rem;text-decoration:none;color:inherit;box-shadow:var(--shadow);transition:box-shadow .2s ease,border-color .2s ease}
.qa a:hover{box-shadow:var(--shadow-hover);border-color:var(--line-strong)}
.qa .qname{font-weight:700;color:var(--ink);font-size:.9rem}
.qa .qdesc{font-size:.78rem;color:var(--muted);line-height:1.4}
.sectiontitle{display:flex;align-items:center;gap:.6rem;font:600 .68rem/1.2 var(--mono);text-transform:uppercase;letter-spacing:.1em;color:var(--muted);margin:1.75rem 0 .75rem}
.sectiontitle::after{content:"";flex:1;height:1px;background:var(--line)}
.sectiontitle:first-child{margin-top:0}
.caption{font-size:.78rem;color:var(--muted);line-height:1.45;margin:-.3rem 0 .7rem}
.modelcard{display:flex;justify-content:space-between;align-items:center;gap:1rem;background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:.9rem 1.1rem;margin-bottom:.6rem;box-shadow:var(--shadow)}
.modelcard .mname{font-weight:700;font-size:.9rem}
.modelcard .msub{font-size:.78rem;color:var(--muted);margin-top:.15rem}
.toolgrid{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:.75rem}
.toolcard{background:var(--surface);border:1px solid var(--line);border-radius:var(--radius);padding:.9rem 1rem;box-shadow:var(--shadow)}
.toolhead{display:flex;justify-content:space-between;align-items:center;margin-bottom:.25rem;gap:.5rem}
.toolname{font-weight:700;font-size:.9rem}
.toolcat{font:500 .68rem/1.3 var(--mono);letter-spacing:.06em;text-transform:uppercase;color:var(--muted);margin-bottom:.4rem}
.tooldesc{font-size:.8rem;color:var(--muted);line-height:1.45}
.pill{display:inline-flex;align-items:center;gap:.35rem;font-size:.72rem;font-weight:700;border-radius:999px;padding:.22rem .65rem;white-space:nowrap;border:1px solid transparent}
.pill.ok{color:var(--ok-ink);background:var(--ok-bg);border-color:color-mix(in srgb,var(--ok) 35%,transparent)}
.pill.warn{color:var(--warn-ink);background:var(--warn-bg);border-color:color-mix(in srgb,var(--warn) 40%,transparent)}
.pill.crit{color:var(--crit-ink);background:var(--crit-bg);border-color:color-mix(in srgb,var(--crit) 40%,transparent)}
.pill.muted{color:var(--muted);background:var(--surface-2)}
.rtsection{background:var(--surface);border:1px solid var(--line);border-top:3px solid var(--gold);border-radius:var(--radius);padding:1.1rem 1.25rem;margin-bottom:1.4rem;box-shadow:var(--shadow)}
.rtsection.k8s{border-top-color:var(--k8s)}
.rtsection-head{display:flex;flex-wrap:wrap;align-items:center;gap:.55rem;font-weight:700;font-size:1rem;color:var(--ink);margin-bottom:.8rem}
.rtsection.k8s .rtsection-head{color:var(--k8s-ink)}
.rtsection-head svg{width:18px;height:18px;stroke:var(--gold);fill:none;stroke-width:2;stroke-linecap:round;stroke-linejoin:round;flex:0 0 auto}
.rtsection.k8s .rtsection-head svg{stroke:var(--k8s)}
.rtsection-head .ep{font:500 .78rem var(--mono);color:var(--muted)}
.rtchip{font:700 .62rem/1 var(--mono);letter-spacing:.08em;text-transform:uppercase;border-radius:4px;padding:.25rem .45rem;background:var(--ok-bg);color:var(--accent)}
.rtchip.k8s{background:var(--k8s-bg);color:var(--k8s-ink)}
.rttoggle{display:flex;flex-wrap:wrap;align-items:center;gap:.45rem;margin:0 0 1.25rem}
.rtlabel{font:600 .66rem/1 var(--mono);text-transform:uppercase;letter-spacing:.1em;color:var(--muted);margin-right:.3rem}
.rtpill{display:inline-flex;align-items:center;min-height:44px;font-size:.82rem;font-weight:600;text-decoration:none;color:var(--ink);background:var(--surface);border:1px solid var(--ctl);border-radius:999px;padding:0 1rem}
.rtpill:hover{background:var(--surface-2)}
.rtpill.on{background:var(--ok-bg);border-color:var(--accent);color:var(--accent)}
.verdict{display:flex;align-items:center;gap:.9rem;border-radius:var(--radius);padding:1rem 1.2rem;margin-bottom:1.4rem;border:1px solid;border-left-width:4px}
.verdict.ok{background:var(--ok-bg);border-color:var(--ok)} .verdict.warning{background:var(--warn-bg);border-color:var(--warn)} .verdict.critical{background:var(--crit-bg);border-color:var(--crit)}
.verdict b,.verdict strong{font-size:1.02rem}
.dot{width:.7rem;height:.7rem;border-radius:50%;flex:0 0 auto}
.verdict.ok .dot{background:var(--ok)} .verdict.warning .dot{background:var(--warn)} .verdict.critical .dot{background:var(--crit)}
.summary{font:.84em var(--mono);color:var(--muted);font-variant-numeric:tabular-nums}
.finding{border-left:3px solid var(--line-strong);padding:.35rem 0 .35rem .9rem;margin-bottom:.8rem}
.finding.warning{border-color:var(--warn)} .finding.critical{border-color:var(--crit)}
.finding .title{font-weight:700}
.finding .detail{color:var(--muted);font-size:.9em}
.finding .fix{font-size:.86em;margin-top:.25rem}
.finding .fix code,.card :not(pre)>code{background:var(--surface-2);color:var(--ink);padding:.1em .4em;border-radius:4px}
.row{display:flex;justify-content:space-between;gap:1rem;padding:.5rem 0;border-bottom:1px solid var(--line);font-variant-numeric:tabular-nums}
.row:last-child{border-bottom:none}
.row .k{color:var(--muted)}
.path{padding:.45rem 0 .45rem 1rem;border-bottom:1px solid var(--line)}
.path:last-child{border-bottom:none}
.path .dir{color:var(--muted);font-size:.8em;overflow-wrap:anywhere}
.path .name{font-weight:600;overflow-wrap:anywhere}
.main table{font-variant-numeric:tabular-nums}
.main th{font:600 .66rem/1.2 var(--mono);text-transform:uppercase;letter-spacing:.08em;color:var(--muted);padding:.55rem .5rem;border-bottom:1px solid var(--line-strong)}
.main td{padding:.5rem;border-bottom:1px solid var(--line)}
.main tr:last-child td{border-bottom:none}
.unavailable{color:var(--muted);font-style:italic}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:.5rem;min-height:44px;font:inherit;font-weight:700;color:var(--btn-ink);background:var(--btn);border:1px solid var(--btn);border-radius:var(--radius);padding:0 1.25rem;cursor:pointer;transition:background-color .15s ease,box-shadow .15s ease}
.btn:hover{background:var(--btn-hover);box-shadow:var(--shadow-hover)}
.btn:disabled{opacity:.6;cursor:default;box-shadow:none}
input[type="checkbox"],input[type="radio"]{accent-color:var(--gold);width:18px;height:18px}
label{cursor:pointer}
/* Form fields and plain (secondary) buttons — everything that isn't the
   gold primary .btn. 44px tall for AAA target size; --ctl outlines hold
   3:1 against the card so the field boundary is visible (1.4.11). */
.main input:not([type="checkbox"]):not([type="radio"]),.main select{font:inherit;font-size:.88rem;color:var(--ink);background:var(--surface);border:1px solid var(--ctl);border-radius:var(--radius);min-height:44px;padding:0 .75rem;max-width:100%}
.main input::placeholder{color:var(--muted);opacity:1}
.main input[type="number"]{width:5.5rem}
.main button:not(.btn){display:inline-flex;align-items:center;justify-content:center;min-height:44px;font:inherit;font-size:.86rem;font-weight:600;color:var(--ink);background:var(--surface);border:1px solid var(--ctl);border-radius:var(--radius);padding:0 1rem;cursor:pointer;transition:background-color .15s ease}
.main button:not(.btn):hover{background:var(--surface-2)}
.main button:not(.btn):disabled{opacity:.6;cursor:default}
.main form{display:flex;flex-wrap:wrap;align-items:center;gap:.75rem 1rem;margin:.9rem 0}
.main form label{display:inline-flex;align-items:center;gap:.5rem;font-size:.88rem;color:var(--muted)}
.main form .caption{flex-basis:100%;margin:0}
.main fieldset{display:flex;flex-wrap:wrap;gap:0 1.25rem;border:1px solid var(--line-strong);border-radius:var(--radius);padding:.5rem 1rem .6rem;margin:1rem 0}
.main legend{font:600 .66rem/1 var(--mono);text-transform:uppercase;letter-spacing:.1em;color:var(--muted);padding:0 .4rem}
.main fieldset label{display:inline-flex;align-items:center;gap:.5rem;min-height:44px;font-size:.9rem;color:var(--ink)}
.card>label{display:flex;flex-wrap:wrap;align-items:center;gap:.4rem 1rem;margin:.7rem 0;font-size:.88rem;color:var(--muted)}
.card>.btn+.btn{margin-left:.5rem}
footer{color:var(--muted);font-size:.78rem;line-height:1.6;margin-top:2.5rem;padding-top:1.25rem;border-top:1px solid var(--line)}
#vitals-main.refreshing{animation:vitalsPulse .3s ease-out}
@keyframes vitalsPulse{from{opacity:.6}to{opacity:1}}

/* ---- small screens: the sidebar becomes a top bar with a scrolling nav */
@media (max-width:860px){
  .app{flex-direction:column}
  .sidebar{position:relative;width:auto;height:auto;flex:none;padding:.9rem 1rem .6rem;gap:.7rem;overflow:hidden}
  .sidebar::before{display:none}
  .brand{padding:0 0 .7rem}
  .sidebar nav{flex-direction:row;gap:.25rem;overflow-x:auto;scrollbar-width:none;margin:0 -1rem;padding:0 1rem .35rem}
  .navgroup{display:flex;gap:.25rem}
  .navgroup h4{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}
  .navgroup a{white-space:nowrap}
  .navgroup a[aria-current="page"]::before{left:.7rem;right:.7rem;top:auto;bottom:0;width:auto;height:3px;border-radius:3px 3px 0 0}
  .sidebar .sidefoot{display:none}
  header.top{padding:.85rem 1rem}
  header.top h1{font-size:1.25rem}
  .main{padding:1.25rem 1rem 2.5rem}
}
@media (prefers-reduced-motion:reduce){*,*::before,*::after{animation:none!important;transition:none!important}}
</style>
</head>
<body>
<a class="skip" href="#vitals-main">Skip to content</a>
<div class="app">
<aside class="sidebar">
<div class="brand"><span class="mark" aria-hidden="true"></span><div><b>vitals</b><span>Local diagnostics</span></div></div>
<nav aria-label="Primary">{{.Nav}}</nav>
<div class="sidefoot">{{.Version}}<br>Served locally — nothing leaves this machine</div>
</aside>
<div class="shell">
<header class="top">
<div><span class="crumb">vitals dashboard</span><h1>{{.Title}}</h1></div>
<div class="topactions">
{{if .Live}}<button id="vitals-refresh-toggle" class="chipbtn" type="button" aria-pressed="true"><span class="livedot" aria-hidden="true"></span><span class="lbl">auto-refresh: on</span></button>{{end}}
<button id="vitals-theme-toggle" class="chipbtn icon" type="button" aria-label="Switch color theme" title="Switch color theme"><svg class="theme-light-icon" viewBox="0 0 24 24" aria-hidden="true">` + iconMoon + `</svg><svg class="theme-dark-icon" viewBox="0 0 24 24" aria-hidden="true">` + iconSun + `</svg></button>
</div>
</header>
<div class="main">
<main id="vitals-main"{{if .Live}} data-live="1"{{end}} tabindex="-1">{{.Body}}</main>
<footer>vitals {{.Version}} — served locally, nothing leaves this machine. Press Ctrl+C in the terminal that launched it to stop.<br>Issues or feedback: <a href="https://github.com/gautampachnanda101/vitals">github.com/gautampachnanda101/vitals</a></footer>
</div>
</div>
</div>
<script>
/* Theme toggle: flips between light and dark and pins the choice. With no
   pin the page follows the OS setting. */
(function(){
  var btn=document.getElementById('vitals-theme-toggle'), root=document.documentElement;
  if(!btn)return;
  function current(){
    var t=root.getAttribute('data-theme'); if(t)return t;
    return window.matchMedia&&matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';
  }
  function label(){ var next=current()==='dark'?'light':'dark'; btn.setAttribute('aria-label','Switch to '+next+' theme'); btn.title='Switch to '+next+' theme'; }
  btn.addEventListener('click',function(){
    var next=current()==='dark'?'light':'dark';
    root.setAttribute('data-theme',next);
    try{localStorage.setItem('vitals.theme',next);}catch(e){}
    label();
  });
  label();
})();
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
  function paint(){
    if(!toggle)return;
    var lbl=toggle.querySelector('.lbl')||toggle;
    lbl.textContent='auto-refresh: '+(off()?'off':'on');
    toggle.setAttribute('aria-pressed',off()?'false':'true');
  }
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
// in-progress result, or in-flight async client fetch) that an innerHTML
// replacement would destroy.
var noLiveRefresh = map[string]bool{"clean": true, "dupes": true, "disk": true, "advice": true}

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
