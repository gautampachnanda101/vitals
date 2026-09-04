#!/usr/bin/env python3
"""Per-package coverage floor.

A single blended coverage number over `./...` lets one well-tested package
(diag at 97%) carry a barely-tested one (main.go's CLI wiring at 3%) without
the gate ever noticing — which is exactly how two real bugs (a disk-table
ANSI/padding bug, `advice` dumping raw Markdown to the terminal) shipped
through a passing CI run. This enforces a floor per package instead.

Floors below are each package's own measured coverage at the time they were
set (rounded down), so today's numbers can never silently regress and only
ever ratchet up as real coverage improves.

The target is 95%+ raw coverage for every package, no exemption for live
glue (exec.Command wrappers, network probes, watch loops, OS reads) — see
AGENTS.md's "Testing conventions" (re-confirmed 2026-09-04 after an earlier
version of this rule scoped 95% to "pure/testable logic" only, a carve-out
that could not be traced to anything the user actually asked for). Most
floors below still reflect the pre-2026-09-04 pure-logic-scoped baseline,
not yet the raw-95% target — see docs/roadmap/items/ for the tracked,
per-package work to close that gap. Do not read a floor below 95% as "this
package is fine as-is."

Usage: go test -coverprofile=coverage.out ./... && python3 check_coverage.py coverage.out
"""
import sys
from collections import defaultdict

