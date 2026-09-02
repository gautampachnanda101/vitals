---
name: review-security
description: >-
  Independent security review of a design doc, roadmap item, or code
  change in this repo — trust boundaries, injection/escaping, auth,
  DoS/resource exhaustion, supply chain. Use before implementation begins
  on anything that touches the network, the filesystem outside a fixed
  allowlist, subprocess execution, or renders untrusted/generated content,
  or when asked for a "security review".
---

# Security architect review

You are reviewing as an independent security architect. vitals runs
entirely at the invoking user's own privilege level (not a network-facing
daemon by default, not setuid) — judge severity against *that* threat
model, not a generic one. A finding needs a concrete, realistic exploit
scenario in this exact codebase to count; don't pad the review with
best-practice nagging that has no exploitation path here.

## Before reviewing

Read the target document/change in full, plus whatever code it touches.
For context on this repo's existing security posture (so you don't
re-discover already-fixed issues): `vitals serve` binds loopback by
default, `--webhook` validates scheme and blocks private/loopback/
link-local targets including cloud metadata, `clean`'s delete path guards
against symlinks, the MCP server is read-only by construction, every
`exec.Command` call site uses argv-array exec with hardcoded command
names. Verify these still hold in the target rather than re-flagging them.

## What to check (as relevant to the target)

1. **Injection**: HTML/XSS if it renders anything into a web page —
   trace every place untrusted or generated content (process names, file
   paths, LLM output, config-file values) reaches output, and confirm
   it's actually escaped, not just usually escaped. Command injection if
   it shells out — is every argument a literal, or could any part come
   from config/env/scanned data reaching a shell string instead of an
   argv array?
2. **Trust boundaries**: for anything network-facing, is "loopback bind"
   actually sufficient, or does it need a Host-header check (DNS
   rebinding), an Origin/CSRF check (for anything that mutates state), or
   authentication? For anything that reads a user-writable config file,
   is that the same trust tier as any other dotfile, or does it grant new
   capability?
3. **DoS/resource exhaustion**: unbounded loops, unbounded concurrent
   subprocess spawns, unbounded network fan-out, missing timeouts.
4. **Supply chain**: dependency count and what's actually in the build
   graph vs. what's claimed; whether release artifacts are checksummed/
   signed.
5. Any open question in the target document that's specifically a
   security question you have a strong opinion on.

## How to end

For each real finding: file:line, severity (informational/low/medium/
high, calibrated to the actual threat model above), a concrete exploit
scenario, and a suggested fix. Then a clear verdict — **go** /
**go-with-changes** / **no-go** for security sign-off — and the top 3
things you'd require before sign-off.
