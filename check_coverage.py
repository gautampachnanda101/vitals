#!/usr/bin/env python3
"""Per-package coverage floor.

A single blended coverage number over `./...` lets one well-tested package
(diag at 97%) carry a barely-tested one (main.go's CLI wiring at 3%) without
the gate ever noticing — which is exactly how two real bugs (a disk-table
ANSI/padding bug, `advice` dumping raw Markdown to the terminal) shipped
through a passing CI run. This enforces a floor per package instead.

Floors below are each package's own measured coverage at the time they were
set (rounded down), so today's numbers can never silently regress. Packages
whose logic is genuinely pure (already tested from fixtures, no live OS/
network/subprocess calls) should keep climbing toward 95%+; packages
dominated by live glue code (exec.Command wrappers, network probes, watch
loops, main's os.Exit-driven dispatch) are validated by other means instead
(the CLI smoke test, httptest fakes) and are not expected to hit that bar by
adding mocks for their own sake — see AGENTS.md's Testing conventions.

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
    # llm, metrics.Serve/RunOnce, mcp.Serve, guide --web, dashboard.Serve) —
    # real subprocess/network/filesystem/server work, validated by
    # cli_smoke_test.go and (for dashboard specifically, since it never
    # exits on its own) dashboard_smoke_test.go exec'ing the real binary
    # instead.
    "vitals": 40,
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
    # memcheck: Run is the only untested function — four live gopsutil
    # calls (host.Info, mem.VirtualMemory/SwapMemory/SwapDevices) feeding
    # already-100%-covered pure functions (memVerdict, verdict, printIf),
    # the same Collect-then-Analyze shape as internal/doctor. Nothing left
    # to extract without testing gopsutil itself.
    "vitals/internal/memcheck": 33,
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
    # monitor: Run/sample/readDiskCounters/readNetCounters/topProcesses
    # are all live gopsutil/process calls (the Collect side of this
    # package); every pure formatting function (emit, bar, rate,
    # memBreakdownLine, ioDelta) is at 100%.
    "vitals/internal/monitor": 41,
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
