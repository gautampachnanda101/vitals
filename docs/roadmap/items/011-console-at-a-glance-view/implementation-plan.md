# Implementation plan — 011 Console at-a-glance view

[docs](../../../index.md) / [Roadmap](../../index.md) / [011 — Console at-a-glance view](index.md) / **Implementation plan**

Shipped 2026-09-06, built to [`design.md`](design.md) §1–§10 as amended
by the §11 seven-agent review. What landed:

- [x] **Shared foundation** (separate PR, in `main`): `doctor.QuickAssess` /
      `Options.SkipProbes`; `ui.Sanitize`; `ui.TermSize() (cols,rows,ok)`;
      `ui.GradeSeverity(word,text)`. (Review must-fixes 2, 4, 5, 11.)
- [x] **`internal/consoleview` package** — pure
      `Render(in Input, width, height int, fitHeight bool) string`.
      `Input{Snapshot, Report, Procs monitor.Snapshot, Version, Now}`.
      Header from `monitor.Snapshot.Host` + `CPU.Cores` — **no
      `info.Collect`** (must-fix 4). Findings worst-first, **no events
      strip** (must-fix 1). Panels built as `[]string` line slices,
      measured before assembly. Panel severity comes from
      `doctor.AnalyzeResource` — **no threshold constant is duplicated
      from `focus.go`** (must-fix 7 resolved by not re-deriving
      severity at all). `NetIface.RetransPct` not shown (never
      populated). 97.6% raw; `check_coverage.py` floor added.
- [x] **Fit-to-height** (must-fix 3): no-scroll is a target, not a
      guarantee. Verdict + summary + findings always render complete;
      the compressible budget is process rows → whole optional panels →
      least-severe *whole* findings with a "… N more — run `vitals
      doctor`" tail. `fitHeight` false (unknown terminal size) renders
      everything. `DefaultHeight = 24`.
- [x] **`consoleview.Run(w, size, opts) int`** (must-fix 8) — runs
      `QuickAssess` and `monitor.Sample` **concurrently** under a 3s
      `time.After` ceiling, renders with whatever arrived, returns
      `report.ExitCode()`. `DefaultSize` = `ui.TermSize`.
- [x] **`main.go`** — `vitals view` / `vitals glance` subcommand; bare
      `vitals` on a TTY (`ui.ColorEnabled()`) renders it, else the
      existing command-list at exit 2; `VITALS_VIEW=1` forces it into a
      pipe (must-fix 8's `--view` role). `help.go` entry;
      `docs/user-guide.md` `vitals view` section.
- [x] **Terminal-injection safe** (must-fix 5) — every externally
      sourced string (process names/commands, mount paths, hostname)
      goes through `ui.Sanitize`; the shared-foundation retrofit
      already sanitises at the `monitor.Sample` / `doctor` collection
      boundary too.
- [x] **Tests** — property assertion "no rendered line exceeds width"
      over the fixture matrix (healthy / warning / critical, each
      optional panel present + absent incl. SMART, narrow 1-up reflow,
      fit-to-height at 8/12/20/60 rows, unknown-size render-all,
      hostile control chars, no-processes/no-panels, tiny width). One
      real end-to-end `Run` against the live collectors.

## Resolved open questions (§11)

- **Q1 invocation:** ship both — `vitals view` *and* bare `vitals` on a
  TTY. Piped/redirected bare `vitals` keeps the command list, so
  scripts are unaffected; `VITALS_VIEW=1` overrides.
- **Q6 display width:** **no new dependency** for v1. ASCII-safe
  rune-count truncation (`ui.Truncate`); a wide (CJK / emoji) cell can
  be a column off. Taking `golang.org/x/text/width` stays a deliberate
  future decision, not a v1 default — the "one dependency" claim holds.

## Deferred (its own item, when filed)

A live/`--watch` mode: it needs a refresh loop + raw terminal input +
almost certainly a TUI dependency. Precondition, per the review: it
reuses the snapshot-cache single-flight/TTL pattern (or the
`SkipProbes` quick path) — it does not re-run the full gather on a
timer — and its dependency gets the "one dependency" reconciliation.
