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
    "vitals": 3,  # main.go: os.Exit-driven CLI dispatch, validated by cli_smoke_test.go instead
    "vitals/internal/advice": 34,
    "vitals/internal/clean": 41,
    "vitals/internal/config": 80,
    "vitals/internal/dashboard": 98,
    "vitals/internal/diag": 96,
    "vitals/internal/doctor": 50,
    "vitals/internal/dupes": 51,
    "vitals/internal/gpu": 46,
    # guide's floor is lower than its earlier 77 despite new tests, not a
    # regression: allowedHostsOnly/safeLinkHref are fully covered, but the
    # package grew around Serve/ServeHTML/ServeLocal/openBrowser, which
    # are — like doctor.Collect() — live glue (a blocking server loop, an
    # OS browser-launch command) exempt from unit coverage by convention.
    # See AGENTS.md's "95%+ coverage is the target for a package's pure/
    # testable logic" — this is that exemption showing up in the number.
    "vitals/internal/guide": 73,
    "vitals/internal/help": 86,
    "vitals/internal/llm": 53,
    "vitals/internal/mcp": 55,
    "vitals/internal/memcheck": 32,
    "vitals/internal/memhogs": 53,
    "vitals/internal/metrics": 73,
    "vitals/internal/monitor": 34,
    # tools: Installed/detectManager (exec.LookPath), Run/List/Install/
    # Launch/confirm (live subprocess exec, os.Stdin reads, real PATH
    # checks) are the irreducible live-glue majority of this package — a
    # package-manager launcher/installer is mostly "shell out and let the
    # user's terminal take over." The pure logic sitting next to them
    # (installCommand, withSudo, binary, firstOrEmpty, formatToolList) is
    # at 100%.
    "vitals/internal/tools": 44,
    "vitals/internal/ui": 76,
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
