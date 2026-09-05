# Design — 011 Console at-a-glance view

[docs](../../../index.md) / [Roadmap](../../index.md) / [011 — Console at-a-glance view](index.md) / **Design**

**Status: draft, pre-review.** This is the design doc `AGENTS.md`'s
"Roadmap discipline" requires before any code — a one-screen panelled
console view is a structurally new user-facing output surface, and (if
it ever goes live-refreshing) potentially a new dependency, so it gets
a full `review-panel` pass before implementation, per this item's
`index.md`. The sections below are the proposal as submitted for that
review; an "As built" section will be appended after implementation,
mirroring [005's design.md](../005-dashboard-write-actions/design.md).

## 1. What the view is, and what it is not

`vitals` in a terminal is print-and-exit: `doctor` prints a verdict and
findings, each resource command prints its own block, and every one
returns. Nothing shows the whole machine on one screen.

This adds **one screen**: `doctor`'s verdict and findings at the top, in
colour, followed by tiled panels for every subsystem `doctor` already
measures — CPU/memory/swap/load, per-interface network, disk I/O and
capacity, sensors/battery, containers when present, and a truncated
per-process table — sized to fit a normal terminal without scrolling.

It is **not**:

- **a live process monitor.** No refresh loop in v1 (§3), no arrow-key
  navigation, no per-process actions. `vitals live` already hands off to
  a dedicated monitor for people who want that; this view's reason to
  exist is the verdict layer on top, not the gauges
  (the product's core "diagnostic layer" bet).
- **a re-theme of the existing renderers.** `doctor.PrintFindings` /
  `internal/ui` stay exactly as they are for the individual commands.
  This is a separate renderer (§5), not a restyle
  (the interactive-TUI direction notes settled that further styling of
  the one-shot renderers does not close this gap).
- **a copy of any existing monitor.** §8. The information architecture
  is borrowed — one screen, subsystem panels, colour-coded monospace
  tables, a persistent summary line — because it is genuinely the right
  way to read a machine. No layout, panel name, key map, colour choice
  or wording is lifted from a specific tool.

## 2. Invocation

**Proposed: `vitals` with no subcommand renders this view.** Today a
bare `vitals` prints usage; a first-time user typing the bare command
expects *something about their machine*, not a man page. Usage moves to
`vitals help` / `vitals -h` (both already work).

Rejected alternatives, for the review to confirm:

- `vitals view` / `vitals dash` subcommand — discoverable, but a bare
  `vitals` still printing usage wastes the most obvious entry point.
- `vitals doctor --view` flag — couples the view to `doctor`'s own
  flag surface and implies it's a `doctor` mode rather than the
  default face of the tool.

A `--json`-style consumer never wants this; it's a TTY-only view. When
stdout is not a terminal, a bare `vitals` falls back to today's
behaviour (usage text) so scripts and pipes are unaffected.

## 3. Static snapshot first; live refresh is a separate decision

**v1 is a single render and exit.** One `doctor.Assess` pass, lay it
out, print, return — the same lifecycle every other command has. This
needs **no new dependency**: the layout is text, composed with the
existing `internal/ui` width helpers (§5).

A live-refreshing version (clear screen, re-render every N seconds,
quit on `q`/Ctrl-C) is explicitly **out of scope for v1** and is its
own follow-on item, because it changes the cost and dependency profile
completely:

- it needs a refresh loop and raw-terminal input handling, which in Go
  realistically means a TUI library (bubbletea/tview) — a **second
  dependency**, directly contradicting the "one static binary, one
  dependency (`gopsutil`)" claim `docs/user-guide.md` and
  the product's core "diagnostic layer" bet both make. Adding it is a deliberate,
  reviewed trade, not a v1 detail.
- it re-runs collection on a timer, so the snapshot-cache discipline
  (§7) becomes load-bearing rather than merely tidy.

The review should confirm snapshot-first, and confirm that a future
live mode is gated on its own dependency-reconciliation review.

## 4. Layout

Vertical order, top to bottom — **verdict leads**, numbers support it:

1. **Header line** — hostname · OS/arch · uptime · vitals version.
   One line. (`info.Collect` already produces these; the field
   allowlist is 007's — hostname, OS, arch, cores — nothing
   fingerprint-grade.)
2. **Verdict banner** — `doctor`'s ranked verdict (OK / WARNING /
   CRITICAL) and the one-line summary, coloured. Identical wording to
   `vitals doctor`'s own verdict line.
3. **Findings** — every `diag.Finding` this pass raised, worst first,
   each with its `Fixes`. This is the point of the view; it is not
   truncated. If there are none: one green "nothing needs attention"
   line.
4. **Resource panels**, in a grid that reflows to terminal width
   (2-up on a normal terminal, 1-up on a narrow one):
   - **CPU** — used% (user/sys vs iowait vs steal split), load vs
     cores, package temp, top process.
   - **Memory** — used%, kernel available%, swap used% and in/out
     rates, top process.
   - **Disk** — per real mount: used%, free, and the device
     util/await/IOPS estimate; S.M.A.R.T. line when
     [010](../010-companion-tools-integration/) data is present.
   - **Network** — per active interface: rx/tx rates, link speed,
     retransmit%. Idle interfaces omitted (matches `vitals net`).
   - **Power** — source, charge%, time remaining (only when a battery
     exists).
   - **GPU** — per device: util%, VRAM used/total, temp (only when a
     GPU is detected).
5. **Process table** — top N by CPU then by memory (N chosen to fill
   the remaining rows, not a fixed 20), columns CPU% / RSS / PID /
   command. Data from `monitor.Sample` (§7).
6. **Footer line** — timestamp, and `vitals doctor --json` / `vitals
   <resource>` pointers for drilling in.

Panels for absent subsystems (no battery, no GPU) are simply not
rendered — same rule as the dashboard's nav gating.

**Fit, not scroll.** The renderer measures terminal height and drops
the lowest-priority content first (process-table rows, then whole
optional panels) so the verdict and findings are never pushed
off-screen. It never emits more rows than the terminal has. On an
unknown-size terminal (not a TTY, or `$LINES`/`ioctl` unavailable) it
uses a sensible default height and lets the pager/scrollback handle the
rest.

## 5. Rendering approach — no new dependency for v1

A new `internal/consoleview` package. Pure composition:

- input: a `doctor.Snapshot` + `diag.Report` (from `doctor.Assess`) and
  a `monitor.Snapshot` (from `monitor.Sample`) — passed in, so the
  layout is fixture-tested with no live calls, the same split
  `internal/dashboard` uses.
- a `Render(in Input, width, height int) string` entry point — pure,
  deterministic, returns the whole screen as a string; `main` prints
  it. Width/height are injected, so every layout branch (narrow, short,
  wide) is a table test.
- box-drawing and column alignment reuse `internal/ui`'s existing
  width-aware helpers (`ui.Wrap`, `DefaultWrapWidth`, `ui.Truncate`,
  `ui.GradeWidth`) — the same ANSI-aware measurement the resource
  commands already use. Panels are framed with a light box character
  set chosen here, not copied from a tool.
- colour goes through `internal/ui`'s existing severity palette
  (`ui.Okf`/`Warnf`/`Actionf`), so `NO_COLOR` / non-TTY already
  degrades correctly and the markdown-style extensibility
  the "heuristics first, LLM on top" rule already asked for stays
  intact.

No bubbletea, no tview, no termbox. If the review decides the snapshot
view is not worth building without the live mode, that's a valid
outcome — but the live mode's dependency question is then the thing to
review, separately.

## 6. Data source — one snapshot per render

`vitals` (no subcommand) does exactly:

1. `doctor.Assess(RunOptions{...})` — one collect + analyze + history
   append, same as `vitals doctor`.
2. `monitor.Sample(monitor.Options{Top: wide, SortBy: "cpu"})` — one
   process sample, same call the dashboard's Processes page makes.
3. `consoleview.Render(...)` with those two results and the measured
   terminal size.

No per-panel collection. No goroutine fan-out. The two calls above are
the entire cost, paid once. (This mirrors `internal/dashboard`'s
`snapshotCache.refresh` doing all gathering in one place — there's no
cache here because it's a single render, but the "gather once, render
pure" shape is the same.)

The **events strip** idea from `index.md` (findings that *just* crossed
a threshold, vs. steady-state findings) reads `doctor`'s existing
history file — no new persistence. If the review considers it scope
creep for v1, it drops to "findings, worst-first" with no
just-crossed distinction and becomes a follow-on.

## 7. Boundary with 007 (web dashboard)

They share the **model** and nothing else:

- same `doctor.Snapshot` / `diag.Report` / severity ordering.
- same `info.Collect` identity allowlist.
- same `monitor.Sample` process data.

They do **not** share rendering: `internal/dashboard` emits HTML via
`html/template`; `internal/consoleview` emits ANSI text. A finding-
severity-to-colour mapping may be worth extracting to a shared helper
if it's duplicated; a panel layout is not.

Neither item's scope may grow into the other: 007 stays the browser
surface, 011 stays the terminal surface. This doc and 007's are each
other's boundary reference.

## 8. Inspiration, not imitation — as a hard constraint on the deliverable

Restating `index.md`'s rule so the review can hold the implementation
to it:

- No panel title, column header, key hint, box-drawing style, colour
  value, or status wording is copied from any specific existing
  monitor. Where this design borrows an *idea* (one-screen layout,
  subsystem panels, a persistent summary line), it names the idea and
  the problem it solves, in vitals' terms.
- No tool name appears in the rendered output or in the package's code
  or comments as a thing being matched.
- The reference bar is "a person can read their machine's state at a
  glance", not "it looks like <tool>".

## 9. Open questions for the review

1. **Invocation** (§2): bare `vitals` renders the view (proposed) vs. a
   `vitals view` subcommand. Does moving usage to `vitals help` / `-h`
   surprise existing muscle memory in a bad way?
2. **Scope of v1** (§3, §6): is the events strip (just-crossed vs
   steady findings) in or out for v1?
3. **`internal/consoleview` vs. folding into `internal/doctor`**: a new
   package keeps `doctor` free of layout concerns (consistent with
   Collect/Analyze/Render separation), at the cost of one more package.
4. **Fit-to-height policy** (§4): is "drop process rows, then optional
   panels, never drop verdict/findings" the right priority order, and
   what's the default height when the terminal size is unknown?
5. **Does the snapshot view stand on its own**, or is it only worth
   building alongside the (dependency-adding, separately-reviewed) live
   mode? A "no" here is a valid, useful outcome.
6. **Shared severity→colour helper with 007** (§7): extract now, or
   leave until it's actually duplicated?

## 10. Verification gates

Standard repo gates (`go build`, `go vet`, `staticcheck`,
`go test -race`, `make coverage` — `internal/consoleview` to the 95%
raw floor, easy since `Render` is pure) plus:

- fixture tests for every layout branch: wide/narrow/short terminal,
  each optional panel present/absent, zero findings, many findings,
  a terminal too short for the process table.
- a golden-file test for a representative full render, regenerated with
  a documented flag (same pattern as `doctor`'s schema golden).
- one real end-to-end test that `vitals` with no args against a live
  machine produces a non-empty screen containing the verdict line —
  the "prove the real wiring" test this repo requires for glue code.
- `check_docs.py` for the doc updates (`docs/user-guide.md` gains a
  section; this `design.md` gets its "As built" appendix).

## Plan

[`implementation-plan.md`](implementation-plan.md) — stays empty until
this doc's `review-panel` pass converges and its must-fix findings are
folded back in here.

---

## 11. Review outcome (2026-09-05)

A full seven-agent `review-panel` ran against §1–§10 above: three
independent technical architects, security, product, QA, performance.
**All seven returned go-with-changes; no no-go.** The findings converged
tightly — most of the must-fix list below was surfaced independently by
two or more reviewers.

### Convergent must-fix (fold into the design before code)

1. **[3 architects + QA] Cut the "events strip" from v1.**
   `internal/doctor/history.go`'s `HistoryPoint` stores only
   CPU/mem/disk% + the top-mem process — never prior *findings*. So
   "findings that just crossed a threshold vs. steady state" is not
   reconstructable without a findings-history persistence format, which
   this item's own scope boundary forbids ("No new persistence
   format"). Ship `report.SortedBySeverity()` worst-first (already
   available, zero new code); make the delta strip a `doctor`-side
   follow-on once "should history carry findings" is answered. (§6 is
   amended: v1 findings section = worst-first, no delta.)

2. **[performance, deep — echoed by all 3 architects + PM on latency]
   Bare `vitals` must not call `doctor.Assess` + `monitor.Sample`
   raw, sequential, uncached.** Traced worst case ≈ **16s** (GPU probe
   2×3s + power probe 2×2s + Ollama HTTP 3s + dead mount 1.5s +
   `Collect`'s 700ms window + `monitor.Sample`'s 2×500ms), typical
   **2–3s** — for what becomes the tool's default entry point (today:
   instant). This is the exact "one page load could cost tens of
   seconds" figure that forced `internal/dashboard`'s `snapshotCache`
   redesign. Required:
   - a `doctor` **`SkipProbes` / `QuickCollect`** path that skips
     `collectGPUs`/`collectPower`/`collectLLM`/`collectThermal` (the
     four subprocess/network signals) — **shared with 008's own
     must-fix #2**, which needs the same option;
   - the GPU/power/LLM panels, if kept, populated on **one bounded
     ~300ms concurrent budget** (3 goroutines + `WaitGroup`, no dep),
     rendering "probing timed out — run `vitals gpu`" on expiry;
   - collapse the **double process-table scan**: `doctor.Collect` and
     `monitor.Sample` each enumerate every process with a separate
     sleep window. Call `monitor.Sample` with `Interval: 700ms` so it
     reuses one window, or extract a shared one-shot `procscan`;
   - a **hard ~1.5s wall-clock ceiling** on the whole gather; render
     what resolved, mark the rest "still measuring".
   Target: **< 800ms typical, < 2s ceiling** for bare `vitals`.

3. **[all 3 architects + QA] The §4 fit-to-height rules are
   self-contradictory.** "Findings not truncated" + "never emits more
   rows than the terminal has" + "verdict/findings never pushed
   off-screen" cannot all hold when findings alone overflow a short
   terminal (a critical machine: 8+ findings × wrapped detail + fixes).
   Resolution: **no-scroll is a target, not a guarantee.** Header +
   verdict + findings always render complete (the screen scrolls if it
   must); the *only* compressible budget is the process table then
   whole optional panels. If findings themselves must be trimmed, drop
   the least-severe **whole** findings (each shown finding keeps all
   its fixes) with a "+N more — run `vitals doctor`" tail — never
   truncate a shown finding's fix list. On unknown height (a TTY where
   `term.GetSize` errors): render full, do not guess.

