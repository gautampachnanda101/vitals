# 008 — `vitals heal`

[docs](../../../index.md) / [Roadmap](../../index.md) / **008 — `vitals heal`**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Not started — captured as a followup, not yet designed
**Depends on**: [diag](../../../architecture/design.md) findings/fixes model (existing); logically follows `doctor`/`advice`
**Target release**: not yet
**Architecture**: not written yet — needs its own design doc and review
panel before implementation starts, per `AGENTS.md`'s "Roadmap
discipline"

## What

`vitals doctor` diagnoses (rule-based findings), `vitals advice` explains
(heuristic findings plus, when reachable, LLM commentary on top — the
heuristic half is the immediate, always-available answer; the LLM
complements it, never replaces it). Neither one *acts*. `vitals heal`
would be the third step: apply a finding's own
suggested fix — the same `Fixes []string` a `diag.Finding` already
carries and `doctor`/`advice` already print — with confirmation, instead
of leaving the user to copy a command like `kill 40547` out of the
terminal by hand.

Raised by the maintainer while reviewing the advice heuristic-fallback
work (2026-09-03): once findings have real, well-established fixes
attached (quit a process, clear a cache, reboot-suggested), the next
obvious gap is that nothing in vitals can take that action itself.

## Why this needs a design doc and review panel before any code

Every other command in vitals is read-only by construction (`doctor`,
`advice`, the resource-focus commands) or gated behind an explicit,
narrow write action reviewed on its own merits (`clean`, and the
dashboard write-action work in [005](../005-dashboard-write-actions/)).
`heal` would be the first command whose entire purpose is executing
another program's suggested remediation — `kill <pid>`, quitting an
application, clearing a specific cache directory — which is a materially
bigger blast radius than `clean`'s already-reviewed, narrowly-scoped
disk reclamation. Real open questions a review needs to settle, not an
exhaustive list:

- Which `Fixes` strings are even safe to execute automatically at all —
  today they're free-text written for a human to read and run, not a
  structured, machine-executable action. Turning them into one probably
  means `diag.Finding` grows a separate, structured `Remedy` type per
  finding kind, not a regex over the existing prose.
- Confirmation model: per-fix, per-finding, or a single `--yes` for a
  batch — and whether `heal` should ever run non-interactively at all
  given a `kill` on the wrong pid or an app quit while unsaved work is
  open is a real, immediate loss for the user, not a reversible one like
  most of what `clean --dry-run` protects against today.
- Overlap with `clean`: swap/disk-pressure fixes already have a
  dedicated, reviewed command — `heal` should not become a second,
  looser path to the same destructive operations `clean` already gates
  carefully.

## Plan

[`implementation-plan.md`](implementation-plan.md) — empty until this is
designed and reviewed; do not start checking off tasks here without a
design doc this item's own header can point to.
