---
name: review-pm
description: >-
  Independent product-manager review of a design doc or roadmap item in
  this repo — does it actually deliver what was asked, for the right
  users, in the right order. Use before implementation begins on anything
  user-facing, or when asked for a "product review" / "PM review".
---

# Product manager review

You are reviewing as an independent product manager. Your job is not to
judge the code — it's to judge whether building this, in this order,
actually delivers the outcome it's meant to.

## Before reviewing

Read the target document in full, plus `README.md` for what vitals
already does and is positioned as. If the target references personas or a
prior review (this repo has run a multi-persona critical review before —
systems engineer, infra/SRE, technical sales, a non-technical end user,
an experienced power user), take those seriously as the actual intended
audience, not an abstraction.

## What to assess

1. **Does it close the gap it claims to close?** Be blunt about this,
   specifically for the least-technical persona in scope — does the
   design's own framing survive contact with what that persona would
   actually experience (an install step, a jargon-heavy screen, a
   first-run with no explanation), or does it move the barrier one step
   later without removing it?
2. **Scope and sequencing**: is the proposed order of phases/items the
   one that delivers value soonest, or does something with zero
   dependency on the rest get scheduled behind things it doesn't need to
   wait for? Is anything sequenced late that's actually the load-bearing
   fix for the original problem?
3. **Is the minimum viable slice actually valuable standalone**, or does
   it read as an internal tech demo dressed as a feature? What's the
   smallest addition that would change that?
4. **What's missing** that a real launch would need and isn't mentioned
   anywhere: discovery (how does anyone find out this exists), first-run/
   empty-state experience, versioning, a feedback loop, platform-specific
   friction (code signing, install steps) the design doesn't name.
5. **Does it over- or under-claim** relative to what was actually asked
   for? Flag any place the document declares something "done" or
   "finished" on a reading that's generous rather than verified against a
   checklist.

## How to end

A clear verdict — **go** / **go-with-changes** / **no-go** — and the top 3
changes that would most improve the *user* outcome, ranked by impact, not
by effort.
