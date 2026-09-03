# Implementation plan — 006 Coverage hardening

[docs](../../../index.md) / [Roadmap](../../index.md) / [006 — Coverage hardening to the 95%+ hard rule](index.md) / **Implementation plan**

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
- [x] `internal/memhogs` (53.2% -> 59.7%) — `describe` (a pure Chrome/VS
      Code helper-process name simplifier) was fully untested; `userFamilies`
      (config-file read/parse — missing file, malformed file, valid file)
      was only 45.5% covered, now 90.9%, tested via a local
      `isolateConfigDir` helper matching `internal/doctor`'s. One real
      test bug caught along the way: writing the fixture families.json
      under the raw temp dir instead of the *resolved* `os.UserConfigDir()`
      path (which is `$HOME/Library/Application Support` on macOS, not
      `$HOME` itself) silently tested the wrong file. `Run`/`once`/
      `readCgroup` are genuinely live.
- [x] `internal/llm` (53.6% -> 57.7%) — covered `classify`, `capitalize`,
      `plural`, `nz`, `shortLocalName`, `modelOrDefault`, and
      `ollamaModelChoice`'s two pure branches (override/resident-model,
      before its live `/api/tags` fallback) — all previously untested.
      **Follow-up left for a future pass**: `render` (llm.go:497) is a
      large print function, same shape as `internal/dupes`' `render` —
      worth the same stdout-capture treatment, not done here for time.
      `Run`/`once`/`scanProcesses`/`checkGPUDriver`/`runsCleanly`/`RunFit`
      remain live (process scanning, subprocess exec).
- [x] `internal/mcp` (55.2% -> 68.7%) — `toolText`, `jsonString`,
      `ToolNames` were fully untested despite being trivially pure.
      `tools()` (10%) registers each tool's `Handler` closure, only
      executed by an actual `tools/call` — the existing tests
      deliberately avoid that since it would touch live system state
      (`doctor.Assess`, `gpu.Probe`, etc.), matching this server's own
      read-only-by-construction design.
- [ ] `internal/guide` (73.4%) — `Serve`/`ServeHTML`/`ServeLocal`/
      `openBrowser` are exempt (documented in `check_coverage.py`); verify
      nothing else is dragging the number down.
- [x] `internal/metrics` (73.7% -> 75.2%) — covered the CPU-steal metric
      (only emitted when `StealPct > 0`, a hypervisor-only signal) and
      the unnamed-GPU `gpu%d` label fallback, both untested. `trimFloat`'s
      defensive empty-string guard looks unreachable for real float64
      input and was left alone rather than forced. `collect`/`RunOnce`/
      `Serve` remain live.
- [x] `internal/ui` (76.0% -> 96.0%, **clears the 95% hard rule**) —
      `Header`/`Rule`/`Infof`/`Okf`/`Warnf`/`Errf`/`Key`/`Emph` were all
      completely untested despite being used by every other package in
      this codebase; covered via the same stdout/stderr-capture pattern
      used elsewhere. `colorEnabled`'s NO_COLOR branch is now covered
      too; its TTY-detection branch is environment-dependent and left as
      the only real gap.
- [x] `internal/config` (80.6% -> **100.0%**) — `Parse` only had 3 of 6
      recognized keys exercised (`disk_critical_percent`/`ram_warn_percent`/
      `ram_high_percent` were untested). `Path`/`Load`'s
      `os.UserConfigDir()`-fails branch turned out to be reliably
      testable cross-platform: setting `HOME`/`APPDATA`/`XDG_CONFIG_HOME`
      to the empty string (not just unset) makes the real stdlib function
      itself return an error identically on macOS/Linux — no need to
      treat it as an untestable defensive branch.
- [x] `internal/help` (86.5% -> **100.0%**) — `RenderList` was the sole
      0%-coverage function: pure and trivially testable since it already
      takes an `io.Writer` parameter (no stdout-capture needed), covered
      via `bytes.Buffer` — asserts version/USAGE/COMMANDS/footer text and
      that every `Names()` entry appears in the rendered list.
- [x] `main` (3.3% -> 42.2%) — extracted `run(argv []string, version
      string) int` holding the entire dispatch switch that used to live in
      `main()`; every inline `os.Exit(N)` became `return N`, and `must`
      became `must(err error) int` (prints, returns 1/0) instead of
      calling `os.Exit` itself. `main()` is now the one-line
      `os.Exit(run(applyGlobalFlags(os.Args[1:]), version))`. This made
      the whole dispatch/validation surface — empty args, unknown
      commands, `help`/`version`/`completion` variants, `doctor --schema`,
      `doctor --compare`'s arg-count and unreadable-file validation,
      `guide --raw`/default (not `--web`) — directly unit testable
      in-process via `main_test.go`'s new `TestRun*` tests, no subprocess
      needed. `newFlagSet` and `defaultOllamaURL` (both previously 0%)
      are now 100%. Deliberately left calling into `run()` for any
      subcommand's real `Run`/`RunFocus` (doctor, clean, dupes, tools,
      memhogs, memcheck, gpu, monitor, advice, llm, metrics, mcp, `guide
      --web`) — those touch real subprocess/network/filesystem state and
      stay validated by `cli_smoke_test.go`'s exec-the-real-binary
      approach, consistent with every other package's Collect/Run-is-live
      exemption in this initiative.

## Exit criteria

Every package above is either at 95%+, or has an inline comment in
`check_coverage.py` (matching `guide`'s existing one) explaining exactly
which functions are irreducibly live and why the remaining number is the
real ceiling — never just left low with no explanation.
