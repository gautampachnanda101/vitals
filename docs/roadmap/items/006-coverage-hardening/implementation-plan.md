# Implementation plan — 006 Coverage hardening

This file shows what's **left**. Check off a package once it's at 95%+
(or its genuinely-irreducible live-glue ceiling, documented inline in
`check_coverage.py` the way `guide`'s is) and its floor is ratcheted up
to match. See `AGENTS.md`'s "Roadmap discipline" for the living-document
rule.

Ordered by how much of each package's shortfall looks like extractable
pure logic vs. genuinely irreducible live glue, easiest first — that's a
starting guess, re-order as each package turns out easier or harder than
expected.

## Packages

- [x] `internal/tools` (27.0% -> 44.6%) — extracted/fully tested the pure
      logic: `withSudo` took `runtime.GOOS`/`os.Geteuid()` as parameters
      instead of reading them directly (the Windows/root branches were
      otherwise never exercised by the Linux-only coverage gate no matter
      how many OSes the test matrix covered), `formatToolList` pulled the
      pure rendering out of `List()`, plus full `installCommand`/`binary`/
      `firstOrEmpty` branch coverage. Remaining 0%/low functions
      (`Installed`, `detectManager`, `Run`, `List`, `Install`, `Launch`,
      `confirm`) are genuinely live (subprocess exec, real PATH/stdin) —
      documented inline in `check_coverage.py`.
- [x] `internal/memcheck` (32.3% -> 33.3%) — small bump: added the
      missing `diag.Warn`-severity case to `TestVerdictPrintsEachSeverityLine`
      (only OK and Critical were exercised before), bringing `verdict` to
      100%. `printIf`/`memVerdict` were already 100%. `Run` (0%) is
      genuinely all live gopsutil calls feeding already-tested pure
      functions — the same Collect-then-Analyze shape as `internal/doctor`,
      nothing left to extract.
- [x] `internal/monitor` (34.2% -> 41.2%) — `bar` was missing its
      yellow-range (60-84%) case entirely (only green/red were tested);
      `emit` was missing the mem-breakdown/swap/disk-IO/net-IO branches
      (previous test used a snapshot with none of that data) and the
      "cap at 4 rows" break in both I/O loops. All pure formatting
      (`emit`, `bar`, `rate`, `memBreakdownLine`, `ioDelta`) is now 100%;
      `Run`/`sample`/`readDiskCounters`/`readNetCounters`/`topProcesses`
      are live gopsutil calls, documented as the ceiling.
- [x] `internal/advice` (34.8% -> 39.1%) — `Generate`'s error branch (no
      provider reachable) was untested; now 100%. `Run` (0%) is thin live
      glue over `doctor.Assess` + the now-fully-tested `Generate` —
      nothing left to extract.
- [x] `internal/clean` (41.0% -> 50.2%) — `osCacheDirs` and `withSudo`'s
      sibling pattern applied again: `freeSpaceRoot`/`osCacheDirs` now
      take `goos`/`systemRoot` as parameters instead of reading
      `runtime.GOOS`/`os.Getenv` directly, making their Windows/darwin
      branches testable on Linux CI (one exception, documented in a test
      comment: the exact Windows drive-letter extraction depends on
      `filepath.VolumeName`'s own OS-aware stdlib behavior and can only
      be verified on a real Windows host). `devCacheDirs`, `plural`, and
      `renderCleanHistory`'s location-count branch were fully untested
      before, now 100%. Remaining 0%/low functions are genuinely live —
      this package's whole job is filesystem/subprocess I/O — documented
      inline in `check_coverage.py`.
- [x] `internal/gpu` (46.9% -> 54.7%) — several parsers were much less
      tested than assumed: `attachNvidiaApps` (0 devices/procs edge
      cases, multi-device attach), `atoiOr`/`numOr`/`firstNonEmpty`'s
      default-fallback branches, `strSort` (a hand-rolled insertion sort
      that was essentially untested — now verified it actually sorts, not
      just doesn't crash), and blank/malformed-line skipping in
      `parseNvidiaSMI`/`parseNvidiaApps`. `Probe`/`run`/report.go's `Run`
      shell out to nvidia-smi/rocm-smi/ioreg — genuinely live.
- [x] `internal/doctor` (50.1% -> 55.0%) — `AnalyzeResource` (the switch
      `vitals cpu|mem|disk|...` and the dashboard's resource pages both
      dispatch through) had zero direct tests in this package at all —
      only exercised indirectly via `internal/dashboard`'s own tests,
      which don't count toward this package's coverage. `diskGrowthRate`
      was fully pure and fully untested despite a detailed doc comment
      describing three branches. Also covered: `pct`, `throttleNote`,
      `fullestDisk`, `summaryLine`, `procSuffix`, `quitFix`, `coreSpread`,
      `timeToFull`, `nz` — all small pure helpers nobody had gotten to.
      Remaining low-coverage functions are `Collect`'s live OS-level
      helpers and the CLI entrypoints' print wrappers, documented inline
      in `check_coverage.py`.
- [x] `internal/dupes` (51.1% -> 68.4%) — `render` was a large, entirely
      untested (0%) print function despite its name suggesting pure
      string-building; covered via the same stdout-capture pattern used
      in `internal/monitor`/`internal/memcheck`/`internal/tools`
      (no-groups-found, top-N cap, "and N more" counter). `Run`/
      `applyHardlinksWithConfirmation`/`confirmHardlink` are genuinely
      live (stdin prompts, real hardlinking).
- [ ] `internal/memhogs` (53.2%) — `appFamily`/`bucketFamilies` (per
      memory of this package) should already be well-tested; audit
      `stopCommand` and the rest.
- [ ] `internal/llm` (53.6%) — provider probing/completion HTTP calls are
      exempt; parsing (`parseOllamaChatResponse` etc.) and decision logic
      (`ollamaModelChoice`, `defaultModelFor`) should be near-100%.
- [ ] `internal/mcp` (55.2%) — the JSON-RPC `handle()` function should
      already be well-tested per its own design (pure + tested per
      memory); audit the rest of the dispatch surface.
- [ ] `internal/guide` (73.4%) — `Serve`/`ServeHTML`/`ServeLocal`/
      `openBrowser` are exempt (documented in `check_coverage.py`); verify
      nothing else is dragging the number down.
- [ ] `internal/metrics` (73.7%) — `Render` should be pure/near-100%; the
      HTTP server (`Serve`/`RunOnce`) is the exempt live part.
- [ ] `internal/ui` (76.0%) — small formatting-helper package; should be
      easy to push close to 100% since almost none of it is live.
- [ ] `internal/config` (80.6%) — a flat-file parser; should be easy to
      push higher, it's almost entirely pure.
- [ ] `internal/help` (86.5%) — static command-doc data + rendering;
      should be easy to push higher.
- [ ] `main` (3.3%) — the hardest one. Extracting a testable
      `run(args []string) (int, error)` that `main()` just wraps with
      `os.Exit` (instead of every `case` calling `os.Exit`/`must` inline)
      would let most of the flag-parsing/dispatch logic be unit tested
      directly, the same way `cli_smoke_test.go` proves it works but at
      unit-test speed and granularity. This is a real refactor, not a
      quick pass — worth its own design note before starting given how
      central `main.go` is; don't attempt it casually.

## Exit criteria

Every package above is either at 95%+, or has an inline comment in
`check_coverage.py` (matching `guide`'s existing one) explaining exactly
which functions are irreducibly live and why the remaining number is the
real ceiling — never just left low with no explanation.
