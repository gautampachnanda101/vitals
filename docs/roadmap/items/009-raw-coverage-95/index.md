# 009 — Raw 95%+ coverage, no live-glue exemption

[docs](../../../index.md) / [Roadmap](../../index.md) / **009 — Raw 95%+ coverage, no live-glue exemption**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Not started
**Depends on**: — (cross-cutting, touches most packages)
**Target release**: ongoing, per-package
**Architecture**: `AGENTS.md`'s "Testing conventions" section (updated
2026-09-04) is the rule this item exists to satisfy; `check_coverage.py`
is the gate.

## What

Roadmap item 006 ("coverage hardening") brought every package to 95%+
of its own *pure/testable logic*, with live glue (real `exec.Command`
calls, OS/network reads, watch loops) left as a documented, unexercised
exemption in `check_coverage.py`. That scoping was written into
`AGENTS.md` as "confirmed explicitly by the user," but the maintainer
challenged that claim directly (2026-09-04) — no verbatim record of
them actually asking for that scope exists anywhere in this repo's
history. Given the direct choice, they chose **95%+ raw coverage of
every package, full stop, no live-glue exemption.**

This item is the work to actually close that gap: make the previously
"exempt" live glue genuinely testable — typically by turning a direct
`exec.Command`/gopsutil/`os.*` call into an injected function value or
small interface a test can substitute a fake for — then write the
tests, then raise that package's floor in `check_coverage.py` to match.

## Why this is its own item, not folded into ongoing work

Same reasoning item 006 itself used: this touches nearly every package
in the repo (see the task list) and is a genuinely large, mechanical
effort once the pattern is established per package. Tracking it as one
item keeps `docs/roadmap/index.md` honest about how much of the
codebase is actually at the new bar versus the old one, instead of
scattering "also raised coverage a bit" into unrelated feature commits.

## Ground rules (apply to every task below)

- **TDD**: write the failing test against the *not-yet-injectable* live
  call first is impossible by definition — so the order here is:
  extract/parameterize the live call into an injected value first (this
  step itself needs no new test, it's a pure refactor — confirm via the
  existing test suite staying green), *then* TDD the new test against
  the now-injectable seam.
- **Never bulk-raise a floor without the coverage to back it.** Raise
  `check_coverage.py`'s floor for a package only after real measured
  coverage for that package has actually gone up — floors ratchet up
  alongside real work, per `AGENTS.md`'s existing discipline, unchanged
  by this item.
- **Don't fake the exempt-in-principle cases dishonestly.** A test that
  mocks `exec.Command` just to satisfy a percentage, without the mock
  ever diverging from reality in a way that would catch a real bug, is
  worse than an honest gap — prefer injecting the *smallest* real seam
  (e.g. a `func(string, ...string) ([]byte, error)` for a subprocess
  call) so the test still proves real call-site logic (argument
  construction, error handling, output parsing), not just "the mock
  returned what I told it to."
- **`main`'s `os.Exit`-driven dispatch is the one true exception** — a
  process actually exiting cannot be unit tested by definition. Every
  other "live" function in the list below is a "how do we inject this,"
  not a "this can never be tested."
- Full verification before every commit (`gofmt -l .`, `go vet ./...`,
  `staticcheck ./...`, `go test -race ./...`, `check_coverage.py`,
  `check_docs.py`) — same gate the pre-commit hook already runs.
- No `Co-Authored-By: Claude` or any other AI-attribution trailer in
  commit messages — `.githooks/commit-msg` rejects it outright.

## Plan

[`implementation-plan.md`](implementation-plan.md) — the per-package
task list.
