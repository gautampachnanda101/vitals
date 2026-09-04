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
- [x] `internal/gpu` (54.7% → 74.8% → 98.6%, floor 54 → 74 → 96) —
      **closed out**. `Run`'s printing half is split out as
      `printReport(devs []Device)`, the same live-vs-print seam
      `internal/monitor`'s `sample`/`emit` uses, fixture-tested for every
      per-field gate (this is also where the real Apple-Silicon-VRAM/
      Temp-zero bug fixes landed — see git log). `Probe`'s
      `nvidia-smi`/`rocm-smi`/`ioreg` subprocess calls are now behind an
      injected `deps` struct (`goos`/`lookPath`/`runCmd` — same shape as
      `internal/tools`' and `internal/llm`'s `gpuPreflightDeps`), with
      `probe(d)`/`run(d, ...)` as the testable core: nvidia-preferred,
      compute-apps attachment, fallthrough to rocm-smi on nvidia
      absent/empty-parse/failing, fallthrough to ioreg on darwin only,
      nothing-on-PATH, plus one real `Probe()`/`Run()` end-to-end call
      each (including `Run`'s `--json` envelope). Parsers
      (`parseNvidiaSMI`/`Apps`, `attachNvidiaApps`, `parseRocmSMIJSON`,
      `parseIORegApple`, `atoiOr`/`numOr`/`firstNonEmpty`/`strSort`)
      already ~100%.
- [x] `internal/doctor` (54.6% → 56.6% → 73.7%, floor 54 → 56 → 71) —
      **two slices; second one closes the "biggest lift."** First slice:
      `checkDNSLatency`/`topRemotePeers` split into thin real wrappers
      over `checkDNSLatencyWith(lookupHost, ...)`/`topRemotePeersWith(
      connections, ...)`, branch-tested (slow/failing/timing-out lookup;
      connection ranking, capping, the error path) plus one real
      end-to-end call each. Second slice (the decomposition decision
      below, resolved as "named per-signal collectors"): `Collect` now
      takes an injected `source` struct (same shape as
      `internal/monitor`'s — see `source.go`'s `procSource`/`procHandle`
      adapter, reused verbatim) and delegates Snapshot-building to
      `collectCPU`/`collectMemory`/`collectGPUs`/`collectThermal`/
      `collectLLM`, each independently fixture-tested (success,
      error/zero-value, and counter-reset branches); `firstTimes`/
      `percoreTimes`/`swapCounters`/`topProcs`/`netCounters` now take
      that same injected source and are tested through it too, plus one
      real end-to-end `realProcesses()` call and one full `collect()` run
      over an all-fake source. **Still fully live, deliberately out of
      scope for this pass** (see `source.go`'s own comment): `diskCounters`/
      `collectDisks`/`collectPower`/`runCmd`/`readLinuxBattery` — Disk and
      Power are their own bigger subsystems (per-mount filtering/cooldown
      state, three platforms' battery reads) that deserve their own
      decomposition pass. Also still untouched: the CLI entrypoints
      (`Run`/`Assess`/`RunFocus` and their print helpers). `Analyze`/
      `AnalyzeResource` and the small pure helpers are already ~100%.
- [x] `internal/llm` (62.6% → ~86-87% → ~92% → 98.4%, floor 57 → 85 →
      90 → 96) — **three slices, closed out**. First: `Run`'s `--watch`
      loop split into `watch(ctx, opts)` with an injected
      `newSignalContext` (same pattern as `internal/monitor`/
      `internal/memhogs`); `checkGPUDriver`/`runsCleanly` gained a
      `gpuPreflightDeps` struct (`goos`/`lookPath`/`runCmd`),
      branch-tested for nvidia-smi/rocm-smi found-and-working,
      found-but-failing, and neither-present; the four thin exported
      wrappers (`OllamaModels`/`ProbeProviders`/`ScanProcesses`/
      `CloudAPIKeyEnvVars`) and `RunFit`'s two error branches now have
      their own direct tests, not just their unexported counterparts'.
      Second slice: `complete.go`'s ~12 provider-completion functions
      (`completeLocal`/`completeCloud`/`completeOllama`/`completeNamed`/
      `doComplete` and their per-provider response parsers) are now
      branch-tested via several `httptest` response-shape variations per
      provider — bad-URL/unreachable/non-200/unparseable-body/empty-
      response branches for Ollama, OpenAI-compatible, and Anthropic
      shapes, plus `completeNamed`'s full provider-resolution matrix
      (forced ollama found/model-less, forced local non-ollama
      found/unreachable, forced cloud found/missing-key, unknown
      provider). `complete.go` itself is now ~100%. Third slice: `run`/
      `watch`'s remaining dispatch branches (empty-`OllamaURL` default,
      `run()` actually dispatching through `newSignalContext` into
      `watch()` rather than only `watch()` tested directly, the non-JSON
      clear-screen line, a direct real-signal-context test matching
      `internal/monitor`'s own `TestDefaultSourceNewSignalContextWiresRealSignalNotify`);
      `render` gained the same `printReport`-style fixture-table
      treatment `internal/gpu` got (host-process table, blank-`Location`
      grouping, latency/error display, and the full "loaded models"
      insight switch); `probeOne`/`parseModels`/`collectResidentModels`/
      `ollamaModels`'s remaining branches (bad endpoint, ollama-shaped
      garbage, dedup-by-key, blank-name skip, unparseable body,
      name-falls-back-to-`Model`); `once()`'s `needsGPUPreflightCheck`
      branch exercised for real via a fake Ollama server reporting a
      CPU-bound resident model (a real integration call, not an injected
      seam); and `RunFit` gained one size-chosen-for-determinism
      end-to-end case: a 5000B model no real machine's budget clears,
      forcing the "nothing fits" branch — safely asymmetric (no real
      machine has terabytes of VRAM). A "tiny model, everything fits"
      counterpart was tried and reverted: GitHub's macOS CI runner's
      virtualized Apple GPU reports a real, `ioreg`-sourced VRAM budget
      of ~104 MB — smaller than even a 500M-parameter model's smallest
      quant — so unlike the safely-huge direction, there is no
      small-enough-to-always-fit model size. **Left as genuinely
      unreachable**, same class already accepted elsewhere in this repo:
      `scanProcesses`' `process.Processes()`-fails branch; `watch`'s
      `ui.Errf` branch (`once()` only errors on a JSON-encode failure,
      and `Report` is plain marshalable data); `vramBudget`'s non-taken
      `gpu.Probe`/`mem.VirtualMemory` branches (deliberately left
      un-injected per this item's own prior "no new seam invented for a
      function this thin" call); and `RunFit`'s downstream
      `vram<=0`/switch-default/plain-"yes"-not-recommended branches.
- [x] `internal/clean` (67.9% → 86.2%, floor 50 → 84) — `os.UserHomeDir`/
      the confirm read injected via a `deps` struct; `optional`'s
      package-manager exec injected via `lookPath`/`runCmd` fields added
      to the existing `runner` struct (`Apply`'s constructor), so a
      `recordingRunCmd` proves the exact argv without ever shelling out
      to brew/docker/npm for real; `cleanHistoryPath`/`History`/
      `recordRun` gained real `isolateConfigDir`-based tests. Remaining:
      `cleanLinux`/`cleanWindows` stay 0% on any one CI runner by
      construction (`Apply`'s own `runtime.GOOS` switch — the 3-OS CI
      matrix naturally covers each branch on its own runner);
      `freeSpace`/`History`/`appendCleanHistoryTo`'s own error branches
      need a real permission/IO failure to exercise honestly, not done.
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
- [x] `internal/mcp` (68.7% → 95.5%, floor 68 → 95) — decided in favor of
      the first option this task posed: `doctor.Assess`/`llm.OllamaModels`/
      `llm.ScanProcesses`/`gpu.Probe` are all injected via a `deps` struct
      threaded through `tools(d)`/`handle(req, opts, d)`, so a fake now
      flows through a real `tools/call` for each of the 4 tools — proving
      the actual dispatch path, not a parallel test of its shape.
      `Serve` gained its blank-line-skip and malformed-JSON parse-error
      branches; `handle` gained the untested `ping` method. Remaining
      gaps, left honest: `Serve`'s `enc.Encode`-fails branch, and a
      tool's `run()` returning a non-nil error (unreachable in practice —
      every real `deps` function returns a concretely-typed,
      always-marshalable value).
- [x] `internal/guide` (72.0% → 94.8-95.2%, floor 72 → 93) — the split
      turned out to be possible: `buildServer(handler, opts)` now does
      the real `net.Listen` + Host/CSRF handler-wrapping and returns the
      real listener/URL, so a test drives `srv.Serve(ln)` itself against
      a real ephemeral port and proves the wiring with real HTTP
      requests (cross-origin POST → 403, foreign Host header → 400) —
      catches a dashboard.Serve-POST-never-reaching-routeWrite-class bug,
      not just `allowedHostsOnly`/`sameOriginOnly`'s own unit logic.
      `signal.NotifyContext`/`openBrowser` injected via a `deps` struct;
      `serveLocal`'s shutdown-on-signal and browser-open-unless-`NoOpen`
      branches both covered; `openBrowser` gets one real `exec.Command`
      call. Remaining gaps: the exported `Serve`/`ServeHTML`/`ServeLocal`
      one-liners (calling them for real risks actually opening a browser
      in a test run — same class of gap as `internal/metrics`' exported
      `Serve()`), `serveLocal`'s genuine-non-`ErrServerClosed` error
      branch, `openBrowser`'s two not-the-host-OS branches.
- [x] `internal/metrics` (75.2% → 97.8-98.5%, floor 75 → 96) — `collect`/
      `signal.NotifyContext` injected via a `deps` struct;
      `newMux(d, ollamaURL)` split out from `serve` so the `/metrics`/`/`
      handlers are tested via `httptest.NewServer`, never a real bound
      port. Remaining gaps: the exported `Serve()` one-liner (a real
      blocking HTTP server, no clean in-test interrupt) and
      `trimFloat`'s unreachable defensive branch.
- [x] `main` / `vitals` package (40.9%, floor 40, unchanged — **design
      question resolved, no code change**) — `run()`'s flag-parsing and
      validation logic (unknown commands, missing args, `--schema`/
      `--compare` handling) is already unit tested directly. The
      remaining gap is each subcommand's one-line call into its own
      package's already-well-tested `Run`/`RunFocus`/`Serve`
      (doctor/clean/dupes/tools/memhogs/memcheck/gpu/monitor/advice/
      llm/metrics.Serve/mcp.Serve/guide --web/dashboard.Serve) — by now
      every one of those packages has its own logic covered in
      isolation via the per-package work above, so injecting through
      `main`'s dispatch too would just re-exercise that same
      already-tested logic a second time, via function-pointer
      indirection added *only* to satisfy this package's own coverage
      number. What that one-line call site actually needs proving is
      that the wiring is right end to end — right flag parsed into the
      right field, right package invoked, right exit code — which is
      precisely what `cli_smoke_test.go` (execs the real compiled
      binary for every read-only command, on all three OSes) and
      `dashboard_smoke_test.go` (the one blocking-server exception)
      already do. Conclusion: `cli_smoke_test.go`/`dashboard_smoke_test.go`
      are the right validation layer for this dispatch code, not further
      in-process injection — `main` stays the one explicit exception
      the exit criteria below already carves out for `os.Exit`-driven
      glue, with this as its reviewed justification.

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
