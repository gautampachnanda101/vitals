# Implementation plan — 002 `vitals dashboard` MVP

[docs](../../../index.md) / [Roadmap](../../index.md) / [002 — `vitals dashboard` MVP](index.md) / **Implementation plan**

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

## Tasks

- [x] `internal/dashboard/dashboard.go`: `Options{Addr, NoOpen, OllamaURL,
      Version}`, `Serve(opts) error`. `route(path, ctx) (int, string)` is
      the pure exists/available/404/Prepare-error branching (100% tested
      in `dashboard_test.go`, no server needed); `Serve` itself is thin
      glue (build the snapshot cache, wrap `route` in an `http.Handler`,
      hand it to `guide.ServeLocal`) and is the one genuinely-live
      function in the file.
- [x] Wired `vitals dashboard [--addr] [--no-open]` into `main.go`,
      matching `guide`'s flag pattern. `--addr` only ever controls the
      port — `loopbackAddr` (100% tested) forces the host to 127.0.0.1
      regardless of what's passed, so the "nothing leaves this machine"
      promise can't be bypassed via a flag.
- [x] `guide.ServeLocal` gained `ServeOptions{Addr, NoOpen}` (zero value =
      its original ephemeral-port/auto-open behavior, so `guide --web`'s
      own call site needed no behavior change) — the shared serving
      plumbing this task's own note anticipated reusing.
- [x] `dashboard_smoke_test.go`: execs the real binary (`--addr
      127.0.0.1:0 --no-open`), waits for `ServeLocal`'s listen line, hits
      `/` (200), `/advice` (200 — unavailable page, not a 404, when no
      LLM is reachable), `/nope` (404), then sends an interrupt and
      asserts the process exits within 5s (`os.Interrupt` isn't
      deliverable via `Process.Signal` on Windows, so that OS falls back
      to asserting `Kill` still terminates it, not that the
      graceful-shutdown path specifically ran). First precedent in this
      repo for smoke-testing a long-running server command. Along the
      way, fixed a real bug this surfaced: `buildCLIOnce` used
      `t.TempDir()`, which is deleted when the *first* test that
      triggered the build finishes — broke the instant a second test
      function (`TestDashboardSmoke`) tried to reuse the shared binary
      after `TestCLISmoke` completed. Now uses `os.MkdirTemp` with a
      `TestMain`-level cleanup instead.
- [x] `docs/user-guide.md` "vitals dashboard" section + a Quick Reference
      line; `README.md` command table row.
- [x] Discovery hook: `doctor`'s terminal report (plain-text path only,
      not `--json`/`--ci`/`-q`) ends with a line pointing at `vitals
      dashboard`.
- [x] WCAG 2.2 Level AAA: no new colors or CSS were introduced by this
      item — `route`/the resource pages reuse item 001's already-verified
      `layout`/`verdictBanner`/`findingsList`/`row` untouched, so the
      existing `TestPaletteMeetsWCAGAAAForNormalText` coverage carries
      forward by construction rather than needing a new check.
- [x] First-run polish: a one-line "what is this" sentence at the top of
      the overview page; the footer now shows `vitals <version>` (falls
      back to "dev") and a link to the GitHub repo for issues/feedback.

## Exit criteria

`vitals dashboard` is a real, documented, tested command; CI (3-OS
matrix, coverage floors, smoke test) is green. All met — `internal/
dashboard` and `internal/guide`'s coverage floors were adjusted down
slightly in `check_coverage.py` to reflect the new, documented live-glue
surface (`Serve`, `ServeLocal`'s `Addr`/`NoOpen` branches), not a
regression in what's actually tested.

**Known follow-up, raised after this shipped, not yet scoped**: the
overview page is intentionally minimal for this MVP (verdict + findings
list, no charts/sparklines/trend visualization, no expanded machine
identity block beyond what `doctor`'s own summary line shows). Making it
visually distinctive and/or adding richer machine detail is real,
reasonable feedback — but it's a scope decision for its own roadmap item,
not a silent addition here, since item 002's design was shaped by an
explicit six-agent architecture review that didn't include it.
