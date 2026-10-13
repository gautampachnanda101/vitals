# 014 - Native deep disk usage analysis

[docs](../../../index.md) / [Roadmap](../../index.md) / **014 - Native deep disk usage analysis**

**Status**: Done.
**Target release**: v0.11.0.
**Depends on**: Existing filesystem and dashboard infrastructure only.
**Implementation plan**: [Tasks and exit criteria](implementation-plan.md).

## Problem and user-approved scope

The maintainer's installed v0.10.0 reports 83% disk usage, 39.94 GB free,
approximately 2.68 GB of cache candidates, and "no disk issue detected".
That does not explain the bulk of occupied space or support a useful
decision about reclaiming it. Capacity warnings currently start at 90%;
passing that threshold check is not proof there is enough space for the
user's next workload.

The maintainer selected native analysis in `vitals disk`, with CLI and
dashboard sharing the scanner. This is a ranked diagnostic summary and
path-by-path drill-down, not a replacement for ncdu's full interactive
file manager. No deletion, dependency installation, subprocess-based
scanner, or automatic threshold change is in scope.

## User contract

- Preserve existing `vitals disk` flags, output envelope, and exit-code
  semantics without an explicit scan. Clarify the healthy footer:
  no configured health threshold was exceeded, not "enough disk space".
- Add `vitals disk --scan`, defaulting to the user's home. Add `--root`
  for narrowing the scan, `--top` (default 20, valid 1-100), and
  `--scan-budget` (default 30s, valid greater than zero and at most 5m).
  Reject scan-only flags without `--scan`. Validate before collecting
  telemetry or walking files.
- Rank immediate child directories by recursively measured regular-file
  logical bytes, and separately rank the largest regular files anywhere
  under the selected root. List files directly under the root distinctly
  from directories. Break equal sizes by path for deterministic results.
- Show root, observed bytes, visited entries, elapsed time, completion,
  skipped symlinks/special files, and access errors. Partial rankings are
  "largest observed", never asserted to be the true largest.
- Supply a drill-down action for directory rows and a clear manual-review
  explanation for personal files. Large files are not implicitly disposable.
  Cache cleanup remains a separate preview-driven command. For supported
  cache roots, link to the cleanup preview; for model/application data,
  recommend the owning application's storage management, never direct blob
  deletion. Unknown paths get drill-down and manual review, not an invented
  disposable classification. Show home-scan exclusions directly on the page:
  system data, other users and snapshots are not accounted for.
- Dashboard extras: breadcrumb/up navigation, directory drill-down,
  explicit rescan, scope/elapsed/entry/error counts, and actionable completion
  messages. Empty-complete means no regular-file bytes found in this scope;
  failed/partial-empty is not "no files". Offer narrowing the root or increasing
  budget when limited; access failures explain which paths to review and
  never recommend running the dashboard as root.
- Sizes are logical bytes per path, not allocated bytes or guaranteed
  reclaim. Hardlinks, sparse files, APFS clones/compression and snapshots
  mean scanned bytes cannot be subtracted from filesystem usage to infer
  exact missing space. Never sum rows from different levels.
- `--scan --json` adds optional `disk_usage` inside `JSONEnvelope`.
  Scan-mode stdout and `--output` both use the resource-tagged envelope;
  preserve the existing no-scan stdout/file shapes unchanged. Extend the
  schema to allow optional `resource` and `disk_usage`, with contract tests
  for the actual focus payload and omitted fields on no-scan output.
  `disk_usage` has root, options, directory/file rows, observed_bytes,
  entries, elapsed_ms, complete, stop_reasons, skipped counts, error_count
  and bounded error samples; rows include path, bytes and complete.
  Incomplete rows are lower-bound observed sizes; exact fixture totals
  apply only to completely traversed children. Use the repository's
  schema/version/golden discipline once for the eventual release.
  Reject `--scan` with `--quiet` or `--ci`, rather than silently omitting
  the requested result. An incomplete/error scan must not exit as success;
  retain the more severe existing diagnostic exit code when applicable.
  Root-open failure is exit 2; partial scans are at least exit 1. The
  diagnostic verdict is unchanged; scan completeness is a separate state.

## Shared scanner and boundaries

Extract the disk-path sizing responsibility from
`internal/dashboard/disk_usage.go` into a focused `internal/diskusage`
package, with a typed result and validated options. Do not reuse the
current silent errors or a goroutine per immediate child. `Analyze`
remains pure; scans run at the CLI/dashboard call site.

