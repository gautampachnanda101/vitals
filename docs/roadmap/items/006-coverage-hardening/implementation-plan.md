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
- [ ] `internal/memcheck` (32.3%) — `Run`'s live gopsutil calls stay
      exempt; check whether more of its formatting can follow the
      `internal/monitor` pattern from item 001.
- [ ] `internal/monitor` (34.2%) — already raised once this session
      (15.5%→34.2%); `sample`/`topProcesses`/`readDiskCounters`/
      `readNetCounters` are the live core and likely stay exempt, but
      re-check for any remaining pure formatting.
- [ ] `internal/advice` (34.8%) — `Run`'s live LLM call is exempt;
      `Generate` already has good coverage from item 001, check for gaps.
- [ ] `internal/clean` (41.0%) — real filesystem operations dominate;
      look for pure decision logic (what counts as a cache dir, size
      thresholds) separable from the actual `os.Remove` calls.
- [ ] `internal/gpu` (46.9%) — `Probe`'s subprocess calls are exempt; the
      output *parsers* (`parseNvidiaSMI` etc., per memory of this
      package's design) should already be near 100% pure-tested — verify,
      don't assume.
- [ ] `internal/doctor` (50.1%) — large package; `Analyze`/
      `AnalyzeResource` should already be near-100% (the correlation
      engine's whole design point). Audit `Collect` and its helpers for
      any pure sub-logic not yet split out the way `diskGrowthRate`/
      `withDiskHistory` were.
- [ ] `internal/dupes` (51.1%) — hashing/comparison logic should be pure
      and testable; the live directory walk is the exempt part.
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
