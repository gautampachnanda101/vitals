# 004 — Native double-click launcher

**Status**: Not started
**Depends on**: [002](../002-dashboard-mvp/)
**Target release**: v0.6.0
**Architecture**: [design doc](../../architecture/design.md) §7

## What

A thin, OS-native wrapper per platform that just execs `vitals dashboard`
— nothing else. No GUI toolkit, no rewrite; the dashboard itself is the
whole product, this item is only about *reaching* it without a terminal.

## Why

The product-manager review that shaped this roadmap found that items
001–003 don't close the original non-technical-persona gap: running
`vitals dashboard` from a terminal is still a terminal step. This item is
the one that actually does. It's scheduled ahead of item 005
(write actions) on purpose — write actions mainly serve personas already
satisfied by the CLI; this serves the persona the whole redesign started
from.

## Plan

[`implementation-plan.md`](implementation-plan.md)
