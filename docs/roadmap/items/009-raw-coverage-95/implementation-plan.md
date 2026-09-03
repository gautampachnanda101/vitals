# Implementation plan — 009 Raw 95%+ coverage

[docs](../../../index.md) / [Roadmap](../../index.md) / [009 — Raw 95%+ coverage, no live-glue exemption](index.md) / **Implementation plan**

This file shows what's **left**. Check off or delete a task as it
lands, and update `check_coverage.py`'s floor for that package in the
same commit. See `index.md`'s "Ground rules" before starting any task.

Numbers below are each package's real measured coverage as of
2026-09-04 (`go test -cover ./...`), the current floor in
`check_coverage.py`, and what's actually live in it per that file's own
existing per-package comments — read those comments in full before
starting a package, they're the fastest way to know exactly which
functions need an injectable seam.

- [ ] `internal/memcheck` (33.3%, floor 33) — smallest package, good
      first one to establish the pattern on. `Run` calls `host.Info`,
      `mem.VirtualMemory`, `mem.SwapMemory`, `mem.SwapDevices` (all
      gopsutil) feeding already-100%-covered `memVerdict`/`verdict`/
      `printIf`. Inject these four as function values (or a small
      `type source struct{ hostInfo func()...; virtualMemory
      func()...; ... }`) with real gopsutil calls as the production
      default, fakes in the test.
- [ ] `internal/monitor` (41.2%, floor 41) — `Run`/`sample`/
      `readDiskCounters`/`readNetCounters`/`topProcesses` are the live
      gopsutil/process side; `emit`/`bar`/`rate`/`memBreakdownLine`/
      `ioDelta` are already ~100%. Same injection shape as memcheck.
