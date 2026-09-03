# 002 — `vitals dashboard` MVP

[docs](../../../index.md) / [Roadmap](../../index.md) / **002 — `vitals dashboard` MVP**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Done — see `implementation-plan.md` for what shipped and a
noted follow-up (richer overview visuals/machine detail) raised after,
not yet scoped as its own item.
**Depends on**: [001](../001-dashboard-foundation/)
**Target release**: [v0.5.0](../../releases/v0.5.0.md)
**Architecture**: [design doc](../../../architecture/design.md) §5.1, §6

## What

The actual `vitals dashboard` command: the HTTP handler wired on top of
item 001's fixed foundation, `main.go` wiring, a smoke test, docs, and the
discovery/first-run polish a real product surface needs. This is what
"the binary serves the GUI" concretely means once shipped.

## Why

This is the direct answer to "the binary is the product, it can serve
content" and the capability-gated plugin architecture — see `DESIGN.md`
§3 for how that direction was set, and §6 for the architecture it's built
from and the review that validated it.

## Plan

[`implementation-plan.md`](implementation-plan.md)
