# Implementation plan — 005 Dashboard write actions

[docs](../../../index.md) / [Roadmap](../../index.md) / [005 — Dashboard write actions](index.md) / **Implementation plan**

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

**Hard gate**: no task below starts until item 001's Host-header fix has
been in `main` for at least one full CI cycle. (Met — item 001 is done.)

## Tasks

- [x] Design the CSRF/same-origin model as its own short design note,
      reviewed by a seven-persona panel (all seven: go-with-changes) —
      [`design.md`](design.md). Implemented as `sameOriginOnly`
      (`internal/guide/serve.go`, applied generically inside
      `ServeLocal`) and the `WriteAction` registry
      (`internal/dashboard/module.go`, `routeWrite` in `dashboard.go`) —
      commits `170e08e`, `9da3dd5`. See `design.md`'s "As built" section
      for exactly what shipped versus the original draft.
- [x] Rewrite `internal/clean` to expose what a `/clean/preview` write
      action needs — commit `a9c8efb`. `ReclaimableSummary(budget)`
      already measured without deleting (no new function needed for
      preview, per `design.md` §4). Added `Apply(home, opts) Result`
      (`FreedBytes`, `FreeBefore`/`FreeAfter`, `Records
      []PurgeRecord` — one entry per non-empty purge location, the
      per-category breakdown; a failed removal simply isn't credited,
      rather than a separate error list, matching the existing
      `purgeContents`/`removeTree` accounting already verified this
      session not to over-count on partial failure) instead of only
      `error`. `Run` is now a thin CLI wrapper over it — no behavior
      change, confirmed by the existing test suite.
- [x] Mandate `html/template` for the new write-action render
      functions, with a crafted-filename regression test — commit
      `7265006`. `internal/dashboard/modules_clean.go`'s
      `renderCleanPreview` goes through `cleanPreviewTmpl`
      (`html/template`); `TestRenderCleanPreviewEscapesACraftedPath`
      proves a `<script>`-bearing cache path renders escaped.
- [x] Wire `clean --dry-run`-equivalent to a dashboard `POST
      /clean/preview` write action and a button — commits `7265006`,
      `c2dd226`. `POST /clean/preview` calls `clean.ReclaimableSummary`
      directly and returns the rendered result; a new "Clean" nav page
      (`/clean`) has a Preview button that fetch()es it and injects the
      response — the dashboard's first client-side JS, vanilla, no
      framework. Verified end to end against a real running dashboard
      (curl, and `dashboard_smoke_test.go`'s `assertRoute("clean", ...)`
      / `assertPostRoute("clean/preview", ...)` cases) — no filesystem
      mutation.

      Wiring the backend surfaced and fixed a real, previously
      invisible bug (commit `7265006`): `dashboard.Serve`'s own
      `http.HandlerFunc` never dispatched `POST` requests to
      `routeWrite` at all — every POST silently 404'd through the GET
      `route` path instead, even though `routeWrite` itself was fully,
      correctly unit tested. No unit test of `routeWrite` in isolation
      could have caught this; only a real request against the live
      server could, and now does — see
      `feedback-verify-write-actions-live` in project memory.

      Wiring the button surfaced a real accessibility bug before it
      shipped too (commit `c2dd226`): the first `.btn:hover` style used
      `--ok-bg` as background with accent-colored text, which
      `TestPaletteMeetsWCAGAAAForNormalText` proved only reaches
      6.32:1/6.83:1 contrast, below the required 7:1 — fixed by
      switching to `--bg`, an already-verified-AAA combination.
- [x] A security-focused review of the concrete `/clean/apply` design
      (design.md §7), before implementation — see design.md's "Security
      review outcome (2026-09-03)". Verdict: go-with-changes, nothing
      blocking. Closed the preview→apply single-use token question
      (not needed — see next item); flagged as an accepted, non-blocking
      limitation that a hung `optional()` subprocess call (no timeout
      anywhere in `internal/clean`) wedges the single-flight guard for
      the life of the server process, confirmed live during this same
      pass (`brew cleanup -s` made real network calls under a throwaway
      `$HOME`); confirmed the confirm-body validation and request/
      response shape are sound for a route that actually deletes.