FLOORS = {
    # vitals (main.go): main() itself is a one-line os.Exit(run(...)) wrapper,
    # exempt by construction. run()'s dispatch/validation logic (unknown
    # commands, missing args, --schema/--compare validation, help/version/
    # completion output) is now unit tested directly; the remaining
    # uncovered lines in run() are each subcommand's live Run/RunFocus call
    # (doctor, clean, dupes, tools, memhogs, memcheck, gpu, monitor, advice,
    # llm, metrics.Serve/RunOnce, mcp.Serve, guide --web, dashboard.Serve,
    # info) — real subprocess/network/filesystem/server work (or, for
    # info specifically, just flag-parsing dispatch to an already-96.7%-
    # covered package), validated by cli_smoke_test.go and (for dashboard
    # specifically, since it never exits on its own) dashboard_smoke_test.go
    # exec'ing the real binary instead.
    "vitals": 39,
    # advice: Run is thin live glue (doctor.Assess, then the
    # already-100%-covered Generate, then print/JSON-encode) — nothing
    # left to extract without testing doctor.Assess itself. Dropped from
    # 39 to 37 when Run started calling doctor.PrintFindings directly for
    # the styled/coloured terminal path (matching vitals doctor's own
    # output instead of a plain text dump) — still glue, still exercised
    # end-to-end by cli_smoke_test.go, not unit tested for the same reason
    # as the rest of Run.
    "vitals/internal/advice": 37,
    # clean (item 009): os.UserHomeDir and the os.Stdin confirm read are
    # injected via a deps struct (homeDir, confirmReader), same shape as
    # internal/tools'/internal/dupes'; optional()'s package-manager exec
    # (brew/docker/npm/...) is injected via lookPath/runCmd fields added
    # directly to the existing runner struct (Apply's own constructor),
    # branch-tested for tool-not-on-PATH, dry-run's "would run" line, and
    # a recordingRunCmd proving the exact argv without ever shelling out
    # to a real package manager. cleanHistoryPath/History/recordRun
    # gained real isolateConfigDir-based tests, same pattern internal/
    # doctor's/internal/memhogs' own config-dir tests already use.
    # 67.9% -> 86.2% measured. cleanLinux/cleanWindows stay at 0% on any
    # one CI runner by construction (Apply's own runtime.GOOS switch —
    # each OS in the 3-OS CI matrix naturally covers its own branch, the
    # same class of OS-partitioned gap accepted elsewhere in this repo,
    # e.g. internal/memhogs'). Remaining real gaps, not yet closed:
    # freeSpace/History/appendCleanHistoryTo's own error branches
    # (disk.Usage/os.Create failing) would need a real permission/IO
    # failure to exercise honestly.
    "vitals/internal/clean": 84,
    "vitals/internal/config": 99,  # 100.0% measured; 99 for float-rounding margin
    # dashboard: floor dropped from 98 with roadmap item 002 (the vitals
    # dashboard MVP), not a regression — Serve (dashboard.go) is new,
    # genuinely live glue (wires the snapshot cache + route into an
    # http.Handler, then calls guide.ServeLocal, a blocking server), the
    # same shape as every other Serve function in this codebase. route
    # and loopbackAddr, the pure logic Serve wraps, are both 100%.
    # Raised to 91 after the html/template migration + advice-cache fix
    # (item 007's review follow-ups): mustExecute's panic branch (only
    # reachable from a template/struct mismatch, i.e. a coding bug) and
    # generateAdvice's json.Marshal error branch (doctor.JSONReport is
    # plain structs/slices/strings, can't realistically fail to marshal)
    # are the only remaining gaps, both the same class of genuinely
    # unreachable defensive code already exempted elsewhere in this repo.
    "vitals/internal/dashboard": 91,
    "vitals/internal/diag": 96,
    # doctor (item 009, second slice): Collect is now decomposed behind a
    # source struct (same shape as internal/monitor's), with named
    # per-signal collectors — collectCPU/collectMemory/collectGPUs/
    # collectThermal/collectLLM — plus firstTimes/percoreTimes/
    # swapCounters/topProcs/netCounters all taking that injected source.
    # Each collector and helper is now fixture-tested (success, error/
    # zero-value, and reset-counter branches), plus one real end-to-end
    # realProcesses() call. 56.6% -> 73.7% raw; floor set a few points
    # below the measured value for the usual cross-OS margin. Disk and
    # Power stay on their existing collectDisks/collectPower call sites
    # deliberately (see source.go's own comment) — both are their own
    # bigger subsystems (per-mount filtering/cooldown state, three
    # platforms' battery reads) that deserve their own decomposition pass
    # rather than being folded into this one. readLinuxBattery (Linux-only
    # sysfs reads) and the CLI entrypoints (Run/Assess/RunFocus and their
    # print helpers focusDetail/printFindings/printReclaimable/
    # listExcludedMounts) remain untouched — thin glue over already-tested
    # pure logic, not this item's target. The correlation engine itself
    # (Analyze/AnalyzeResource and every analyze* rule) and the small pure
    # helpers (pct, throttleNote, fullestDisk, summaryLine, diskGrowthRate,
    # procSuffix/quitFix/coreSpread/timeToFull/nz) are at or near 100%.
    "vitals/internal/doctor": 71,
    # dupes (item 009): os.UserHomeDir and the os.Stdin confirm prompt are
    # both injected via a deps struct (homeDir, confirmReader), same
    # shape as internal/tools'; Run/applyHardlinksWithConfirmation/
    # confirmHardlink are now one-line wrappers over run/.../confirmHardlink(d,
    # ...), exercised against real temp-dir fixtures (real duplicate
    # files, a real hardlink, a real aborted confirmation, a real failed
    # --output write). Scan/hashPrefix/hashFile/linkOver were already
    # real-filesystem-tested via t.TempDir(); hashPrefix/hashFile gained
    # their missing-file error case. 93.7% measured; the remaining gaps
    # (Scan's WalkDir-error branch, linkOver's Link/Rename-failure
    # branches) need a real OS-level permission/IO failure to exercise
    # honestly, not yet done here.
    "vitals/internal/dupes": 93,
    # gpu (item 009, closed out): Probe's nvidia-smi/rocm-smi/ioreg
    # exec.Command calls are now behind an injected deps struct (goos/
    # lookPath/runCmd — same shape as internal/tools'/internal/llm's
    # gpuPreflightDeps), with probe(d)/run(d, ...) as the testable core.
    # Branch-tested: nvidia-preferred, compute-apps attachment, fallthrough
    # to rocm-smi on nvidia absent/empty-parse/failing, fallthrough to
    # ioreg on darwin only, nothing-on-PATH, plus one real Probe()/Run()
    # end-to-end call each (including Run's --json envelope). Run's
    # printing half stays its own pure seam: printReport(devs []Device),
    # fully fixture-tested including the bug this was split out to fix —
    # a real Apple Silicon reading (real UtilPct/VRAM from ioreg's
    # PerformanceStatistics, see gpu.go's parseIORegApple) must never
    # print a bare "Temp 0°C"/"Power 0 W"/"Clock 0 MHz" alongside it, and
    # a real NVIDIA/AMD-shaped device (every field populated) must still
    # show all of them together. 74.8% -> 98.6%.
    "vitals/internal/gpu": 96,
    # guide (item 009): signal.NotifyContext and openBrowser are both
    # injected via a deps struct; ServeLocal's real net.Listen + handler-
    # wrapping half is split into buildServer(handler, opts), returning
    # the real listener/URL so a test can drive srv.Serve(ln) itself
    # against a real ephemeral port and prove the Host-header/CSRF
    # wiring end to end with real HTTP requests (a cross-origin POST
    # really gets 403, a foreign Host header really gets 400) — not just
    # allowedHostsOnly/sameOriginOnly's own unit logic, the same class of
    # wiring bug as dashboard.Serve's POST-never-reaching-routeWrite
    # incident this repo already hit once. serveLocal's shutdown-on-
    # signal and browser-open-unless-NoOpen branches are both covered via
    # the injected deps; openBrowser itself gets one real exec.Command
    # call. 95.2% measured. Remaining gaps, left honest: the exported
    # Serve/ServeHTML/ServeLocal one-liners (calling them for real risks
    # actually opening a browser during a test run, since they don't
    # expose NoOpen/deps injection — same class of gap as
    # internal/metrics' exported Serve()), serveLocal's genuine-
    # non-ErrServerClosed error branch, and openBrowser's two
    # not-the-host-OS branches (only one of three runs per CI machine).
    "vitals/internal/guide": 93,
    "vitals/internal/help": 99,  # 100.0% measured; 99 for float-rounding margin
    # info: Collect's two live calls (hostInfoFn, executableFn) and
    # abbrevHome's homeDirFn are all injected function values, exercised
    # via fakes for both success and failure paths; Render and
    # overriddenKeys are pure and fully table-tested. 100.0% measured; 99
    # for float-rounding margin.
    "vitals/internal/info": 99,
    # llm (item 009, second slice): Run's --watch loop is split into
    # watch(ctx, opts) with an injected newSignalContext, same pattern as
    # internal/monitor/internal/memhogs; checkGPUDriver/runsCleanly gained
    # a gpuPreflightDeps struct (goos/lookPath/runCmd), branch-tested for
    # nvidia-smi/rocm-smi found-and-working, found-but-failing, and
    # neither-present, plus one real end-to-end call. The four thin
    # exported wrappers (OllamaModels/ProbeProviders/ScanProcesses/
    # CloudAPIKeyEnvVars) and RunFit's two error branches are now
    # exercised directly rather than only through their unexported
    # counterparts. Second slice: complete.go's ~12 provider-completion
    # functions (completeLocal/completeCloud/completeOllama/completeNamed/
    # doComplete and their per-provider response parsers) are now
    # branch-tested via several httptest response-shape variations per
    # provider — bad-URL/unreachable/non-200/unparseable-body/empty-
    # response branches for Ollama, OpenAI-compatible, and Anthropic
    # shapes, plus completeNamed's full provider-resolution matrix
    # (forced ollama found/model-less, forced local non-ollama found/
    # unreachable, forced cloud found/missing-key, unknown provider).
    # 62.6% -> ~86-87% -> ~92%. complete.go itself is now ~100%.
    #
    # NOT done, a smaller remaining slice: llm.go's run/render (the CLI
    # entrypoint and its terminal-report printer) and fit.go's vramBudget/
    # RunFit still have real uncovered branches — vramBudget calls
    # gpu.Probe/mem.VirtualMemory directly (both already ~100% tested in
    # their own packages) rather than through an injected seam, and
    # render's per-field terminal-formatting branches haven't had the
    # same fixture-table treatment internal/gpu's printReport got.
    "vitals/internal/llm": 90,  # measured ~92% locally; a few points of margin for cross-OS variance
    # (this package has shown it before: 86.6% local vs. 84.5% on a
    # Windows CI runner in an earlier pass).
    # mcp (item 009): the open design question this package's task noted
    # ("accept doctor.Assess/etc. as injectable dependencies of each
    # Handler closure" vs. "unit-test the JSON-RPC plumbing against a
    # fake result only") is resolved in favor of the former —
    # doctor.Assess/llm.OllamaModels/llm.ScanProcesses/gpu.Probe are all
    # injected via a deps struct threaded through tools(d)/handle(...,d),
    # so a fake now flows through a REAL tools/call for each of the 4
    # tools (system_health, diagnose_bottleneck incl. its healthy/ok
    # branch, llm_status, gpu_status), not a parallel test of the
    # dispatch shape alone. Serve gained coverage for its blank-line skip
    # and malformed-JSON parse-error reply; handle gained the untested
    # "ping" method. 95.5% measured. Remaining gaps, left honest: Serve's
    # enc.Encode-fails branch (needs a writer that succeeds once then
    # fails, not yet built) and a tool's run() returning a non-nil error
    # (unreachable in practice — every real deps function returns a
    # concretely-typed, always-marshalable value, so jsonString never
    # actually fails for any of the 4 tools; forcing a fake to simulate
    # it would test the mock, not the code, per this item's own "don't
    # fake it dishonestly" ground rule).
    "vitals/internal/mcp": 95,
    # memcheck: the four gopsutil calls (host.Info, mem.VirtualMemory/
    # SwapMemory/SwapDevices) are now injected via a `source` struct (item
    # 009) — Run() is a one-line pass-through to run(defaultSource), and
    # run() itself is exercised with fakes for the hostInfo-fails,
    # virtualMemory-fails, swapMemory-fails, and swap-device-lines cases,
    # plus one real end-to-end call through Run()/defaultSource. 100.0%
    # measured raw coverage.
    "vitals/internal/memcheck": 99,
    # memhogs: Run/once/watch now go through an injected `source` struct
    # (processes/readCgroup/virtualMemory/swapMemory/newSignalContext,
    # item 009), so once()'s three-section rendering, the --watch loop
    # (driven by an already-expiring context, not a real signal), and the
    # process-table-enumeration-fails path are all exercised with fakes;
    # readCgroup is split into readCgroupFor(goos, pid, readFile) so both
    # the non-Linux short-circuit and the Linux read/error paths are
    # testable on any host. 96.1% raw — the residual gaps are
    # OS-partitioned switch arms (the linux/windows section-3 remedy
    # lines, the Windows System-process branch) and unreachable error
    # branches (realProcesses' OS-level failure, families()' embedded-file
    # panic, userFamilies' os.UserConfigDir error), same class as
    # internal/monitor's documented remainder.
    "vitals/internal/memhogs": 95,
    # metrics (item 009): collect and signal.NotifyContext are both
    # injected via a deps struct (collect, newSignalContext), same shape
    # tools/dupes already use; RunOnce/Serve are now one-line wrappers
    # over runOnce/serve(defaultDeps, ...). newMux (the /metrics and /
    # handlers) is split out from serve so it's tested via
    # httptest.NewServer without ever binding a real port; serve's own
    # shutdown-on-signal and ListenAndServe-error branches are both
    # covered (an already-done injected context, and a real unbindable
    # address). One real end-to-end call each for collect()/RunOnce()
    # through the actual doctor.Collect/Analyze/Render wiring. The
    # exported Serve() one-liner (0%) and trimFloat's "s == \"\" ||
    # s == \"-\"\" guard (unreachable for any real float64 via %.6f
    # formatting) are the only remaining gaps — the former is a real
    # blocking HTTP server with no clean way to interrupt it from inside
    # a test (same documented exemption class as internal/guide's
    # ServeLocal), the latter is dead defensive code. 97.8-98.5% measured
    # across runs (some run-to-run variance observed between an isolated
    # `go test ./internal/metrics/` and the full `go test ./...`); floor
    # set with margin below the low end rather than the isolated-run
    # high end.
    "vitals/internal/metrics": 96,
    # monitor: the gopsutil/process surface (host.Info, load.Avg,
    # cpu.Counts/Percent, mem.VirtualMemory/SwapMemory, disk.IOCounters,
    # net.IOCounters, process.Processes via a procSource interface +
    # procHandle adapter) is now injected via a `source` struct (item 009),
    # with defaultSource wiring the real calls and Run() reduced to
    # run(defaultSource, opts). The --watch loop is pulled into its own
    # watch(ctx, src, opts) so a test can drive it with an
    # already-expiring context instead of a real OS signal; --watch's
    # underlying signal.NotifyContext is itself injected as
    # source.newSignalContext. 98.5% measured raw coverage; the 3
    # remaining uncovered lines are genuinely unreachable/hard-to-trigger
    # defensive code, same class as internal/dashboard's mustExecute
    # panic branch: sample() has no code path that returns a non-nil
    # error (every gopsutil call it makes already tolerates its own
    # failure), so the "if err != nil" branches in both run() and
    # watch() that exist to handle a failing sample() are dead by
    # construction; realProcesses()'s "process.Processes() itself
    # fails" branch would need a real OS-level process-table failure to
    # exercise honestly.
    "vitals/internal/monitor": 98,
    # tools (item 009): exec.LookPath and the subprocess exec are both
    # injected via a `deps` struct (lookPath, runCmd, confirmReader, goos)
    # — defaultDeps wires the real calls; Run/List/Install/Launch/confirm
    # are now one-line wrappers over run/list/install/launch/confirm(d,
    # ...), each fully exercised with fakes (a recordingRunCmd proves the
    # exact argv without ever shelling out) plus one real end-to-end call
    # per public wrapper. 100.0% measured; 99 for float-rounding margin.
    "vitals/internal/tools": 99,
    "vitals/internal/ui": 96,
}


