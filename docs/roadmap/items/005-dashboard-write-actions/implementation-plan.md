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
- [ ] Rewrite `internal/clean` to expose what a `/clean/preview` write
      action needs: reuse `ReclaimableSummary(budget)` for the preview
      (no new cleanup logic), and a proper `Apply`-style function
      returning structured data (bytes freed, per-category breakdown,
      partial failures) instead of only `error`.
- [ ] Mandate `html/template` for the new write-action render
      functions, with a crafted-filename regression test (matching the
      migration already done for every read-only render function in
      007's follow-up work).
- [ ] Wire `clean --dry-run`-equivalent (the new preview function above)
      to a dashboard `POST /clean/preview` write action and a button —
      read-only, no filesystem mutation.
- [ ] A single-flight guard against concurrent `/clean/apply` calls
      (same shape as `prepareAdviceCache`'s single-flight pattern in
      `internal/dashboard/modules_advice.go`).
- [ ] Only after the above ships and is reviewed: an actual apply/confirm
      flow for `clean`, matching the CLI's own interactive-confirmation
      safety default in spirit — including `/clean/apply`'s
      non-blocking/progress-feedback story and the actual UX spec (what
      preview shows, per-category exclusion, partial-failure display).
- [ ] Consider a preview→apply single-use token as defense-in-depth
      (security reviewer's recommendation, not a hard requirement — the
      same-origin check is the real security boundary here).
- [ ] `dupes`/`--hardlink` exposure, if still wanted at this point,
      follows the same pattern.

## Exit criteria

A security-focused review (at minimum a dedicated security-architect-
persona agent pass, per `AGENTS.md`) signs off on the CSRF/auth model
specifically, before any write route ships. **Met** for the
`sameOriginOnly`/`WriteAction` foundation; a real mutating write action
(`clean` apply) still needs its own pass before it ships, per the tasks
above.
