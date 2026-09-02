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
- [ ] **Add `Prepare func(*PageContext) error` to `Module`**; route the
      advice module's `llm.Complete` call through it instead of a
      handler-level special case. Done when: nothing in item 002's handler
      dispatch code branches on a specific slug string.
- [ ] **Cache `Collect()`/`probeProviders()` behind a short TTL +
      single-flight guard**, shared across concurrent requests
      (`internal/dashboard`, e.g. a new `snapshot_cache.go`). Done when: a
      test issuing N concurrent requests shows exactly one underlying
      `Collect()`/probe call, and a cold request is bounded (the cached
      path is sub-second; only one goroutine at a time pays the
      uncached-refresh cost).
- [ ] **Parallelize provider probes** (`internal/llm`) — per-target
      goroutines instead of a sequential loop, each still
      individually timeout-bounded.
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
