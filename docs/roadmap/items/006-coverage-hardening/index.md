# 006 — Coverage hardening to the 95%+ hard rule

[docs](../../../index.md) / [Roadmap](../../index.md) / **006 — Coverage hardening to the 95%+ hard rule**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Done — every package in `implementation-plan.md` is either at
95%+ or has a documented live-glue ceiling in `check_coverage.py`.
**Depends on**: —
**Target release**: ongoing (not gated to v0.5.0)
**Architecture**: `AGENTS.md`'s "Testing conventions" — 95%+ of a
package's pure/testable logic is a hard rule (2026-09-03).

## What

Bring every package up to 95%+ of its pure/testable logic, per the hard
rule confirmed 2026-09-03. A 2026-09-03 audit found only 2 of 19 packages
(`dashboard` 98%, `diag` 97%) actually cleared it; this item tracks
closing that gap for the rest.

## Why

Stated directly: "yes that's a hard rule for agents." Not folded into an
existing item because it's cross-cutting (touches every package) rather
than tied to one feature, and because packages should be brought up
opportunistically as work touches them in addition to this dedicated
pass, not treated as a one-time backfill that then goes stale again.

## Approach

For each package: run `go tool cover -func` to find under-covered
functions, then for each one, ask "is this pure logic sitting next to a
live call" (test it directly, extracting a seam if the live call and the
logic are currently tangled together — see `withDiskHistory`,
`internal/dashboard`'s `Prepare` hook, `advice.Generate` for the pattern
this session already established) or "is this genuinely irreducible live
glue" (a blocking loop, an OS command, `main`'s dispatch — leave it, but
don't let it hide adjacent pure logic). Package floors in
`check_coverage.py` ratchet up as each package clears its new bar.

## Plan

[`implementation-plan.md`](implementation-plan.md) — one section per
package, worked through in priority order (most extractable pure logic
first; genuinely live-glue-dominated packages like `main`/`tools`/`gpu`
last, since they need the most judgment about what's truly irreducible).
