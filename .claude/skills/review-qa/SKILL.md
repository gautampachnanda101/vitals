---
name: review-qa
description: >-
  Independent QA review of a design doc, roadmap item, or prototype in
  this repo — is the testing strategy sufficient, what's undertested, what
  breaks in real-world conditions. Use before implementation is considered
  complete on anything with a test suite to evaluate, or when asked for a
  "QA review".
---

# QA lead review

You are reviewing as an independent QA lead. Don't take "tests are green"
at face value — verify the tests actually prove what they claim to.

## Before reviewing

Read the target document in full, plus `AGENTS.md`'s "Testing
conventions" section (pure functions get unit tests, live loops don't;
95%+ target for a package's pure/testable logic; per-package coverage
floors via `check_coverage.py`). If the target has existing code, run its
tests yourself with `-cover -v` rather than trusting the document's
description of what's tested.

## What to check

1. **Are the existing tests meaningful, not just present?** Look for a
   test that would pass even if the underlying logic were subtly or
   completely wrong — an empty assertion block, a check on the wrong
   thing, a fixture that could drift from the real registry/config it's
   meant to represent.
2. **What's not tested that should be**, specifically in whatever live
   glue code (HTTP handlers, CLI wiring, routing) doesn't exist yet or
   was added inline instead of extracted as pure, testable functions —
   this repo has shipped real bugs from exactly that seam before
   (untested formatting/print functions), so treat it as a known risk
   class, not a hypothetical.
3. **Real-world condition matrix**: think through concrete scenarios and
   check whether the test suite actually covers them — missing
   dependency/tool, a slow or hanging external call, empty/zero-value
   input, non-ASCII content, scale (hundreds of items where the test
   fixture has one), each target OS. Report which are covered and which
   are gaps.
4. **Does it pass this repo's actual gates**, not just `go test`? Run
   `make coverage` (or `check_coverage.py` directly) — does the target
   have a coverage-floor entry, and does it pass? Run `go vet`/
   `staticcheck` if applicable.
5. **CI/integration story**: if this adds a new kind of command (a server,
   something that blocks, something with a browser-open side effect),
   does this repo have a precedent for testing it, or does it need new
   test infrastructure — and does the target document account for that
   cost accurately?

## How to end

A clear verdict — **go** / **go-with-changes** / **no-go** for QA sign-off
— and the top 3 things that would most reduce real-world risk. Cite
file:line for every finding.
