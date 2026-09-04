# vitals — product design & architecture

[docs](../index.md) / **Architecture**

Status: **reviewed.** Six independent reviews (3 technical architects, 1
security architect, 1 product manager, 1 QA lead) assessed this design and
the working prototype it describes. All six independently returned
**go-with-changes** — the architecture is validated, with a specific,
convergent list of fixes required before the implementation plan's Phase 1
is built out. See `docs/roadmap/index.md` for the resulting task list;
this document is the architecture and the record of what was decided and
why.

## 1. What vitals is today

A single static Go binary, `gopsutil` for system data plus a small
terminal-reliability group (`go-isatty`/`go-colorable`/`golang.org/x/term`,
added 2026-09-03 for genuine cross-platform color/width detection — see
AGENTS.md's "One dependency" principle for why this specific exception
exists), cross-platform (macOS/Linux/Windows), no phone-home. It complements — does
not replace — btop/glances/ncdu/nvtop by adding cross-resource
correlation, remediation, and local/cloud LLM diagnostics on top of the
same underlying data.

**Commands shipped**: `doctor` (cross-resource verdict + fix, exit
0/1/2), `cpu`/`mem`/`disk`/`net`/`power` (per-resource deep dive), `gpu`,
`top`, `memhogs`, `memcheck`, `llm` (+ `llm fit <model>`), `advice`
(LLM-generated plain-English interpretation of a `doctor` report), `clean`
(cache/log/trash cleanup, dry-run first), `dupes` (duplicate-file finder,
`--hardlink` opt-in), `tools`/`explore`/`live` (detect/install/hand off to
companion tools), `serve`/`export` (Prometheus metrics, OTel semconv
names), `mcp` (Model Context Protocol server, read-only by construction),
`guide` (embedded user guide, `--web` renders it in a browser), `help`,
`completion`.

**Packages**: `internal/{ui,diag,doctor,gpu,llm,metrics,mcp,monitor,
memhogs,memcheck,clean,dupes,tools,advice,guide,config,help,dashboard}`.
Architecture rule throughout: `Collect`/live-glue functions are impure and
thin; the logic that matters (`Analyze`, `render*`, parsers) is pure and
unit-tested from fixtures. See `AGENTS.md` for the full list of
non-negotiable conventions this design stays consistent with.

## 2. North star

> Lightning fast, yet the best product, that runs on any machine — all
> three at once. A feature that needs a second heavy dependency, breaks a
> platform, or adds noticeable startup/runtime latency needs a strong
> justification against this, not just "it'd be useful."

Every decision below is evaluated against this explicitly — it's also
exactly the bar the review found the first dashboard prototype falling
short of on latency (§6.4), which is why that's a required fix, not a
nice-to-have.

## 3. How this design came about (condensed)