4. **[all 3 architects] `info.Collect` cannot back the header line
   (§4.1).** It's a *live* call (`host.Info`, `os.Executable`,
   `config.Load`) — unusable from a pure `Render`. Every header field
   is already in the two snapshots: `monitor.Snapshot.Host` carries
   hostname / OS / kernel-arch / uptime, `doctor.Snapshot.CPU.Cores`
   the core count. Drop the `info.Collect` claim; `Input` gains
   `Version string` and `Now time.Time` (main sets both) for
   determinism.

5. **[security, primary — QA & architects concur] Terminal
   control-character / ANSI-escape injection.** The process table
   prints process names, full command lines, mount paths and
   reverse-DNS peer hosts straight to the terminal; `internal/ui` has
   **no** control-character sanitiser (the web side gets this free via
   `html/template`). A hostile local process can name itself with
   cursor-movement escapes and repaint vitals' own verdict line, or
   embed an OSC-8 phishing hyperlink. Required: **one shared
   `ui.Sanitize(string)`** choke point (strips C0 controls except
   `\t`, the C1 range, lone `\x1b`, `\r`, DEL, and OSC/CSI sequences),
   applied at the single point any externally-sourced string is
   formatted for the terminal — which **retrofits `doctor` / `top` /
   `memhogs`**, all of which carry this latent today. Plus a hard
   per-cell rune cap applied *before* any width math, and a `Command`
   length cap at capture time in `monitor.Sample` (~512 runes) so no
   process can break the one-screen invariant with a multi-KB argv.