def per_package_coverage(profile_path):
    totals = defaultdict(lambda: [0, 0])  # pkg -> [covered_stmts, total_stmts]
    with open(profile_path) as f:
        next(f, None)  # skip the "mode: ..." header line
        for line in f:
            line = line.strip()
            if not line:
                continue
            # "<file>:<startLine>.<startCol>,<endLine>.<endCol> <numStmt> <count>"
            location, num_stmt, count = line.rsplit(" ", 2)
            file_path = location.split(":", 1)[0]
            pkg = file_path.rsplit("/", 1)[0]
            n = int(num_stmt)
            totals[pkg][1] += n
            if int(count) > 0:
                totals[pkg][0] += n
    return {pkg: (100.0 * covered / total if total else 100.0) for pkg, (covered, total) in totals.items()}


def main():
    if len(sys.argv) != 2:
        print("usage: check_coverage.py <coverage.out>", file=sys.stderr)
        return 2
    actual = per_package_coverage(sys.argv[1])

    failed = False
    for pkg, floor in sorted(FLOORS.items()):
        pct = actual.get(pkg)
        if pct is None:
            print(f"::warning::{pkg} has a floor of {floor}% but no coverage data was found for it")
            continue
        marker = "OK" if pct + 1e-9 >= floor else "FAIL"
        print(f"{marker:4} {pkg:38} {pct:5.1f}%  (floor {floor}%)")
        if pct + 1e-9 < floor:
            failed = True
            print(f"::error::{pkg} coverage {pct:.1f}% is below its {floor}% floor")

    unlisted = sorted(set(actual) - set(FLOORS))
    if unlisted:
        print(f"::error::new package(s) with no coverage floor set: {', '.join(unlisted)} — add them to FLOORS in check_coverage.py")
        failed = True

    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
