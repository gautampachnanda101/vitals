# Security Policy

## Supported Versions

`vitals` ships as a single rolling `main` branch with tagged releases
(`v0.1.0`, `v0.2.0`, ...). Only the latest tagged release is supported —
please upgrade before reporting an issue if you're on an older one.

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for a security
vulnerability. Instead, use GitHub's private reporting flow:

**[Report a vulnerability](https://github.com/gautampachnanda101/vitals/security/advisories/new)**
(Security tab → "Report a vulnerability")

This opens a private advisory visible only to you and the maintainers —
nothing is disclosed publicly until a fix is ready.

Please include:

- The version (`vitals version`) and OS you're running
- Steps to reproduce, or a minimal example
- The impact you believe it has

You should get an initial response within a few days. Once a fix is
ready, a new patch release is tagged and the advisory is published with
credit to the reporter (unless you'd prefer to stay anonymous).

## Scope

`vitals` is a local diagnostic CLI that reads system metrics
(CPU/memory/disk/network/GPU/power) and optionally talks to a local or
cloud LLM endpoint the user configures. Relevant categories:

- **Local privilege/data exposure**: anything that reads more than the
  documented metrics, or writes/deletes outside what `clean`/`dupes`
  document and confirm.
- **Network egress**: `vitals serve` binds loopback only by design;
  `--webhook` refuses plain HTTP and loopback/private/link-local targets
  unless `--webhook-allow-insecure` is passed explicitly. A report that
  either of these can be bypassed is in scope.
- **Supply chain**: the release workflow, `install.sh`, and the
  Homebrew/Scoop packaging manifests.

Out of scope: vulnerabilities in a local or cloud LLM runtime `vitals`
merely calls out to (Ollama, OpenAI, etc.) — report those upstream.