6. **[QA, primary — architects 1 & 3 concur] Display-width strategy is
   unspecified and the "reuse existing `ui` helpers" claim is
   overstated.** `ui.Wrap` / `ui.Truncate` count *runes, not display
   cells*; there is no `runewidth`/`wcwidth` in the repo or `go.mod`.
   CJK ("微信" = 2 runes / 4 cells), emoji, and combining marks
   misalign every box frame and every column. Decide before code: take
   a width dependency (`golang.org/x/text/width` or
   `mattn/go-runewidth`) — the same "one dependency" reconciliation the
   doc already concedes for live mode, now needed for the *snapshot*
   view too — **or** commit to ASCII-safe hard truncation with a
   documented limitation. Add CJK/emoji and multi-thousand-char-cmdline
   fixtures to §10's matrix either way.

7. **[architect 3, primary — architect 1 concurs] Reconcile with
   `internal/doctor/focus.go`.** `consoleview` would be the *third*
   ANSI per-resource renderer in one binary (`doctor.SummaryLine`,
   `focusDetail`, `consoleview`), each with its own copy of the
   per-resource threshold constants (`focus.go`'s `pct(…,70,90)`, disk
   `85/95`, await `20/50`, mem-available `20/8`) — which *will* drift
   from `config.Default()`. Either refactor `focusDetail`'s per-resource
   blocks into shared pure formatters that `RunFocus` and
   `consoleview.Render` both call, or justify the divergence in the
   design **and** own the duplicated constants as an explicit open
   item. §5 must also re-anchor on the fact that the irreducibly new
   thing is §4's fit-to-viewport layout engine, not "another
   renderer".

