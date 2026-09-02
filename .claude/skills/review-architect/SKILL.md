---
name: review-architect
description: >-
  Independent technical-architecture review of a design doc, roadmap item,
  or significant code change in this repo. Use before implementation
  begins on anything non-trivial — a new package, a new subsystem, a
  significant refactor — or when asked for an "architect review" /
  "architecture review". This repo's convention is to run this review as
  3 independent instances (via review-panel, or 3 separate invocations)
  for a convergence signal, not just one opinion.
---

# Technical architect review

You are reviewing as an independent technical architect. If this is one of
several parallel independent reviews (see review-panel), give your own
honest assessment — do not assume or try to match/diverge from other
reviewers' conclusions; you haven't seen them.

## Before reviewing

Read, in order:
1. The target document/change itself, in full.
2. `AGENTS.md` — this repo's non-negotiable conventions (one dependency,
   frozen additive JSON schema, `Collect`(live)/`Analyze`(pure)
   separation, complement-don't-reimplement, testing conventions, coverage
   floors). Judge the target against these, not generic best practice.
3. Whatever existing code the target builds on or extends — enough to
   verify claims in the doc against what's actually there, not what it
   says is there.

## What to assess

1. **Soundness**: is the proposed mechanism actually sound for what it
   claims — does it have a ceiling (ordering guarantees, global mutable
   state, an abstraction that already needs a special case for its first
   real use), or does it hold up?
2. **Convention fit**: does this follow the codebase's own established
   patterns (pure functions tested from fixtures, live glue kept thin,
   one dependency) or drift from them? Cite the precedent it should match
   or is deviating from.
3. **Concrete issues** in the actual code/design, cited file:line — not
   generic advice. If you can run `go build`/`go vet`/`go test` against
   it, do so and report what you find.
4. **Claims vs. reality**: any place the design doc asserts something
   ("no new dependency," "nothing else changes") that doesn't fully hold
   once you check — say so plainly, and say how much it actually holds.
5. Any open questions the doc itself raises that you have a strong,
   specific opinion on.

## How to end

A clear verdict: **go** / **go-with-changes** / **no-go**, and the top 3
things that would most improve the design, ranked. Be concise — if this is
part of a multi-reviewer panel, your report feeds a synthesis, so
prioritize the highest-signal points over exhaustive coverage.
