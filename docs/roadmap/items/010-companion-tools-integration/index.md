# 010 — Companion tools: real integration, not just a catalog

[docs](../../../index.md) / [Roadmap](../../index.md) / **010 — Companion tools: real integration, not just a catalog**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: In progress — `nvtop` shipped (2026-09-05); `jdupes` and
`smartctl` scoped with real validated findings, implementation not
started, per this item's own "Ground rules" below
**Depends on**: `internal/tools`' registry/`Launch` (existing)
**Target release**: not yet
**Architecture**: this item's own [`implementation-plan.md`](implementation-plan.md)

## What

`internal/tools` lists 8 companion tools (`gdu`, `ncdu`, `dust`, `btop`,
`htop`, `nvtop`, `jdupes`, `smartctl`) and can install or launch some of
them, but auditing the actual code (2026-09-05) found only 2 of 8 —
`gdu`/`ncdu`/`dust` under `vitals explore`, `btop`/`htop` under
`vitals live` — have any real handoff at all. `nvtop`, `jdupes`, and
`smartctl` were catalog-only: listed for `tools install`, never read
from, never delegated to, never backing a real signal anywhere else in
vitals. This item is the work to close that gap for those three,
specifically:

- **`nvtop`** → a `vitals gpu --live` flag handing off to it, the same
  `tools.Launch` pattern `explore`/`live` already use.
- **`jdupes`** → an optional, faster backend for `vitals dupes`'/the
  dashboard's duplicate scan, when installed.
- **`smartctl`** → real S.M.A.R.T. health/wear data feeding
  `vitals disk`'s findings, which today have no health signal at all —
  only usage percentage.

## Why this is its own item

Same reasoning items 005/008 already used: real, user-visible new
signals/backends, not a quick pattern application — `jdupes` and
`smartctl` both surfaced genuine open design questions once their real
CLI output was actually validated (see `implementation-plan.md`), not
assumed from documentation.

## Ground rules

- **Never assume a companion tool is installed.** Every integration
  gates behind `internal/tools`' own `installed(d, t)` check (or
  equivalent); vitals' own existing signal/backend is always there and
  correct with zero companion tools present — a companion tool only
  ever adds to what's already shown, never replaces the baseline as a
  hard dependency.
- **Keep the integration point extensible for tools added later.**
  Follow `internal/tools`' existing registry/category pattern rather
  than a one-off integration hardcoded to today's three tools.
- **Validate real output before writing a parser.** Every field this
  item's own code reads from a companion tool's output must have been
  observed from a real invocation in this session (`smartctl -a -j`,
  `jdupes -j`), not assumed from `--help` text or documentation —
  matching this repo's own `mcp` capability discipline applied here to
  local CLI tools instead of a remote API.
- **Don't silently degrade an existing field.** If a faster backend
  can't populate something the current one does (see `jdupes`'
  `ScannedFiles`/`ScannedBytes` gap below), that's a real design
  question to resolve deliberately, not something to ship as a silent
  zero.

## Plan

[`implementation-plan.md`](implementation-plan.md)