8. **[architect 2 + QA] The `main` seam and its tests.** `main.run` is
   not TTY-aware and `TestRunEmptyArgsPrintsUsageAndExitsTwo`
   (`main_test.go`) only passes today by luck (pipe → non-TTY →
   fallback). Add `var isTTY func() bool` and an injected `sizeFn`;
   route the glue through a testable
   `consoleview.Run(w io.Writer, size func() (int,int,bool), opts) int`
   entrypoint (mirroring `doctor.Run` / `monitor.Run`) so it gets its
   own coverage floor instead of dragging `main` (floor 39). Add a
   `--view` / `VITALS_VIEW=1` flag that forces the render regardless of
   TTY — **required** so the "prove real wiring" test can reach the
   view at all (a piped exec is non-TTY and only hits the fallback),
   and independently useful for `vitals view | less -R`. Give bare
   `vitals` a `--ollama-url` flag (parity with `advice`). Specify the
   exit code: `report.ExitCode()` (0/1/2) on the TTY view, keep `2` for
   the non-TTY usage fallback. Update the broken test.

9. **[architect 2 + performance] `doctor.Assess` does NOT run the
   DNS-latency probe** (it's only in `RunFocus` for `net`). So the
   console view silently omits the "DNS resolution slow/failing"
   finding class that `vitals net` surfaces — state that gap
   explicitly or compute it live at the call site. (Note: 008's
   design.md §10 Q5 has the same factual error and should be
   corrected.)

10. **[performance] Bare `vitals` must not churn the trend history.**
    `finishAssess` → `recordHistory` writes `history.jsonl` every call;
    `watch -n1 vitals` or shell-prompt integration would evict real
    hourly points within minutes and break the dashboard's own
    sparklines. The console-view path reads history
    (`addLeakFinding(LoadHistory())`) but must **not** write it — or
    `recordHistory` self-rate-limits to one write per N minutes.

11. **[QA] §5 names `ui` functions that don't fit.** `ui.Okf`/`Warnf`
    print to stdout and return nothing; `ui.Errf` writes stderr. A
    string-building `Render` uses the raw `ui.Red`/`Yellow`/`Green`/
    `Reset` constants or needs new `ui` helpers (which carry `ui`'s
    96% floor). Fold the severity→colour mapping into one small
    `internal/ui` helper (this is its *third* consumer after the
    verdict banner and `PrintFindings`) — **not** a 007-shared
    abstraction; the web side keys off CSS class names and shares
    nothing here. The findings block should call the already-exported
    `doctor.PrintFindings` rather than re-render.

12. **[performance] The live-mode caching gate is a dangling
    reference.** §3 points at "§7" for the discipline, but §7 is the
    007 boundary. The follow-on live-console item must carry an
    explicit, hard precondition: it reuses the `snapshotCache`
    single-flight + TTL pattern (or the `SkipProbes` quick path), the
    same way item 005 gated on §6.4's cache landing first.

### Testing (QA)

- Turn §10's fixture list into **property assertions** over the whole
  matrix: every rendered line ≤ `width` (display-width aware), total
  line count ≤ `height`, the verdict substring present at *every* drop
  tier, columns aligned. Hitting a line ≠ proving the layout.
- The golden-file plan **oversells its precedent**: `doctor`'s only
  `.golden` is a sorted field-name list; a full-screen render golden is
  a new, wording-sensitive test genre that churns on any reworded
  finding. Keep the golden minimal; lean on the property assertions.
- Matrix must add: non-ASCII/CJK/emoji cell content; a multi-KB
  command line; Windows legacy-console / non-UTF-8 codepage (largest
  ANSI+box surface vitals would emit — an `--ascii` fallback is worth
  considering); the `os.Pipe` `captureStdout` deadlock is avoided by
  testing `Render`'s return value, not `main`'s captured stdout.
- New package ⇒ mandatory `check_coverage.py` `FLOORS` entry for
  `internal/consoleview` in the same change.
- `check_docs.py`: `docs/user-guide.md` section + `design.md` "As
  built" appendix, both with breadcrumbs / nav.

### Answers to §9's open questions, as the panel converged

- **Q1 (bare `vitals` vs. `vitals view` subcommand):** the panel
  splits. Architects: bare `vitals` → view is fine and low-risk
  (today's bare output is a stderr usage dump with exit 2 that nobody
  scripts) **provided** an explicit `vitals view` / `vitals glance`
  alias is also registered. PM: ship as `vitals glance` first, leave
  bare `vitals` alone (decouple the new renderer, the new default, and
  the usage relocation), and separately upgrade bare `vitals` on a TTY
  to print the one-line summary + pointers; promote to default later.
  **Register the explicit alias + `--view` flag regardless. Whether it
  becomes the bare-`vitals` default is the one decision left to the
  maintainer.**
- **Q2 (events strip):** **out of v1** (4 of 7 — must-fix 1).
- **Q3 (`internal/consoleview` vs. fold into `doctor`):** **new
  package** — unanimous. Pair with the `consoleview.Run(...)` glue
  entrypoint (must-fix 8) so the wiring gets its own floor.
- **Q4 (fit-to-height default):** priority order is right; add the
  findings-overflow rule (must-fix 3), a named `consoleview.DefaultHeight`
  constant, independent width/height "unknown" handling, and
  render-full-don't-guess on unknown height.
- **Q5 (does the snapshot stand alone):** **yes for v1** (5 of 7) —
  *conditional on* re-anchoring §5 on the layout engine (must-fix 7)
  and, ideally, a visual link from each finding to the panel(s) it
  implicates so the correlation/verdict bet is what's actually on
  screen. PM most skeptical; without those, v1 reads as "`doctor` +
  `top` with boxes".
- **Q6 (shared severity→colour helper):** extract minimally into
  `internal/ui` when `consoleview` is written (must-fix 11) — not a
  007-shared abstraction, not an up-front extraction.

### Status after this review

**Approved for implementation with the must-fix list folded in.** The
changes are scoping precision and latency discipline, not a redesign —
the pure-`Render` / injected-inputs / snapshot-first / no-new-dependency
(pending the width-lib decision, must-fix 6) spine survives intact.
`implementation-plan.md` can be written from §1–§10 as amended by this
section. The one genuine product decision outstanding is Q1 above.