- [x] A single-flight guard against concurrent `/clean/apply` calls —
      `cleanApplyMu sync.Mutex` + `TryLock`, `internal/dashboard/
      modules_clean.go`, matching design.md §7's own sketch (simpler
      than `prepareAdviceCache`'s TTL-cache shape: no caching semantics
      wanted here, just outright rejection of a concurrent second call).
      Concurrency itself is tested with two real goroutines racing
      through `handleCleanApply`
      (`TestHandleCleanApplyRejectsAConcurrentSecondCall`), not just a
      pre-locked mutex.
- [x] The actual apply/confirm flow for `clean` — `POST /clean/apply`
      registered as a second `WriteAction` alongside `/clean/preview`,
      `internal/dashboard/modules_clean.go`. Body must be exactly
      `{"confirm": true}`; missing/false/malformed is rejected 400
      before `clean.Apply` ever runs (via an injected `cleanApplyFn`,
      swappable in tests — `clean.Apply` in non-dry-run mode is never
      safe to call from an automated test on any OS, matching
      `internal/clean/clean_test.go`'s own reasoning for why only
      `DryRun: true` is exercised there). Response mirrors
      `renderCleanPreview`'s `html/template` shape exactly
      (`renderCleanApplyResult`), with its own crafted-path escaping
      test. Client-side: an "Apply" button, hidden until a preview
      response has rendered, gated by a `window.confirm()` prompt before
      its own POST — the CLI's interactive y/N confirmation, in spirit,
      on top of (not instead of) the same-origin check that's the actual
      security boundary. Verified end to end against a real running
      dashboard: `GET /clean` (button markup), `POST /clean/preview`,
      `POST /clean/apply` with missing/false confirm (400), cross-origin
      Origin header (403), and a real confirmed apply against a
      throwaway `$HOME` that actually deleted the target file and
      returned "Freed: 50.00 KB". `dashboard_smoke_test.go` gained cases
      for the confirm-validation and cross-origin paths — deliberately
      not a real same-origin apply, mirroring `cli_smoke_test.go`'s own
      exclusion of `clean` without `--dry-run` (see this same commit's
      `assertCrossOriginPostRejected`/`assertPostRouteWithBody`).
- [x] Consider a preview→apply single-use token as defense-in-depth —
      considered and **not implemented**, per the security review above:
      `sameOriginOnly` is the real boundary (applied to every non-GET/
      HEAD request on `ServeLocal` uniformly), and a same-origin replay
      is the user's own browser doing what the user already asked it to
      do, not a new threat a token would close. Adding one would give
      the dashboard its first piece of server-side session state for a
      threat §1 already excludes. This closes the question rather than
      leaving it open.
- [ ] `dupes`/`--hardlink` exposure — design note drafted
      ([`design-dupes.md`](design-dupes.md), 2026-09-04): a fixed
      server-side scope enum (no client-chosen paths, unlike
      `/clean/apply` which takes none), a preview→apply pair of
      `WriteAction`s re-running `Scan` server-side on apply, a
      `context`/budget bound added to `Scan` so an expensive walk can't
      wedge the single-flight mutex, and `html/template` responses
      mirroring the `clean` ones. **Still open**: a security-persona
      pass on that note (four open questions listed in it), then the
      `Scan` `context`/`Truncated` refactor, then the two routes +
      client buttons + tests.

## Exit criteria

A security-focused review (at minimum a dedicated security-architect-
persona agent pass, per `AGENTS.md`) signs off on the CSRF/auth model
specifically, before any write route ships. **Met** for the
`sameOriginOnly`/`WriteAction` foundation; a real mutating write action
(`clean` apply) still needs its own pass before it ships, per the tasks
above.
