# 001 — Dashboard foundation fixes

[docs](../../../index.md) / [Roadmap](../../index.md) / **001 — Dashboard foundation fixes**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Done
**Depends on**: —
**Target release**: [v0.5.0](../../releases/v0.5.0.md)
**Architecture**: [design doc](../../../architecture/design.md) §6.4–6.8

## What

Every required fix the six-agent design review found in the
`internal/dashboard/` prototype and the `internal/guide` plumbing it
shares — caching, the Host-header/XSS security fixes, coverage floors.
Not user-visible on its own; it's the prerequisite that makes item 002
safe and fast to build on top of.

## Why

Ships before 002 on purpose: writing the HTTP handler (002) on top of an
uncached, unbounded-latency, Host-header-unchecked foundation would mean
either redoing that work once the review findings are addressed, or
shipping the findings unfixed. Every item in the plan traces to a specific,
independently-found review finding — see the [design doc](../../../architecture/design.md)
§6 for which reviewer(s) found what.

## Plan

[`implementation-plan.md`](implementation-plan.md)
