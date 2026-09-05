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
