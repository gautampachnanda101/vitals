# Implementation plan — 001 Dashboard foundation fixes

This file shows what's **left**. Check off or delete a task as it lands —
don't leave completed work checked-but-present once the whole item ships;
at that point the item's README status changes to Done and this file
should read as a short, accurate completion record, not a growing log.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

## Tasks

- [ ] **Register panics on a duplicate Slug** (`internal/dashboard/module.go`).
      Done when: a test registering two modules with the same slug asserts
      a panic, matching `http.ServeMux.Handle`'s own behavior for a
      duplicate pattern.
- [x] **Add `Prepare func(*PageContext) error` to `Module`**; route the
      advice module's `llm.Complete` call through it instead of a
      handler-level special case. Also extracted `advice.Generate`
      (report JSON -> prompt -> LLM -> strip-fabricated-sources) as a
      shared function so `vitals advice` (CLI) and the dashboard's advice
      page use the exact same path instead of two copies. Failure is
      absorbed into `ctx.AdviceErr` rather than returned, so the page
      still renders a friendly message when no provider answers. Done
      when: nothing in item 002's handler dispatch code branches on a
      specific slug string — item 002 still needs to actually call
      `m.Prepare(&ctx)` uniformly for whichever module matched; this task
      only adds the mechanism and wires the one module that needs it.
- [x] **Cache `Collect()`/`probeProviders()` behind a short TTL +
      single-flight guard**, shared across concurrent requests
      (`internal/dashboard/snapshot_cache.go`). Hand-rolled (no new
      dependency): a 3s TTL, a mutex, and a `chan struct{}` closed when an
      in-flight refresh completes so concurrent callers wait on the same
      refresh instead of starting their own.
      `TestSnapshotCacheSingleFlightsConcurrentRefreshes` proves 20
      concurrent callers collapse into exactly 1 real refresh. The
      TTL/single-flight logic itself is unit-tested via an injectable
      `refresh func() cachedSnapshot` field — `newSnapshotCache` wires
      that to the real, live `doctor.Collect`/`doctor.Analyze`/
      `llm.ProbeProviders` (the last needed a small export,
      `llm.ProbeProviders`, added in the same change). Not wired into a
      live `Serve` yet — that's item 002.
- [x] **Parallelize provider probes** (`internal/llm`) — per-target
      goroutines instead of a sequential loop, each still
      individually timeout-bounded. `TestProbeProvidersRunsTargetsConcurrentlyNotSequentially`
      pins this with 4 artificially slow (150ms) targets: sequential was
      measured at ~609ms, concurrent finishes under 400ms. Race-clean
      (each goroutine writes a distinct result-slice index, no shared
      mutable state).
- [ ] **Host-header allow-list in `guide.ServeLocal`**
      (`internal/guide/serve.go`). Done when: a request with a
      non-matching `Host` header is rejected (400) before reaching the
      mux, with a regression test. This also hardens `guide --web`.
- [ ] **Scheme allow-list for Markdown links in `renderInlineHTML`**
      (`internal/guide/html.go`) — allow `http:`/`https:`/`mailto:`, strip
      or neutralize anything else (`javascript:`/`data:` specifically).
      Done when: a test feeding `[x](javascript:alert(1))` through
      `RenderFragment`/`RenderHTML` asserts no `href="javascript:` reaches
      the output.
- [ ] **Fix the dead assertion in `TestRenderAdviceShowsTheReply`**
      (`internal/dashboard/modules_test.go`) — the empty `if` block that
      currently proves nothing.
- [ ] **Add `vitals/internal/dashboard` to `check_coverage.py`'s
      `FLOORS`**, and fix `vitals/internal/guide` (currently failing its
      own floor at ~76.9% vs. 77%). Done when: `make coverage` exits 0.
- [ ] **Cover the 0%/low-coverage render branches**: `renderMem`,
      `renderPower`, `renderGPU`'s loop body, `renderNet`'s
      active-traffic row, `renderCPU`'s `FreqMHz`/`TopProc` rows
      (`internal/dashboard/modules_resource.go`).
- [x] **WCAG 2.2 Level AAA for the shared page shell** — `--muted`
      retuned to hold >=7:1 against every background it appears on (was
      as low as 4.18:1 in the original prototype); `:focus-visible`
      outline added; `<nav>`/`<main>` landmarks and `aria-current="page"`
      added. Pinned by `TestPaletteMeetsWCAGAAAForNormalText` in
      `internal/dashboard/render_test.go` so a future palette edit can't
      silently regress it. See `docs/architecture/design.md` §6.8 — item
      002 must re-verify this once real served pages/interactive elements
      exist, this only covers the shared shell.
- [ ] Non-blocking, do in the same pass since it's adjacent code: fix the
      unguarded concurrent read-modify-write on `disk_history.json`
      (`internal/doctor/diskhistory.go`) with a mutex; de-duplicate the
      "worst finding as headline" logic shared by
      `modules_overview.go`/`modules_resource.go` into one helper.

## Exit criteria

`go build ./...`, `go vet ./...`, `staticcheck ./...`,
`go test -race ./...`, and `make coverage` all green, every task above
landed and tested. Item 002 does not start before this is true.
