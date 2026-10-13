# Implementation plan - 014 Native deep disk usage analysis

[docs](../../../index.md) / [Roadmap](../../index.md) / [014](index.md) / **Implementation plan**

## Remaining tasks

- [x] Complete seven-persona design review and incorporate its concrete
  requirements (all seven: go-with-changes).
- [x] Obtain explicit maintainer approval of revised scope before implementation.
- [x] TDD shared scanner: exact recursive child totals, largest regular
  files, deterministic ties, distinct root files, safe symlink handling,
  missing/unreadable roots, per-child errors, limits and cancellation.
- [x] Prove bounded scheduling with high-fanout/deep fixtures and injected
  slow filesystem operations; test queue shutdown without leaks/deadlocks.
- [x] Assert lexical ties independent of traversal order, top-N truncation,
  zero-byte/root files, per-child partial lower bounds, root failure,
  error counts beyond 20 samples, every entry/depth/root-state cap, nested
  mount exclusion and symlink replacement races.
- [x] Prove cancellation stops queued work and closes handles once injected
  blocked operations return; preserve admission until worker termination.
- [x] Wire native CLI scan flags, actionable rankings and drill-down,
  output-file/JSON parity, error exit codes, and unchanged no-scan behavior.
- [x] Replace dashboard-only scanner with shared logic and asynchronous
  fragments, home-confined drill-down, bounded cache and single flight.
- [x] Add actual-binary scan smoke cases against isolated synthetic trees;
  assert exact bytes, ranks, error states and no filesystem mutation.
- [x] Cover top 1/100 and invalid bounds, budget bounds, flags without scan,
  quiet/CI conflicts, partial/error exit-code precedence, parsed JSON/file
  equality and matching schema contracts. Snapshot tree metadata/content
  before and after scans to prove no additions, deletions or modifications.
- [x] Add browser tests proving loading resolves, drill-down changes scope,
  rejected paths stay rejected, errors are visible, and pages are responsive
  while an injected scan is slow.
- [x] Use injected clock/scanner counts to prove identical-request coalescing,
  option-aware keys, TTL expiry, cache eviction and busy distinct requests;
  prove cross-origin scan GET rejection and safe crafted filename rendering.
- [x] Update README, help, completion, guide/site and schema contract only with the
  implemented behavior; keep existing pending cleanup changes intact.
- [x] Verify native build, vet, staticcheck, race tests, per-package coverage,
  docs consistency, all release cross-build targets and browser E2E.
- [x] Record real read-only scan evidence on this Mac: scope, budget,
  completeness, largest observed consumers and caveats. No cleanup.

## Verification evidence (2026-10-12)

- Staticcheck 2026.2.2 (0.8.2) runs cleanly across all packages on Go 1.27.
- Actual-binary fixture: 9,010 logical bytes; largest directory/file 6,000
  bytes and root file 3,000 bytes. JSON stdout and saved output match;
  contents, permissions, modification times and tree entries remain unchanged.
  Complete/partial/missing-root process exits match their JSON and health
  verdicts. A complete scan does not suppress a live critical health finding.
- Read-only `.ollama` scan: 60 entries, complete, 2 ms. Models contain
  6,981,550,381 logical bytes, independently matched by `find` + `stat`;
  the largest model blob is 5,347,914,400 bytes. This is application-owned
  storage, not an automatic cleanup candidate.
- Home scan with a 2s budget: 334,087 entries in 2,008 ms, explicitly partial
  with three access errors. Largest observed directories included Library
  (20,902,599,999 bytes, incomplete), `.ollama` (7,372,351,933 bytes,
  complete) and `.vscode` (4,002,413,225 bytes, complete). The 10 GiB Claude
  VM image and 4,269,932,544-byte Chrome model were visible. These are logical
  path sizes, not allocated space or guaranteed recovery; home-only scope
  excludes system data, other users and snapshots. No data was deleted.
- `go test -race -coverprofile=... ./...`, `go build ./...`, `go vet ./...`,
  Windows/Linux amd64 builds and `./build.sh ci` (all six targets) passed.
  Raw coverage: scanner 98.3%, dashboard 95.6%, metrics 96.9%; raised floors
  pass. Main 50.0%, advice 43.4%, doctor 79.6%, dupes 94.4% and guide 94.0%
  remain below the repository's 95% raw target.
- Browser loading, cross-origin rejection, real fixture drill-down/Up/rescan
  and invalid-scope visible recovery are verified. Budget expiry now passes
  its regression after separating the response wait from the scanner's
  cooperative budget, allowing partial rankings to be delivered.
- Staticcheck 2026.2.1 (latest available module v0.8.1) cannot decode Go 1.27
  export-data version 5. It has not produced source diagnostics; staticcheck
  sign-off remains blocked, so this item is not Done or release-ready.
- Remaining dashboard browser suite: 25 passed; the pre-existing Advice
  commentary test remained on "Asking the LLM" beyond 85s with cloud
  credentials disabled. That test still depended on the real local Ollama:
  disabling cloud credentials did not isolate it. The E2E harness now starts
  a zero-dependency Node HTTP stub on an ephemeral loopback port, passes its
  URL to the real dashboard binary and removes cloud API keys from that
  child process. The stub serves model discovery and validates chat requests;
  the browser asserts its exact commentary, not merely a disappearing
  placeholder. The focused Advice test passed in 491 ms; rebuilding
  `./vitals` and running `cd e2e && npm test` passed all 27 tests in 9.8s,
  including budget expiry. This proves the HTTP/UI integration, not real
  model availability or inference speed. Both test servers stop with the
  Playwright harness.

## Exit criteria

- Scanner package has at least 95% raw statement coverage; existing
  package coverage floors do not regress. Cover new CLI/dashboard live glue
  through injected seams; the repository-wide 95% raw target remains in
  force, with pre-existing package shortfalls reported, not exempted.
- Synthetic fixture totals/rankings exactly match known byte counts and
  every file remains present and unchanged after CLI and dashboard scans.
- Limits and permission failures never produce an unqualified complete
  result; canceled work drains without races or stranded workers.
- Browser first render does not wait on the scanner. Repeated requests
  prove one active scan and no more than four cached roots.
- CLI and dashboard expose equivalent results for the same root/options.
- No claim of allocated-space recovery, universal safe deletion, or
  complete filesystem accounting from a home-only scan.
- No version tag or release until evidence is reviewed by the maintainer.
