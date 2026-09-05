# Implementation plan — 011 Console at-a-glance view

[docs](../../../index.md) / [Roadmap](../../index.md) / [011 — Console at-a-glance view](index.md) / **Implementation plan**

Written from [`design.md`](design.md) §1-§10 **as amended by the §11
review outcome** (unanimous seven-agent go-with-changes). Not started.
One product decision is still open — see the last task. Each task is one
working, tested increment.

## Tasks

- [ ] **`internal/doctor`: `QuickCollect` / `Options.SkipProbes`.** A
      collect path that skips `collectGPUs` / `collectPower` /
      `collectLLM` / `collectThermal` (the subprocess + network
      signals), keeping the gopsutil-bounded CPU/mem/swap/net/disk.
      Tested that the skipped probes don't run. **Shared with 008** —
      its review has the same must-fix; land it once, both use it.
- [ ] **`internal/monitor`: cap `ProcInfo.Command`** at capture
      (~512 runes) and expose a way for a caller to reuse one sample
      window (accept `Interval` and prime against it) so the console
      view doesn't pay a second 500ms sleep + a second full process
      sweep on top of `doctor.Collect`'s.
- [ ] **`internal/ui`: `Sanitize(string) string`** — strip C0 controls
      (keep `\t`→space), the C1 range, lone `\x1b`, `\r`, DEL, and
      OSC/CSI sequences. Apply at the single point any externally
      sourced string is formatted for the terminal; **retrofit
      `doctor` / `top` / `memhogs`** in the same change. Regression
      vectors: `\x1b[2J`, `\x1b[1A`, `\x1b]0;x\x07`,
      `\x1b]8;;http://evil\x07`, `\r`, `\x9b31m`.
- [ ] **`internal/ui`: `TermSize()`** (width + height from the
      `term.GetSize` call already at `ui.go`, currently discarding
      height); `GradeSeverity(sev, text) string` (the third consumer of
      the severity→colour switch); a `visualWidth` / `padVisual` over
      ANSI-containing strings (`StripANSI` + a display-width measure).
- [ ] **Display-width decision** — take `golang.org/x/text/width` (or
      `mattn/go-runewidth`) as a second dependency with the "one
      dependency" reconciliation written up, **or** commit to
      ASCII-safe hard truncation with a documented limitation. Blocks
      the layout math.
- [ ] **`focus.go` reconciliation** — refactor `focusDetail`'s
      per-resource blocks into shared pure formatters both `RunFocus`
      and `consoleview` call, **or** record the divergence + the
      duplicated threshold constants (`pct(…,70,90)`, disk `85/95`,
      await `20/50`, mem-available `20/8`) as an accepted, tracked
      duplication.
- [ ] **`internal/consoleview` package** — pure
      `Render(in Input, width, height int) string` where
      `Input{Snapshot doctor.Snapshot; Report diag.Report; Procs
      monitor.Snapshot; Version string; Now time.Time}`. Header from
      `monitor.Snapshot.Host` + `CPU.Cores` (no `info.Collect`).
      Findings via `doctor.PrintFindings`, worst-first, **no events
      strip**. Panels built as measurable `[][]string` before
      join/colour. Fit-to-height: verdict + findings always complete;
      compressible budget = process rows → whole optional panels; drop
      whole least-severe findings with a "+N more" tail before
      truncating any; `DefaultHeight` const on unknown size; drop
      `NetIface.RetransPct` from the panel (never populated). 95%+ raw;
      `check_coverage.py` `FLOORS` entry added.
- [ ] **`consoleview.Run(w io.Writer, size func() (int,int,bool), opts) int`**
      — the testable glue: quick-collect + `monitor.Sample` run
      concurrently under a ~1.5s context deadline; optional GPU/power/
      LLM panels on a shared ~300ms budget rendering a "still measuring"
      note on expiry; **reads** history, never writes it. Returns
      `report.ExitCode()`.
- [ ] **`main.go` wiring** — `var isTTY func() bool`; `--view` /
      `VITALS_VIEW=1` force flag; `--ollama-url` on bare `vitals`;
      bare `vitals` on a TTY → `consoleview.Run`, else the existing
      usage fallback at exit 2. Update
      `TestRunEmptyArgsPrintsUsageAndExitsTwo`; add a `cli_smoke_test.go`
      no-args case (0/1/2 exit allow-list) and a `--view` case with
      `--ollama-url` at a closed port + `HOME`/`XDG_CONFIG_HOME`
      isolation.
- [ ] **Tests** — property assertions over the fixture matrix (every
      line ≤ width display-width-aware; count ≤ height; verdict present
      at every drop tier; columns aligned); minimal golden, not a
      wording-sensitive full snapshot; matrix adds CJK/emoji, a multi-KB
      command line, Windows codepage. The "prove real wiring" test uses
      `--view` + isolation + closed-port ollama.
- [ ] **Docs** — `docs/user-guide.md` section; `design.md` "As built"
      appendix; correct 008's design.md §10 Q5 (it repeats the "Assess
      runs a DNS probe" error).
- [ ] **Product decision (blocks the `main.go` task's default
      behaviour):** does bare `vitals` on a TTY render the view
      (architects: yes, with the explicit `vitals view` alias also
      registered), or does v1 ship `vitals view` only and leave bare
      `vitals` as-is / upgraded to a summary line (PM)? The alias +
      `--view` flag ship either way.

## Live-console follow-on (separate item, when filed)

Hard precondition, per the review: any live/`--watch` mode reuses the
`snapshotCache` single-flight + TTL pattern (or the `SkipProbes` quick
path) — it does not re-run the full gather on a timer. A TUI dependency
gets its own "one dependency" reconciliation. Inherits `ui.Sanitize`.
