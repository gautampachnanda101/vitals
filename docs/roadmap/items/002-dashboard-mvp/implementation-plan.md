# Implementation plan — 002 `vitals dashboard` MVP

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

## Tasks

- [ ] Write `internal/dashboard/dashboard.go`: `Options{Addr, NoOpen,
      OllamaURL, ...}`, `Serve(opts) error`. Routing (path → `Module` via
      `findModule`), `PageContext` assembly (from item 001's cache, not a
      fresh `Collect()` per request), and the available-vs-unavailable-vs-
      404 branch must each be extracted as pure, independently
      unit-tested functions — none of this logic lives inline in the
      `http.HandlerFunc` closure.
- [ ] Wire `vitals dashboard [--addr] [--no-open]` into `main.go`,
      following the existing subcommand pattern (`serve`/`guide`).
- [ ] Verify (empirically, per `AGENTS.md`'s own rule) that
      `openBrowser`'s failure path on a bare Linux CI runner degrades to a
      warning, not an error, and that `--no-open` skips the call entirely.
- [ ] Integration/smoke test: start `vitals dashboard --addr 127.0.0.1:0
      --no-open`, hit the overview route and at least one
      conditionally-unavailable route (e.g. advice with no LLM
      configured), assert the "unavailable" page renders instead of a
      bare 404, send `os.Interrupt`, assert clean shutdown within a short
      bound. New infrastructure — vitals has no precedent for
      smoke-testing a server command today.
- [ ] `docs/user-guide.md`: new section for `vitals dashboard`, matching
      the style of the existing command sections (a runnable example,
      what it shows, what's capability-gated and why).
- [ ] Discovery hook: `doctor`'s terminal output gets a one-line footer
      suggesting `vitals dashboard` for a browsable view; `README.md`'s
      command table gets a row for it.
- [ ] First-run/empty-state polish: a short explanatory line on the
      overview page for someone landing on it cold; a version string in
      the footer (reuse `main.version`); a second footer line pointing at
      the GitHub repo for issues/feedback.

## Exit criteria

`vitals dashboard` is a real, documented, tested command; CI (3-OS
matrix, coverage floors, smoke test) is green.
