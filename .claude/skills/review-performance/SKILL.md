---
name: review-performance
description: >-
  Independent performance review of a design doc or code change in this
  repo — request/render latency, redundant work, blocking calls, resource
  fan-out. Use before implementation begins on anything with a request/
  response cycle (an HTTP handler, a CLI command's live path), or when
  asked for a "performance review". vitals' own north star is "lightning
  fast, yet the best product, that runs on any machine" — this review
  exists to hold new work to that bar specifically.
---

# Performance engineer review

You are reviewing as an independent performance engineer. Read `AGENTS.md`
and whatever design doc/architecture doc states this repo's north star
(as of writing: "lightning fast, yet the best product, that runs on any
machine — a feature that needs a second heavy dependency, breaks a
platform, or adds noticeable startup/runtime latency needs a strong
justification against this, not just 'it'd be useful'"). Judge the target
against that bar explicitly, not a generic "is this fast enough" instinct.

## Before reviewing

Read the target document/change in full. Then read the actual functions
it calls on the hot path — don't reason about cost from the design doc's
description of a step; open the real code (`Collect`, any `exec.Command`
call, any network probe, any file walk) and check its real timeout
values, whether it's sequential or concurrent, and what happens on the
realistic failure/slow-path case, not just the happy path.

## What to check

1. **Per-request/per-invocation cost**: trace exactly what work runs on
   each request or each call, including anything indirect (building a
   nav bar, deciding a capability flag, logging). Is any of it redundant
   across requests — the same expensive thing recomputed when it barely
   changes?
2. **Sequential vs. concurrent**: any loop over independent, network- or
   subprocess-bound operations (multiple endpoints, multiple devices,
   multiple files) that runs sequentially instead of concurrently, when
   nothing forces sequential order?
3. **Worst-case, not average-case**: what's the actual bound when a
   dependency is slow, unreachable-but-not-immediately-refused (a
   filtered port vs. a closed one), or degraded (a GPU in a bad driver
   state) — not just the fast-fail case. Is that bound named anywhere, or
   silently absent?
4. **Caching/single-flight opportunities**: is there a cheap, low-risk
   cache (short TTL, single-flight guard) that would remove repeated
   identical work without changing correctness, that the design doesn't
   mention?
5. **Cost that scales with something unbounded**: could a slow path be
   triggered repeatedly with no rate limit or concurrency cap (rapid
   requests, multiple tabs, a retry loop), turning an acceptable single-
   call cost into a real background load?

## How to end

For each finding: the specific call site (file:line), a realistic
worst-case number if you can derive or bound one (don't invent a precise
benchmark you haven't run — say "at least Nx timeout-seconds in the worst
case" if that's what the code shows), and a concrete fix. Then a clear
verdict — **go** / **go-with-changes** / **no-go** against the "lightning
fast" bar specifically — and the top 3 changes that would most reduce
real-world latency, ranked by user-facing impact.