1. Deep CLI feature work across every resource — all TDD, all shipped
   (§1's command list).

2. A multi-persona critical review (systems engineer, infra/SRE, technical
   sales, a non-technical end user, an experienced power user), grounded
   in real competitor research (btop, glances, ncdu/gdu/dust,
   node_exporter+Grafana, Datadog, New Relic, osquery, Wazuh, CleanMyMac X,
   CCleaner, McAfee/Norton utilities, the MCP ecosystem) and a
   source-level security audit. Two of five personas came back "not for
   me" / "not yet."

3. Security and coverage findings from that review were fixed directly in
   code (§4) before this design work began.

4. Told the review still showed too many rejections, direction became:
   close the real gaps, don't just re-word verdicts.

5. Asked what "a complete product" requires: **all of** a real GUI, the
   in-session web-view work finished, and a real product site.

6. On the GUI specifically: **the binary itself is the product and can
   serve content** — no separate GUI toolkit/app — and the whole surface
   must be capability-gated (no "Advice" page with no LLM reachable) and a
   genuinely extensible plugin architecture, not a hardcoded page list.

7. A prototype (`internal/dashboard/`) was built to make that concrete,
   then six independent agents reviewed the design and the prototype
   together before any further implementation — that review is
   incorporated below.

## 4. Security & quality fixes already shipped (baseline, not under review)

- `vitals serve` now binds `127.0.0.1:9100` by default (was `0.0.0.0`,
  unauthenticated) — `internal/metrics/serve.go`.

- `--webhook` refuses plain http and loopback/private/link-local targets
  (including cloud metadata) by default, checked at dial time (defeats DNS
  rebinding on this path); `--webhook-allow-insecure` opts out —
  `internal/doctor/webhook.go`.

- `clean`'s `purgeContents` refuses to purge through a symlinked or
  non-directory path (`Lstat` guard before `ReadDir`) —
  `internal/clean/clean.go`.

- `vitals advice` renders replies through the Markdown→ANSI renderer
  instead of dumping raw Markdown; its prompt asks the model to connect
  findings sharing a root cause instead of restating each one.

- Per-package coverage floors (`check_coverage.py`) replace one blended
  38% number; `AGENTS.md` states 95%+ as the target for a package's
  pure/testable logic.

## 5. "A complete product" — the three-part scope

### 5.1 A real GUI — realized as: the binary serves a local web app

No Electron/Tauri/Wails/Fyne. `vitals dashboard` starts a loopback HTTP
server from the same static binary — no new dependency, no CGO, no second
build target, no installer/code-signing pipeline. Server-rendered Go,
system fonts, inline CSS, works fully offline. Full architecture in §6.

### 5.2 Finishing the in-session web-view work

The `vitals guide --web` pattern (loopback server + browser auto-open) is
generalized into shared plumbing (`guide.ServeLocal`, `guide.RenderFragment`)
that both the guide and the dashboard now use. Reviewer note (PM): calling
this "finished" is fair for the shared mechanism, but should not be read
as a verified checklist of every web-view idea discussed in-session — only
the guide and the advice page are concretely covered.

### 5.3 A real product site

A separate, static, public artifact: install instructions, feature tour,
the competitive comparison table from the persona review, links to
`USERGUIDE.md`. Proposed home: `docs/index.html` for GitHub Pages. Unlike
the dashboard, this is expected to be viewed with network access, so it
isn't bound by the offline/no-external-resource rule — reusing the
established visual language (teal accent, severity colors) for brand
consistency regardless.

**Sequencing correction from review (PM)**: originally scoped as a later
phase; the review found this backwards — the site has zero dependency on
the dashboard shipping, and is the only piece of this whole plan that can
reach the non-technical persona before they ever touch a terminal. See
`docs/roadmap/index.md`.

## 6. Dashboard architecture

### 6.1 One binary, one new subcommand

`vitals dashboard [--addr 127.0.0.1:0] [--no-open]`: loopback-only HTTP
server (never `0.0.0.0` — same posture as `vitals guide --web` and the
now-fixed `vitals serve`), renders a small multi-page app, opens the
user's default browser. `net/http` and `html/template` are standard
library — no frontend framework, no bundler, no JS dependency.

Reviewer correction (3 architects): "no new dependency" is a true claim
about the build graph, but was being read as "no new risk," which doesn't
hold — every install now compiles in a listening HTTP server (off by
default), and the dashboard is a materially richer target than the
metrics/guide servers it borrows its trust model from. §6.5 below is the
resolved trust model, not the original "loopback is enough" assumption.

### 6.2 Plugin/module registry (prototyped: `internal/dashboard/module.go`)

```go
type Module struct {
    Slug      string                 // URL path segment; "" = root/overview
    NavLabel  string
    Order     int                    // nav position
    Available func(PageContext) bool // can THIS machine offer this page right now?
    Render    func(PageContext) string
}

func Register(m Module) { registry = append(registry, m) } // panics on a duplicate Slug — see §6.6

// AsyncFragment: a GET-only, on-demand HTML fragment for request-scoped
// work too slow to block a page's own render on (2026-09-04, see below).
type AsyncFragment struct {
    Path    string // full URL path, e.g. "/advice/commentary"
    Handler func(PageContext) (status int, body string)
}

func RegisterAsync(a AsyncFragment) { asyncFragments = append(asyncFragments, a) } // panics on a duplicate Path
```

Each page is a separate file that calls `Register(...)` in its own
`init()` — adding a page means adding a file, registering slug/nav/
availability/render, nothing else in the package changes. Verified today
by `TestModulesRegisterThemselvesWithDistinctSlugs`, which runs against
the real registry, not a fixture.

**Review finding (2 architects), resolved, later superseded (2026-09-04)**:
the original design had no `Prepare` hook, so the advice route's need for
an LLM call before rendering was going to be a hardcoded
`if slug == "advice"` special case in the HTTP handler — directly
contradicting "nothing else changes." `Prepare func(*PageContext) error`,
called uniformly by the router for whichever module matched the request,
closed this: advice's `Prepare` called `llm.Complete` and populated
`AdviceReply`/`AdviceErr`; every other module's `Prepare` was nil
(skipped).

**Real bug found in production (2026-09-04): `Prepare` ran synchronously
inside `route()`, before `Render` — so a slow or unreachable LLM (up to
`completeTimeout`, 60s) blocked the *entire* advice page, including the
heuristic findings half that needs no LLM at all and is supposed to be
the immediate, always-available answer (see the `advice`
heuristic-first-LLM-complements principle elsewhere in this doc). A user
saw this as "the Advice page is just blank." `Prepare` is removed;
replaced by `AsyncFragment` (`internal/dashboard/module.go`), a small
registry parallel to `WriteAction` (§ below) for exactly this shape —
request-scoped work too slow to do inline. A module registers a path
(`RegisterAsync(AsyncFragment{Path: "/advice/commentary", Handler: ...})`)
that `route()` dispatches *before* module lookup, returning a bare HTML
fragment instead of a full page; the module's own `Render` emits a
placeholder plus a small vanilla-JS `fetch()` (no framework, same
convention `modules_clean.go`'s Preview/Apply buttons already use) that
swaps the fragment in once it resolves. This keeps "adding a page means
adding a file, nothing else changes" true for *this* shape too — the
next module with slow request-scoped work registers its own
`AsyncFragment`, not a new hardcoded path check — while making it
structurally impossible for a page's own render to block on it.

**Review finding (architect A), resolved**: `Register` must panic on a
duplicate `Slug` (matching `http.ServeMux.Handle`'s own behavior for a
duplicate pattern) instead of silently leaving the second registration
permanently unreachable. A codebase-wide invariant should fail at process
start, not depend on a specific test being run.

### 6.3 Capability gating

`Available(ctx PageContext) bool` is how "no advice with no LLM
configured" is enforced — not a hardcoded `if` in a template, but a
property every module declares:

- `Always` — overview, CPU, memory, disk, network.
- `HasBattery` — the power page; hidden on a desktop/headless VM.
- `HasGPU` — the GPU page; hidden with no GPU detected.
- `AnyLLMReachable` — the advice page; hidden with no local/cloud LLM
  reachable, mirroring `vitals advice`'s own "never assume a runtime is
  installed" rule.

A module that exists but isn't available reports that distinctly from an
unknown route, so the handler can render "Advice isn't available — no LLM
reachable" instead of a bare 404.

### 6.4 Request lifecycle and the latency fix (review-required change)

Original design: every request ran `doctor.Collect()` and
`llm.probeProviders()` (to decide nav visibility) from scratch. **Review
finding (4 of 6 reviewers, independently)**: `Collect()` has a hardcoded
~700ms window plus GPU/disk subprocess calls that can legitimately run to
several seconds on real, plausible machine states (a bad GPU driver, a
firewalled local LLM port); `probeProviders` was a **sequential** loop
over up to 4 local + N cloud targets at 3-4s timeout each — a realistic
worst case over 20 seconds, paid on every nav click regardless of which
page was clicked. This directly violates §2's north star and was not
budgeted for anywhere in the original design.

**Resolved design**: a short-TTL (a few seconds) cached snapshot, refreshed
by at most one in-flight `Collect()`/`probeProviders()` call at a time
(`singleflight`-style guard — no extra dependency, this is ~20 lines of
Go), shared across concurrent requests. Provider probes run **concurrently
per target**, not sequentially. This is the prerequisite for ever safely
adding auto-refresh (§6.8) — polling against the uncached cost model would
mean spamming local LLM servers and spawning subprocesses in the
background for a tab that isn't even focused.

### 6.5 Trust model (review-required changes — this is the biggest single revision)

Original design's trust model was "loopback bind = safe," inherited
wholesale from `vitals guide --web`/`vitals serve`. **Security review
finding**: this is insufficient for what the dashboard actually serves,
and insufficient full stop, for two concrete reasons found independently:

1. **No Host-header check.** `ServeLocal` binds `127.0.0.1` but never
   validates the incoming `Host` header. This means DNS rebinding
   (an external page rebinds its own origin's DNS to 127.0.0.1 with a
   short TTL, then `fetch()`s the now-same-origin loopback server)
   defeats the loopback bind entirely — any external website can read
   live process/path/resource data and LLM output through the victim's
   own browser. **Resolved**: `ServeLocal` rejects any request whose
   `Host` header isn't the actual bound `127.0.0.1:<port>` (or
   `localhost:<port>`), before it reaches any handler. This retroactively
   hardens `guide --web` too.

2. **No CSRF/Origin protection.** Even without DNS rebinding, any other
   tab/extension already open in the browser can blind-request every
   dashboard route. For read-only GETs in Phase 1 this is a
   confidentiality risk (mitigated by the Host-header fix above, since a
   normal cross-origin page can't set an arbitrary Host header and blind
   requests to the real `127.0.0.1:<port>` origin still can't be *read*
   cross-origin without CORS); it becomes a much more serious integrity
   risk the moment any state-changing (POST) route exists. **Resolved
   as a decision, not deferred**: no write/mutating endpoint (§5.3 of
   `docs/roadmap/index.md`, "write actions") ships without a same-origin
   check (Origin/Sec-Fetch-Site header validation) in addition to the
   Host-header fix. This is now a named gate on that future phase, not an
   open question.

**Additional security finding, resolved**: the nav-building provider probe
(§6.4) was making **real, credentialed** HTTPS requests to every
configured cloud LLM provider on every page load — combined with the
Host-header gap, an attacker page could trigger authenticated API calls
blind, burning quota with zero user action, purely as a side effect of
loading an `<img>` tag. The latency fix in §6.4 (caching, not re-probing
per request) resolves this as the same fix, not a separate one.

### 6.6 Markdown/XSS handling (review-required change)

**Finding (3 of 6 reviewers, independently, converging on the same root
cause)**: `guide.RenderFragment` (used to render LLM advice output) calls
`html.EscapeString` before reinjecting Markdown links as `<a href="...">`
— this blocks tag/attribute-breakout injection but does **not** validate
the URL scheme, so a `javascript:`/`data:` URL in an LLM reply survives
into a live, clickable `href`. This code was written and tested against
vitals' own trusted, static `USERGUIDE.md`; the dashboard's advice page is
the first place *uncontrolled* (model-generated, and per `advice.go`'s
prompt, influenced by scannable process names) Markdown reaches it live.

**Resolved**: `renderInlineHTML`'s link substitution allow-lists
`http:`/`https:`/`mailto:` schemes and drops/neutralizes anything else,
before emitting an `href`. A test pinning this (a markdown link with a
`javascript:` URL must not produce a clickable `href`) is required in
`internal/guide`, not just `internal/dashboard`, since the fix belongs to
the shared renderer.

### 6.7 What this design deliberately does not include (Phase 1)

- No GUI toolkit/CGO/second build target — the north star reason (§2).
- No destructive actions (clean/dupes) triggerable from the dashboard —
  gated behind the CSRF/Origin decision in §6.5 being implemented first,
  per `docs/roadmap/index.md`.

- No dynamically-loaded third-party plugins. **Resolved per security
  review**: keep this compile-time-only permanently, not just for now — a
  runtime plugin loader (Go `plugin`, WASM, subprocess) for a tool that
  already shells out with the user's own privileges would be a severe,
  unjustified expansion of attack surface for speculative benefit.

- No auto-refresh/websocket in Phase 1 — explicitly gated on §6.4's
  caching fix landing first (auto-refresh against the uncached cost model
  would be actively harmful, not just premature).

### 6.8 Accessibility: WCAG 2.2 Level AAA

Standing requirement, not a phase-specific task: every page the dashboard
serves targets WCAG 2.2 Level AAA, not just AA. Concretely, so this stays
checkable rather than a vibe:

- **Contrast (1.4.6, AAA)**: every color used as *text* holds >=7:1
  against every background it can actually appear on — checked in code,
  not eyeballed (`internal/dashboard/render_test.go`'s
  `TestPaletteMeetsWCAGAAAForNormalText`, which computes real WCAG
  relative luminance/contrast ratios and fails the build if a future
  palette edit regresses below 7:1). Colors used only for borders or a
  small decorative status dot (`--warn`, `--crit`) are held to the
  non-text 3:1 rule (AA) instead, since AAA's stricter ratio is a
  text-contrast requirement — they're not text here and a fresh reviewer
  should not "fix" them expecting the same 7:1 bar.
- **Keyboard operability**: `:focus-visible` gets an explicit, high-
  contrast outline — the browser default is not trusted to be visible
  against every theme.
- **Semantic structure**: real landmarks (`<nav aria-label="Primary">`,
  `<main>`, `<header>`, `<footer>`), and the current nav item is marked
  with `aria-current="page"` (both the ARIA signal for assistive tech and
  the sole styling hook — one attribute, not a class that could drift out
  of sync with it).
- **Not yet verified**: this section covers what the *shared* page shell
  (`layout`, `verdictBanner`, `findingsList`, `row`) can guarantee today.
  Item 002 (the actual HTTP handler + real served pages) must re-check
  this once real interactive elements (any future buttons/forms in item
  005's write actions especially) exist — AAA compliance for a static
  shell doesn't automatically extend to a form control or a modal.

### 6.9 Testing requirements (from QA review)

- `internal/dashboard` must have an entry in `check_coverage.py`'s
  `FLOORS` — its absence today is a real, reproduced CI failure, not a
  hypothetical.

- Every `render*` function needs coverage for its non-empty-value branches
  (found at 0% for `renderMem`/`renderPower` in the prototype) — the exact
  bug class (`AGENTS.md`'s own stated rationale for the 95% policy) this
  repo has already shipped twice.

- The routing/PageContext-assembly/module-dispatch logic in the (not yet
  written) HTTP handler must be extracted into pure, unit-tested functions
  — not left inline in the handler closure — so it doesn't become the next
  `internal/monitor.emit()`-shaped untested seam.

- A real integration/smoke test is required for `vitals dashboard`
  (start on an ephemeral port with `--no-open`, hit a few routes including
  a deliberately-unavailable one, send `os.Interrupt`, assert clean
  shutdown) — none of vitals' existing server-ish commands
  (`serve`/`mcp`/`guide --web`) currently have this, and the dashboard's
  larger routing surface means it shouldn't inherit that gap.

## 7. Review outcome

Three independent technical architects, a security architect, a product
manager, and a QA lead each reviewed this design and the
`internal/dashboard/` prototype independently. **All six returned
go-with-changes** — no reviewer rejected the architecture; every reviewer
found the plugin/capability-gating/Collect-Analyze-Render pattern sound
and consistent with the rest of the codebase. The changes above (§6.4–6.8)
are the full, de-duplicated set of required fixes across all six reviews,
each resolved as a decision here rather than left as an open question.
Non-blocking secondary findings (a low-impact race on `disk_history.json`,
`guide`'s own server functions never having test coverage, minor
duplicated logic between `modules_overview.go`/`modules_resource.go`) are
tracked as follow-ups in `docs/roadmap/index.md` rather than gates.

See `docs/roadmap/index.md` for the phased build order, including the
sequencing correction from the PM review (product site in parallel with
Phase 1, not after it).
