# 005 — Dashboard write actions

[docs](../../../index.md) / [Roadmap](../../index.md) / **005 — Dashboard write actions**

**Implementation plan**: [what's left →](implementation-plan.md)
**Design**: [same-origin/CSRF model + WriteAction shape →](design.md) —
reviewed (seven-persona panel, go-with-changes) and implemented, see
its own "As built" section for what shipped and what didn't

**Status**: In progress — `sameOriginOnly`, the `WriteAction` registry,
and a full `clean` preview flow (`POST /clean/preview` +
a `/clean` nav page with a Preview button, the dashboard's first
client-side JS) are done and in `main`, verified end to end against a
live dashboard. Still open: a single-flight guard, `/clean/apply`, and
everything after it in `implementation-plan.md`
**Depends on**: [001](../001-dashboard-foundation/), [002](../002-dashboard-mvp/), [004](../004-native-launcher/)
**Target release**: v0.7.0+
**Architecture**: [design doc](../../../architecture/design.md) §6.5, §6.7; this item's own [design.md](design.md)

## What

Mutating actions triggerable from the dashboard — starting with a
`clean --dry-run` preview, later an actual apply/confirm flow.

## Why

Deliberately last. Every reviewer who touched the trust-model question
(three architects and the security architect, independently) agreed:
loopback-only-no-auth is a defensible read-only posture but becomes a real
CSRF/DNS-rebinding risk the moment a state-changing endpoint exists. This
item does not start until item 001's Host-header fix has been live for a
full CI cycle, and the same-origin/CSRF model is designed and reviewed on
its own — not improvised alongside the first button.

## Plan

[`implementation-plan.md`](implementation-plan.md)
