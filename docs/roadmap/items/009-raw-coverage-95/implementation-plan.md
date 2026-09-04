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

- [x] `internal/memcheck` (33.3% → 100.0%, floor 33 → 99) — extracted a
      `source` struct (`hostInfo`/`virtualMemory`/`swapMemory`/
      `swapDevices` function values) with `defaultSource` wiring the real
      gopsutil calls; `Run()` is now a one-line `run(defaultSource)`
      pass-through. New tests cover `run()`'s hostInfo-fails (Host line
      skipped, not fatal), virtualMemory-fails (fatal, wrapped error),
      swapMemory-fails (non-fatal warning), and swap-device/cumulative
      line branches via fakes, plus one real end-to-end call through
      `Run()` itself against the live host.
- [x] `internal/monitor` (41.2% → 98.5%, floor 41 → 98) — extracted a
      `source` struct (`hostInfo`/`loadAvg`/`cpuCounts`/`cpuPercent`/
      `virtualMemory`/`swapMemory`/`diskIOCounters`/`netIOCounters`/
      `processes`/`newSignalContext`) with `defaultSource` wiring the
      real gopsutil calls (including a `procSource` interface +
      `procHandle` adapter for `process.Processes()`, since `*process.
      Process`'s PID is a struct field rather than a method). `Run` is
      now `run(defaultSource, opts)`; the `--watch` loop is pulled into
      its own `watch(ctx, src, opts)` so a test can drive it with an
      already-expiring `context.Context` instead of a real OS signal,
      with the underlying `signal.NotifyContext` call itself injected as
      `source.newSignalContext`. The 3 remaining uncovered lines are
      genuinely unreachable: `sample()` has no path that returns a
      non-nil error, so the `err != nil` branches in `run()`/`watch()`
      built to handle a failing `sample()` are dead by construction, and
      `realProcesses()`'s `process.Processes()`-fails branch would need
      an actual OS-level process-table failure to exercise honestly.
- [x] `internal/memhogs` (59.7% → 95.7%, floor 59 → 95) — not originally
      listed as its own task (it was in the "already close" footer below);
      turned out to need the same treatment as monitor/memcheck. Injected
      `source` (`processes`/`readCgroup`/`virtualMemory`/`swapMemory`/
      `newSignalContext`); `readCgroup` split into `readCgroupFor(goos,
      pid, readFile)` so both the non-Linux short-circuit and the Linux
      read/error paths are testable on any host. Residual gaps are
      OS-partitioned switch arms and the same class of unreachable error
      branches as monitor's.
- [x] `internal/tools` (44.6% → 100.0%, floor 44 → 99) — `exec.LookPath`
      and the subprocess exec are both injected via a `deps` struct
      (`lookPath`, `runCmd`, `confirmReader`, `goos`); `Run`/`List`/
      `Install`/`Launch`/`confirm` are now one-line wrappers over
      `run`/`list`/`install`/`launch`/`confirm(d, ...)`. A
      `recordingRunCmd` fake proves the exact argv (e.g. `brew install
      ncdu`) without ever shelling out; `confirm`'s `io.Reader` is
      injected the same way `internal/clean/clean.go`'s `confirm` already
      does it. One real end-to-end call through each public wrapper
      (`defaultDeps`) keeps the actual wiring exercised too.
- [ ] `internal/gpu` (54.7% → 74.8%, floor 54 → 74) — partial: `Run`'s
      printing half is split out as `printReport(devs []Device)`, the
      same live-vs-print seam `internal/monitor`'s `sample`/`emit` uses,
      fixture-tested for every per-field gate (this is also where the
      real Apple-Silicon-VRAM/Temp-zero bug fixes landed — see git log).
      `Probe` itself still shells out to `nvidia-smi`/`rocm-smi`/`ioreg`
      directly and is still untested — inject the subprocess-exec call
      the same way `internal/tools`' `deps.runCmd` does, to close the
      rest of the gap. Parsers (`parseNvidiaSMI`/`Apps`,
      `attachNvidiaApps`, `parseRocmSMIJSON`, `parseIORegApple`,
      `atoiOr`/`numOr`/`firstNonEmpty`/`strSort`) already ~100%.
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
- [x] `internal/dupes` (68.4% → 93.7%, floor 68 → 93) — `os.UserHomeDir`
      and the `os.Stdin` confirm read are both injected via a `deps`
      struct (`homeDir`, `confirmReader`), same shape `internal/tools`'
      already uses; `Run`/`applyHardlinksWithConfirmation`/
      `confirmHardlink` are now one-line wrappers over `run`/`.../
      confirmHardlink(d, ...)`, tested against real `t.TempDir()`
      fixtures (a real duplicate pair, a real hardlink applied, a real
      aborted confirmation, a real failed `--output` write).
      `hashPrefix`/`hashFile` gained their missing-file error case.
      Remaining gap: `Scan`'s `WalkDir`-error branch and `linkOver`'s
      `Link`/`Rename`-failure branches need a real OS-level permission/IO
      failure to exercise honestly — not yet done.
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
- [x] `internal/metrics` (75.2% → 97.8-98.5%, floor 75 → 96) — `collect`/
      `signal.NotifyContext` injected via a `deps` struct;
      `newMux(d, ollamaURL)` split out from `serve` so the `/metrics`/`/`
      handlers are tested via `httptest.NewServer`, never a real bound
      port. Remaining gaps: the exported `Serve()` one-liner (a real
      blocking HTTP server, no clean in-test interrupt) and
      `trimFloat`'s unreachable defensive branch.
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

Already at or near the target, no work needed: `internal/config` (99%,
gained a generated `DefaultFileContents()` alongside `info`'s config-block
rework, still 100% measured), `internal/help` (99%), `internal/diag`
(96%), `internal/dashboard` (91% — see its own `check_coverage.py`
comment for the two remaining genuinely-unreachable-defensive-code gaps),
`internal/ui` (96%), `internal/info` (99%, raised alongside the same
config-block rework — see `check_coverage.py`'s comment).

## Exit criteria

Every package in `check_coverage.py` at 95%+ raw coverage, or (for
`main`'s `os.Exit`-driven dispatch specifically, the one true exception
per `index.md`) a floor with an explicit, reviewed justification for
why 95% genuinely cannot apply — not "it's live glue" alone, since this
item exists specifically because that justification is no longer
accepted as sufficient by itself.