- [ ] `internal/tools` (44.6%, floor 44) — `Installed`/`detectManager`
      (`exec.LookPath`), `Run`/`List`/`Install`/`Launch`/`confirm`
      (subprocess exec, `os.Stdin` reads, real PATH checks). `LookPath`
      and `exec.Command` both need injecting as function values;
      `os.Stdin` reads need the same `bufio.Scanner`-over-an-injected-
      `io.Reader` shape `confirm()`-style functions elsewhere in this
      repo already use (see `internal/clean/clean.go`'s `confirm`).
- [ ] `internal/gpu` (54.7%, floor 54) — `Probe`/`report.go`'s `Run`
      shell out to `nvidia-smi`/`rocm-smi`/`ioreg`. Parsers
      (`parseNvidiaSMI`/`Apps`, `attachNvidiaApps`, `parseRocmSMIJSON`,
      `atoiOr`/`numOr`/`firstNonEmpty`/`strSort`) already ~100%. Inject
      the subprocess-exec call the same way as `tools`.
- [ ] `internal/doctor` (54.6%, floor 54) — the biggest lift: `Collect`
      and its OS-level helpers (`firstTimes`, `percoreTimes`,
      `topProcs`, `swapCounters`, `diskCounters`, `netCounters`,
      `collectPower`, `runCmd`, `readLinuxBattery`, `collectDisks`),
      the CLI entrypoints (`Run`/`Assess`/`RunFocus` and their print
      helpers), and `checkDNSLatency`/`topRemotePeers` are all live.
      `Analyze`/`AnalyzeResource` and the small pure helpers are
      already ~100%. Given the volume, consider whether `Collect`
      itself can be decomposed into named per-signal collectors each
      taking an injected data source, rather than one monolithic
      injection point — matches how item 006 already extracted
      `withDiskHistory` out of this same package.
- [ ] `internal/llm` (62.6%, floor 57) — `Run`/`once`/`scanProcesses`/
      `ScanProcesses`/`OllamaModels`/`ProbeProviders`/`RunFit`,
      `checkGPUDriver`/`runsCleanly` (subprocess exec), and `render`
      (a print function, not yet covered via the stdout-capture pattern
      other packages already use — see `internal/dupes/dupes_test.go`'s
      `captureStdout`, and `internal/llm/llm_test.go`'s own copy added
      2026-09-03 for `TestRenderListsModelNamesForReachableLocalProviders`
      — extend that, not a new one). Network calls (`probeOne`'s
      `http.Client`) already use `httptest.NewServer` in existing tests
      — extend that pattern to the currently-uncovered call sites.
- [ ] `internal/clean` (67.9%, floor 50) — `Run`/`confirm`/`freeSpace`/
      `cleanDevCaches`/`cleanLinux`/`cleanMacOS`/`cleanWindows`/
      `ReclaimableSummary`/`optional` and the history file read/write
      wrappers are live; pure decision functions are already ~100%.
      `Apply` (added 2026-09-03) is a better seam than `Run` now exists
      — the OS-specific `clean*` functions and `optional` (subprocess
      exec for brew/docker/npm/etc.) still need their own injection.
- [ ] `internal/dupes` (68.4%, floor 68) — `Run`/
      `applyHardlinksWithConfirmation`/`confirmHardlink` are live
      (`os.Stdin` prompts, real hardlinking); `render` is already fully
      tested via stdout-capture.
- [ ] `internal/mcp` (68.7%, floor 68) — `tools()` registers each
      tool's `Handler` closure, which calls live `doctor.Assess`/`llm`/
      `gpu` functions only when actually invoked; existing tests
      deliberately avoid a real `tools/call` to dodge touching live
      system state. This one needs a real design decision, not just
      injection: either accept `doctor.Assess`/etc. as injectable
      dependencies of each `Handler` closure (so a fake report can flow
      through a real `tools/call`), or keep avoiding live state and
      instead unit-test each `Handler`'s JSON-RPC plumbing against an
      injected fake result. Decide before starting, don't improvise
      mid-implementation.
- [ ] `internal/guide` (72.0%, floor 72) — `Serve`/`ServeHTML`/
      `ServeLocal`/`openBrowser` are live (a blocking server loop, an
      OS browser-launch command). `allowedHostsOnly`/`safeLinkHref`/
      `sameOriginOnly` already 100%. `ServeLocal`'s own non-blocking
      setup (listener creation, handler wrapping) may be separable from
      the actual blocking `Serve()` call it makes at the end — worth
      checking whether that split is even possible before assuming the
      whole function is irreducible.
- [ ] `internal/metrics` (75.2%, floor 75) — `collect`/`RunOnce`/
      `Serve` are live (real `Collect` + HTTP server). Same
      Collect-vs-Analyze-style split as `doctor`/`memcheck` may apply.
- [ ] `main` / `vitals` package (40.9%, floor 40) — `run()`'s dispatch/
      validation logic is already unit tested; the remaining gap is
      each subcommand's actual `Run`/`RunFocus` call
      (doctor/clean/dupes/tools/memhogs/memcheck/gpu/monitor/advice/
      llm/metrics.Serve/mcp.Serve/guide --web/dashboard.Serve). Once
      each of those packages' own `Run` becomes more injectable per the
      tasks above, revisit whether `main`'s dispatch can inject through
      to them too, or whether `cli_smoke_test.go`/`dashboard_smoke_test.go`
      remain the right validation layer for this specific
      one-line-per-subcommand dispatch code — a real design question,
      not an assumed yes.

Already at or near the target, no work needed: `internal/config` (99%),
`internal/help` (99%), `internal/diag` (96%), `internal/dashboard`
(91% — see its own `check_coverage.py` comment for the two remaining
genuinely-unreachable-defensive-code gaps), `internal/ui` (96%),
`internal/memhogs` (59.7% — re-measure, may be closer than it looks
since `describe`/`userFamilies` are already covered and `Run`/`once`/
`readCgroup` may be the only real gap left).

## Exit criteria

Every package in `check_coverage.py` at 95%+ raw coverage, or (for
`main`'s `os.Exit`-driven dispatch specifically, the one true exception
per `index.md`) a floor with an explicit, reviewed justification for
why 95% genuinely cannot apply — not "it's live glue" alone, since this
item exists specifically because that justification is no longer
accepted as sufficient by itself.
