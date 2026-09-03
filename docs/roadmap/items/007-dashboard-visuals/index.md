# 007 — Dashboard visuals & machine identity

[docs](../../../index.md) / [Roadmap](../../index.md) / **007 — Dashboard visuals & machine identity**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Not started — deliberately unscheduled, see "Trigger" below
**Depends on**: [002](../002-dashboard-mvp/)
**Target release**: not yet — see Trigger
**Architecture**: not yet written; the seven-agent review that created
this item (2026-09-03, on the maintainer's request to re-litigate
deferring this) is the record of what shaped its scope, until a real
design doc exists

## What

Two things the dashboard doesn't do today, raised by the maintainer right
after item 002 shipped: make the overview visually distinctive (trend
charts/sparklines, not just the current verdict banner), and show more
about the machine itself (hostname, OS, uptime — beyond the CPU/mem/disk
percentages `doctor` already surfaces).

## Why this is its own item, not folded into 002 or built immediately

Item 002 shipped under an explicit six-agent review that didn't include
either of these. Rather than add them silently afterward, or treat the
maintainer's "build it now" instinct as settled without checking it, a
second seven-agent panel (3 architects, security, PM, QA, performance)
independently reviewed the specific question "should this be pulled
forward now." **All seven returned go or go-with-changes; none argued
for building the broad feature now.** Convergent reasoning: the
`Module`/`PageContext`/`route` architecture (`internal/dashboard`) is
already additive-extensible for both — a new page is a new file calling
`Register`, a new `PageContext` field is a zero-migration struct
addition — so there's no retrofit cost being avoided by acting early.
Building charts or an identity model now, with no reviewed feature
shaping them, risks guessing wrong on retention window/resolution
(charts) or scope creep into data with no diagnostic value (identity)
more than it saves engineering time later.

Two things the panel found *were* worth doing immediately, precisely
because they're cheap now and get more expensive the more the dashboard
grows around the current gap — these are being fixed directly as
follow-up commits, not gated on this item:
- The dashboard never records to `doctor`'s history file (calls
  `Collect` directly, skipping `recordHistory`) — a dashboard-only user
  accumulates zero trend data today, and history is time-based, so this
  can't be backfilled once noticed later.
- `/advice`'s `Prepare` hook calls a real LLM completion on every
  request, unlike every other route, which is fully cache-protected — an
  already-shipped bug, not a groundwork question.

## Trigger

Not scheduled to a release. Revisit after items 003 (product site) and
004 (native launcher) ship and at least one real usage signal comes back
through the dashboard footer's GitHub-issues link (shipped in 002) — per
the PM review, neither chart nor identity richness has been validated
against an actual user need yet, only the maintainer's own instinct that
it would help. If that signal never materializes, this item stays
unscheduled; it does not silently expire or get built anyway.

## Scope boundary — must be respected wherever this ends up implemented

- **Machine-identity field allowlist**: hostname, OS name/version,
  architecture, core count — explicitly **not** MAC addresses, hardware
  serial numbers, or `gopsutil`'s persistent cross-boot `HostID`. The
  security review flagged these as raising the stakes of any future
  loopback-binding slip from "leaks a CPU percentage" to "leaks a stable
  device fingerprint," with no diagnostic value to justify it.
- **Charts read from `doctor`'s existing history mechanism** (after the
  follow-up fix above wires it into the dashboard's cache) — no new
  persistence format invented here. Any data gathering for a chart or an
  identity block belongs in `snapshotCache.refresh()`, never inside a
  `Render` function — the performance review was explicit that an
  uncached read/subprocess call inside a render path silently becomes a
  new per-request cost.
- **If this ever grows to include live/push updates** (auto-refresh,
  SSE, WebSocket — not currently asked for, but named in earlier design
  discussion as a possible future direction): it does not ship before
  item 005's Origin/Sec-Fetch-Site CSRF gate is live, and it must reuse
  the existing single-flight `snapshotCache` with a hard connection cap
  and context-aware shutdown per handler — never a fresh `Collect()` per
  connection. `internal/dashboard`'s HTML composition should also have
  already migrated to `html/template`'s auto-escaping by then (tracked
  as an immediate follow-up, not part of this item), since more render
  surface on the current manual-escaping discipline raises the cost of
  a slip.
- Whatever design eventually gets written for this should go through the
  same review-panel process items 001/002 did before implementation
  starts, per `AGENTS.md`'s "Roadmap discipline."

## Plan

[`implementation-plan.md`](implementation-plan.md) — empty until this is
actually triggered and designed; do not start checking off tasks here
without a design doc this item's own header can point to.
