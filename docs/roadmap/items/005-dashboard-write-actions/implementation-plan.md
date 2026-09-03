# Implementation plan — 005 Dashboard write actions

[docs](../../../index.md) / [Roadmap](../../index.md) / [005 — Dashboard write actions](index.md) / **Implementation plan**

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

**Hard gate**: no task below starts until item 001's Host-header fix has
been in `main` for at least one full CI cycle.

## Tasks

- [ ] Design the CSRF/same-origin model as its own short design note
      (Origin header check, and/or a per-session token minted on first
      page load and required on every mutating request) — get this
      reviewed on its own (per `AGENTS.md`'s "Roadmap discipline" —
      significant/security-relevant design changes get a persona-based
      agent review before implementation) before any POST route exists.
- [ ] Wire `clean --dry-run` (read-only preview, no filesystem mutation)
      to a dashboard button first.
- [ ] Only after the above ships and is reviewed: an actual apply/confirm
      flow for `clean`, matching the CLI's own interactive-confirmation
      safety default in spirit.
- [ ] `dupes`/`--hardlink` exposure, if still wanted at this point,
      follows the same pattern.

## Exit criteria

A security-focused review (at minimum a dedicated security-architect-
persona agent pass, per `AGENTS.md`) signs off on the CSRF/auth model
specifically, before any write route ships.
