# 011 — Console at-a-glance view

[docs](../../../index.md) / [Roadmap](../../index.md) / **011 — Console at-a-glance view**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Not started — captured from a maintainer request (2026-09-05)
after looking at how dense, single-screen terminal system monitors
present a whole machine at once. Needs a design doc and a
`review-panel` pass before any code, per the "Why this needs review
before code" section below.
**Depends on**: [doctor](../../../architecture/design.md)'s existing
`Collect`/`Analyze` and the single-flight snapshot-cache pattern
(`internal/dashboard`); logically parallel to
[007](../007-dashboard-visuals/) (that item is the *web* dashboard —
this one is the *terminal*)
**Target release**: not yet — see "Trigger"
**Architecture**: not yet written

## What

`vitals` in a terminal today is print-and-exit: each command emits one
flat block of text and returns. There is no single view that shows the
whole machine at once.

This item is a native, one-screen console view that lays out every
signal `doctor` already collects as tiled panels sized to fit a normal
terminal without scrolling:

- CPU / memory / swap / load, as labelled gauges rather than a sentence
- per-interface network throughput (rx/tx rates)
- disk I/O rates and per-filesystem capacity
- sensors / battery where available
- containers, when a container runtime is present
- a per-process table (CPU%, memory, pid, command), sorted, truncated
- a short events strip: the findings `doctor` raised this pass, and any
  that just crossed a threshold

The point that makes it a *vitals* view and not a generic monitor: the
**verdict leads**. The findings — what is wrong and what to do about it
— sit at the top of the layout, in colour, before the raw gauges. The
numbers are the supporting evidence for the verdict, not the headline.
That ordering is the same principle the rest of the product already
follows: rule-based diagnosis first, always; everything else is context
layered on top ([[feedback-llm-complement-not-primary]],
[[world-class-bet]]).

## Inspiration, not imitation

The maintainer's framing when raising this: *take inspiration from what
dense terminal monitors do well, do not copy them.* Concretely, for
whoever designs and builds this:

- **Borrow the information architecture** — everything on one screen,
  panels grouped by subsystem, colour-coded values in monospace tables,
  a persistent summary row — because that is genuinely a better way to
  read a machine's state than a scrolling wall of text.
- **Do not reproduce another tool's layout, key bindings, panel names,
  colour choices, or output wording.** No code, config, or text is
  lifted from any existing monitor. The design doc names the *problems*
  each borrowed idea solves, in vitals' own terms, and the
  implementation solves them its own way.
- **Do not reimplement a live process monitor.** vitals' standing bet
  is that it is the diagnostic layer *over* established tools, not
  another one of them ([[world-class-bet]]). This view's reason to
  exist is the verdict layer and the correlation across subsystems that
  `doctor` already does — a plain resource monitor with no diagnosis on
  top is out of scope, and `vitals live` already hands off to a
  dedicated one for people who want that.

## Why this needs review before code

- **It changes vitals' output shape, not just its styling.** Further
  polishing the existing one-shot renderers (`internal/ui`,
  `doctor.PrintFindings`) was already established as *not* closing this
  gap ([[project-interactive-tui-vision]]). A one-screen panelled view
  is a structurally different renderer, and — if it ever becomes
  live-refreshing rather than a single snapshot — likely needs a
  layout/TUI dependency, which has to be reconciled explicitly with the
  "one static binary, one dependency (`gopsutil`)" claim `docs/user-guide.md`
  and [[world-class-bet]] both make today. That reconciliation is a
  review-panel decision, not an implementation detail.
- **Static snapshot vs. live refresh is an open question**, and they
  have very different cost and dependency profiles. A single-shot
  panelled render (`vitals` with no subcommand, or a `--view` flag)
  needs no new dependency and no refresh loop; a live view does. The
  design should probably land the snapshot form first and treat live
  refresh as its own follow-on decision.
- **Overlap with 007.** That item is the web dashboard's visual layer;
  this is the terminal. They should share the *model* — the same
  `doctor` snapshot, the same finding-severity ordering — and nothing
  else. The design doc says explicitly where the boundary is so neither
  item silently grows into the other.

## Scope boundary — must be respected wherever this ends up implemented

- **One `doctor` snapshot per render, through the existing cache.**
  Never a fresh `Collect()` per panel or (if live) per frame — reuse
  the single-flight snapshot-cache pattern
  (`snapshotCache`, `internal/dashboard`). Any per-panel data-gathering
  belongs in the refresh, never in a panel's own draw path — the same
  rule 007's scope boundary sets for `Render` functions.
- **No new persistence format.** Trend/threshold-crossing data for the
  events strip comes from `doctor`'s existing history mechanism, same
  constraint as 007.
- **Process table = the data `monitor.Sample` already returns.**
  No new per-process probes (per-process disk I/O and per-process
  network counters are their own scoped question — see
  [010](../010-companion-tools-integration/) and its implementation
  plan) unless a review signs off on adding them here.
- **No tool names, key maps, or wording copied from any existing
  monitor**, in the code or the docs — restating the "Inspiration, not
  imitation" rule above as a hard constraint on the deliverable.
- **Design doc + `review-panel` pass before implementation**, per
  `AGENTS.md`'s "Roadmap discipline", the same as items 001/002/008.

## Trigger

Not scheduled to a release. The maintainer's request is logged; the
next step is a design doc this item's header can point to, then a
review panel. It does not get built straight from this `index.md`.

## Plan

[`implementation-plan.md`](implementation-plan.md) — empty until the
design doc exists and its review converges.
