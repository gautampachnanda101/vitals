---
name: review-panel
description: >-
  Full multi-persona design review before committing to phased
  implementation — 3 independent technical architects, 1 security
  architect, 1 product manager, 1 QA lead, run in parallel against the
  same design doc, then synthesized into one must-fix list and verdict.
  Use this before starting a new roadmap item, or whenever asked for a
  "full review" / "review panel" / "6-agent review" of a design.
---

# Full review panel

This is the review gate this repo's roadmap items go through before
implementation starts (see `AGENTS.md`'s "Roadmap discipline" and
`docs/architecture/design.md` §7 for where this convention came from and
a worked example of its output). We don't have a dedicated automated
review-tooling layer, so this substitutes the user's own judgment plus
Claude Code agents running each persona, independently, against the same
material.

## Steps

1. **Confirm the target.** A design doc (e.g. a roadmap item's design
   section, or a new `docs/roadmap/items/NNN-slug/` write-up) must exist
   and be readable before this runs — if asked to review something that's
   only been discussed in conversation, write it down first (see the
   "Roadmap discipline" section of `AGENTS.md` on spec-before-code).

2. **Spawn six independent agents in parallel**, in one batch of Agent
   tool calls (not sequential), each `general-purpose`, each told to read
   the target document plus whatever code/context it references. For each
   agent's prompt, use the corresponding skill file's content in this
   directory's siblings as the checklist to embed, adapted to name the
   actual target document and any actual code paths involved:
   - Three agents using `review-architect`'s checklist — explicitly told
     they are independent parallel reviewers who haven't seen each
     other's output, and should not try to converge or diverge on
     purpose.
   - One agent using `review-security`'s checklist.
   - One agent using `review-pm`'s checklist.
   - One agent using `review-qa`'s checklist.

   Each prompt must be self-contained (the agent has no memory of this
   conversation) — include exact file paths to read, enough background
   for the agent to make judgment calls, and the same "end with a clear
   verdict + top 3" instruction from the skill file.

3. **Wait for all six** — do not synthesize or report partial results as
   if they were final. Per-agent completions arrive as separate
   notifications; a brief one-line acknowledgment as each lands is fine,
   but the actual synthesis waits for all six.

4. **Synthesize**, don't just concatenate:
   - Lead with what's **unanimous or convergent** (the same finding
     surfaced by 2+ independent reviewers) — this is the highest-
     confidence signal a panel like this produces, and worth calling out
     as such.
   - Then unique findings each reviewer caught that the others didn't.
   - An overall verdict: if any reviewer said no-go, treat that as a
     blocker requiring explicit discussion, not a vote to average out.
     Six independent go-with-changes with a convergent must-fix list is
     the expected healthy outcome, not a rubber stamp.
   - Turn the fixes into a concrete task list (this is what an item's
     `implementation-plan.md` should absorb — see "Roadmap discipline" in
     `AGENTS.md`), not just a list of concerns.

5. **Do not proceed to implementation on your own judgment** once the
   synthesis is done — present it and let the user decide whether to
   proceed, adjust scope, or request another round. This mirrors the
   "agents execute, verification runs at agent speed, humans handle
   judgment" split this convention is built on.
