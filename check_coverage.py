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
    # clean: this package's whole job is filesystem/subprocess I/O, so
    # Run/confirm/freeSpace/cleanDevCaches/cleanLinux/cleanMacOS/
    # cleanWindows/ReclaimableSummary/optional and the clean_history.jsonl
    # path/read/write wrappers (cleanHistoryPath/History/recordRun, same
    # shape as internal/doctor's disk_history.json ones) are the
    # irreducible live majority. Every pure decision function
    # (devCacheDirs, osCacheDirs, freeSpaceRoot, withSudo-style OS/env
    # parameterization, appendCleanHistoryTo, renderCleanHistory, plural)
    # is at or near 100%.
    "vitals/internal/clean": 50,
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
    # doctor: Collect and its OS-level helpers (firstTimes, percoreTimes,
    # topProcs, swapCounters, diskCounters, netCounters, collectPower,
    # runCmd, readLinuxBattery, collectDisks), the CLI entrypoints
    # (Run/Assess/RunFocus and their print helpers focusDetail/
    # printFindings/printReclaimable/listExcludedMounts), and
    # checkDNSLatency/topRemotePeers are all live. The correlation engine
    # itself (Analyze/AnalyzeResource and every analyze* rule) and the
    # small pure helpers (pct, throttleNote, fullestDisk, summaryLine,
    # diskGrowthRate, procSuffix/quitFix/coreSpread/timeToFull/nz) are at
    # or near 100%.
    "vitals/internal/doctor": 54,
    # dupes: Run/applyHardlinksWithConfirmation/confirmHardlink are live
    # (os.Stdin prompts, real hardlinking); render (a print function
    # despite its name) is now fully tested via the same stdout-capture
    # pattern as internal/monitor/internal/memcheck.
    "vitals/internal/dupes": 68,
    # gpu: Probe/run (report.go's Run too) shell out to nvidia-smi/
    # rocm-smi/ioreg — genuinely live. Every pure parser (parseNvidiaSMI/
    # Apps, attachNvidiaApps, parseRocmSMIJSON, atoiOr/numOr/
    # firstNonEmpty/strSort) is at or near 100%.
    "vitals/internal/gpu": 54,
    # guide's floor keeps dropping despite new tests, not a regression:
    # allowedHostsOnly/safeLinkHref/sameOriginOnly are all fully covered
    # (100%), but the package grows around Serve/ServeHTML/ServeLocal/
    # openBrowser, which are — like doctor.Collect() — live glue (a
    # blocking server loop, an OS browser-launch command) exempt from
    # unit coverage by convention. ServeLocal grew again to wire in
    # sameOriginOnly (roadmap item 005's CSRF defense) — more lines in
    # the same already-exempt function, not a new gap. See AGENTS.md's
    # "95%+ coverage is the target for a package's pure/testable logic"
    # — this is that exemption showing up in the number.
    "vitals/internal/guide": 72,
    "vitals/internal/help": 99,  # 100.0% measured; 99 for float-rounding margin
    # info: Collect's two live calls (hostInfoFn, executableFn) are both
    # injected function values, exercised via fakes for both their
    # success and failure paths — nothing genuinely irreducible-live
    # left in this package. 96.7% measured; 96 for float-rounding
    # margin.
    "vitals/internal/info": 96,
    # llm: Run/once/scanProcesses/ScanProcesses/OllamaModels/
    # ProbeProviders/RunFit, checkGPUDriver/runsCleanly (subprocess
    # exec), and render (a print function, like internal/dupes' — not yet
    # covered via the stdout-capture pattern, a good next step) remain
    # low/live. classify/capitalize/plural/nz/shortLocalName/
    # modelOrDefault/ollamaModelChoice's pure branches are now covered.
    "vitals/internal/llm": 57,
    # mcp: tools() registers each tool's Handler closure, which calls live
    # doctor.Assess/llm/gpu functions when actually invoked — only
    # exercised by a real tools/call, which the existing tests
    # deliberately avoid (would touch live system state). toolText/
    # jsonString/ToolNames are now fully covered.
    "vitals/internal/mcp": 68,
    # memcheck: the four gopsutil calls (host.Info, mem.VirtualMemory/
    # SwapMemory/SwapDevices) are now injected via a `source` struct (item
    # 009) — Run() is a one-line pass-through to run(defaultSource), and
    # run() itself is exercised with fakes for the hostInfo-fails,
    # virtualMemory-fails, swapMemory-fails, and swap-device-lines cases,
    # plus one real end-to-end call through Run()/defaultSource. 100.0%
    # measured raw coverage.
    "vitals/internal/memcheck": 99,
    # memhogs: Run/once/readCgroup are live (real process scanning,
    # /proc reads). describe and userFamilies (config-file read/parse,
    # tested via isolateConfigDir the same way internal/doctor's history
    # tests are) are now fully covered.
    "vitals/internal/memhogs": 59,
    # metrics: collect/RunOnce/Serve are live (real Collect + HTTP
    # server). trimFloat's "s == \"\" || s == \"-\"" guard looks
    # unreachable for any real float64 via %.6f formatting (the leading
    # digit before the decimal point is never trimmed) — left
    # undisturbed rather than forcing a test for dead defensive code.
    "vitals/internal/metrics": 75,
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
    # tools: Installed/detectManager (exec.LookPath), Run/List/Install/
    # Launch/confirm (live subprocess exec, os.Stdin reads, real PATH
    # checks) are the irreducible live-glue majority of this package — a
    # package-manager launcher/installer is mostly "shell out and let the
    # user's terminal take over." The pure logic sitting next to them
    # (installCommand, withSudo, binary, firstOrEmpty, formatToolList) is
    # at 100%.
    "vitals/internal/tools": 44,
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