- Enumerate directories using batched reads, not whole-directory
  `ReadDir`/`WalkDir` allocations. Maximum 1,000,000 visited entries,
  including root entries; depth 128, six workers, 256-entry read batches.
  Use a bounded channel of 64 immediate-child tasks; workers walk each
  child depth-first with depth-bounded state, and do not enqueue recursively.
  The root enumerator feeds workers and is the only queue producer.
  Limit retained root-child aggregates to 10,000, then stop and report
  root-list truncation; retain per-child states for these aggregates,
  return only top-N rows and overall counts. Bound file rankings to top-N.
- Inject filesystem operations and the clock for deterministic tests of
  blocked/error/cancellation paths. Keep live wiring thin and covered.
- Open a root handle and use rooted filesystem operations. Reject symlink
  roots/ancestors; skip descendant symlinks and special files. Do not read
  file contents or execute any names encountered. Explain symlink-root
  rejection with the real-directory action the user can take.
  Rooted operations are containment, not a no-symlink guarantee: lstat
  before descent and compare the opened directory's identity with that
  metadata. Close/skip replacements rather than following them.
- Stay on the root's filesystem where native volume identity is available;
  collect known mount boundaries at the live call site and skip nested
  mounts, recording counts/paths. Test Linux/macOS/Windows behavior and
  mark boundary detection unavailable rather than silently claiming
  filesystem-exclusive totals when the platform cannot establish it.
- Budgets are cooperative between filesystem operations, not a promise
  that Go can interrupt a blocked filesystem syscall. Do not claim a
  strict wall-clock upper bound on disconnected network filesystems.
  Cancellation stops queued work; handles release once in-flight calls
  return. The budget applies only to the scan, not preceding doctor
  collection; document the additive command latency.
- Record bounded error samples (at most 20), with full error counts,
  per-child completion state, and explicit root-open failure.
- CLI roots are explicit caller-authorized paths. Dashboard drill-down
  is limited to actual directories beneath the home root, validated with
  rooted operations; arbitrary paths or symlink escapes are rejected.
  Browser routes retain existing Host/Origin protections. Scan-triggering
  GETs additionally reject cross-site Fetch Metadata and foreign Origin
  when present; permit same-origin and headerless local clients.
- Terminal paths must not inject ANSI/control sequences; HTML uses
  `html/template` and JSON uses `encoding/json`. Drill-down uses structured
  paths/URL encoding, never interpolated shell commands. Visibly escape
  bidi/format controls in CLI and HTML display strings while retaining
  raw paths separately for rooted operations and URL construction.
  Dashboard passes its already-open selected directory to the shared scanner;
  scanning never reopens an absolute path after confinement validation.

## Dashboard and cost

Disk page HTML renders without waiting for a deep walk. Fetch the scan
through a same-origin fragment, show loading/partial/error states and
directory drill-down controls, and reuse the exact scanner semantics.
Bound concurrent scans globally (one active scan), coalesce identical
requests, and bound a cache (four entries, 90s TTL) so repeated
tabs do not create unbounded scans or cache growth. Reject excess distinct
requests with an actionable busy response rather than queue forever.
No scans run on other pages. Keep the plain CLI command's current cost.
Cache/coalescing keys include canonical root and all effective scan options.
Request cancellation stops waiting independently of shared scans. Normal
cooperative budget expiry must render the scanner's partial result, not
discard its observed rankings because the response deadline races the scan.
A separately bounded wait for draining operations can show a busy/timeout
state if they remain blocked, but the global scan slot is not released
until its actual workers exit. Never start
replacement workers to bypass a blocked syscall. Rescan observes the
same admission limits and explicitly bypasses stale cached results.

## Independent review outcome

All seven reviewers returned **go-with-changes**; none returned no-go.
Convergent requirements: exact JSON placement/contracts, truthful
per-child partial totals, cancellation versus blocked-call lifetime,
bounded root fanout, and nested mount scope. Unique additions: option-aware
cache keys, scan-GET cross-origin protection, filename bidi display safety,
empty/error recovery and safe application-owned cleanup guidance.
These requirements are incorporated above and in the acceptance checklist.
CLI/shared-scanner delivery is independently useful and can land before
dashboard enhancements, but this item's requested CLI/dashboard parity is
not done until both are verified. README discoverability is included.

## Verification and release gate

See [implementation-plan.md](implementation-plan.md). Independent design
review precedes implementation; review findings and explicit maintainer
approval are required before proceeding. Do not present the feature as
shipped in the user guide/site until it is actually built and verified.
