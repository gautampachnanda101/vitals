# AGENTS.md

Working notes for any AI coding agent (or human) making changes in this repo.
Read this before writing code, not after something breaks.

## What this is

`vitals` is a single static Go binary: cross-platform system diagnostics that
correlate resources into a verdict + fix, rather than just drawing gauges.
Four dependencies: `github.com/shirou/gopsutil/v4` (system data),
`github.com/mattn/go-isatty` + `github.com/mattn/go-colorable` +
`golang.org/x/term` (reliable cross-platform terminal color/width
detection, added 2026-09-03 — see "One dependency" below for why the
hand-rolled approach was replaced). No other dependency gets added
without a real reason — see "One dependency" below.

## Build, test, lint

```sh
go build ./...              # compile everything
go vet ./...                # must be clean
go test ./...                # unit tests (fast, no live-system assumptions beyond gopsutil)
go test -race ./...          # same, with the race detector — run this before every push
"$(go env GOPATH)/bin/staticcheck" ./...   # go install honnef.co/go/tools/cmd/staticcheck@latest if missing
gofmt -l .                   # must print nothing
./build.sh ci                # cross-compile every release target (matches the `crosscompile` CI job)
```

Also run, before trusting a green local build:

```sh
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux   GOARCH=amd64 go build ./...
```

Windows and Linux catch real bugs macOS-only development won't (see
"Cross-platform gotchas" below). `staticcheck` catches dead code `go build`
doesn't (an unused top-level func is not a compile error, but CI's `lint` job
fails on it — install and run it locally before pushing, every time).

### The CLI smoke test

`cli_smoke_test.go` (package `main`) execs the actual compiled binary for
every read-only command — `go test ./...` runs it, but *silently*: without
`-v` its 26 subtests produce one aggregate `ok vitals 16s` line with no
visible per-command evidence. To see them:

```sh
go test -v -run TestCLISmoke .
```

CI has a dedicated verbose step for this so the tested-command list is
visible in the job summary, not just buried in a pass/fail. When you add a
new command or a new safe (non-destructive, non-blocking, non-network) flag
combination, add a case here.

## Architecture map

```
main.go              CLI dispatcher — one `case` per subcommand
internal/doctor       correlation engine: Collect (live) / Analyze (pure) / schema
internal/diag         severity / finding / report vocabulary
internal/config       ~/.config/vitals/config.toml — threshold overrides
internal/llm          local + cloud provider probing, offload %, chat completion, `llm fit`
internal/advice       doctor snapshot -> LLM prompt -> printed advice
internal/gpu          nvidia-smi / rocm-smi / ioreg telemetry
internal/clean        cross-platform cache/log/trash cleanup + audit history
internal/dupes        duplicate-file finder (report-only, optional --hardlink)
internal/tools        detect/install/launch companion tools (ncdu, btop, nvtop, ...)
internal/guide        Markdown -> ANSI / -> HTML renderers, shared local-server plumbing
internal/dashboard    `vitals dashboard` — plugin/module registry, capability-gated pages
internal/monitor      `top`
internal/memhogs      `memhogs` (OS-native app-family resolution + families.json)
internal/memcheck     `memcheck`
internal/metrics      Prometheus exporter (OTel semconv names)
internal/mcp          Model Context Protocol server
internal/ui           terminal formatting helpers (color, tables, human-readable units)
internal/help         command docs, contextual help, shell completion
```

## Docs structure

Docs follow Backstage/TechDocs conventions: everything under `docs/` is
built by `mkdocs.yml` (`docs_dir: docs`) and cataloged by `catalog-info.yaml`
at the repo root (a Backstage `Component` entity — this repo doesn't run a
Backstage instance today, but the files are real and would register
correctly if one existed).

- `docs/index.md` — the docs home page.
- `docs/user-guide.md` — **the file `//go:embed`ed into the binary as
  `vitals guide`**. If you move or rename this file, update the
  `//go:embed` directive in `main.go` and the error message in
  `TestUserGuideEmbedded` (`main_test.go`) in the same change — this has
  already bitten one doc reorganization and is easy to miss because the
  build still succeeds (an empty/missing embed only fails at test time,
  via `TestUserGuideEmbedded`'s length check, not at compile time for a
  renamed-but-still-present file).
- `docs/architecture/design.md` — the architecture doc: what's built, why,
  and the outcome of any review-panel run against it (see "Roadmap
  discipline" below).
- `docs/roadmap/` — one item per initiative; see "Roadmap discipline".
- Repo-root files that stay at the root regardless of this convention:
  `README.md` (GitHub renders this as the repo landing page — moving it
  breaks that), `AGENTS.md` (this file — AI coding agents look for it at
  the repo root by convention, unrelated to Backstage/TechDocs), `LICENSE`.
- `plugin/skills/` is a different thing entirely — it's the Claude Code
  *plugin* vitals ships to its own end users (system-diagnostics skill,
  `/vitals-*` commands). `.claude/skills/` (see "Roadmap discipline") is
  for contributors working on vitals itself and never ships to users.

## Roadmap discipline

`docs/roadmap/` holds one directory per roadmap item
(`items/NNN-slug/index.md` + `implementation-plan.md`), grouped into
releases (`releases/vX.Y.Z.md`). This exists because unclear intent
doesn't just slow down a human contributor — handed to an agent, it
produces confidently-wrong output instead of a slower right answer. A
roadmap item's `index.md` is the precise, codebase-grounded spec that has
to exist *before* implementation starts on anything non-trivial; the
`implementation-plan.md` is what turns that spec into checkable tasks.

- **An implementation plan is a living document, not a historical log.**
  It shows what's *left*. Check off or delete a task as it lands. Once
  every task in an item is done, flip that item's `index.md` status to
  Done — don't leave a fully-checked plan sitting as if it were still
  active, and don't let a plan silently drift out of sync with what's
  actually been built. A stale plan is worse than no plan: it tells the
  next contributor (human or agent) something false with total confidence.
- **Significant design changes get reviewed before implementation, not
  after.** "Significant" means: a new package, a new user-facing surface,
  anything touching the network/filesystem/subprocess trust boundaries, or
  anything a reviewer would reasonably want to weigh in on before it's
  hard to unwind. Use the `review-panel` skill (`.claude/skills/`) for
  this — it runs three independent technical-architect reviews plus
  security, product, and QA reviews in parallel against the design doc,
  then synthesizes convergent findings into a must-fix list. This
  substitutes for the automated review-tooling layer a larger team might
  have (we don't have one): agents handle the parallel first-pass review
  at agent speed, the user makes the final call, same split either way.
  The individual persona skills (`review-architect`, `review-security`,
  `review-pm`, `review-qa`) can also be invoked standalone for a narrower
  review.
- **Verification gates before an item is called done**: the same ones
  every change in this repo goes through (`go build`, `go vet`,
  `staticcheck`, `go test -race`, `make coverage`), plus whatever
  item-specific exit criteria its `implementation-plan.md` states. An
  item's exit criteria are part of the spec, not an afterthought — write
  them when the item is created, not when it's finished.
- **Commit locally as each task lands** — see "Committing" below; this is
  the SDLC loop for a roadmap item: pick the next unchecked task in its
  `implementation-plan.md`, TDD it, get the repo's gates green, commit,
  check it off, repeat.
- **Sequencing is part of the spec, not just a task order.** If one item
  has no dependency on another, say so explicitly and let it ship
  independently — don't default to a linear phase order out of habit. A
  roadmap review has already caught this mistake once (a static, zero-
  dependency deliverable was sequenced behind two unrelated items purely
  by default ordering).

## Non-negotiable principles

- **One dependency for data, three for reliable terminal output.**
  gopsutil for system data — that's the whole "one dependency" claim's
  original scope, and it still holds there. Before reaching for a
  library (a config format, a Markdown renderer), check whether the
  actual feature surface used is small enough to hand-write — it
  usually is. `internal/config`'s flat `key = value` format and
  `internal/guide`'s Markdown renderers exist because of this rule, not
  because hand-rolling is inherently better.

  The one deliberate exception (2026-09-03, maintainer decision after
  three rounds of "the CLI's colored output still isn't good enough"):
  `internal/ui` now depends on `github.com/mattn/go-isatty` +
  `github.com/mattn/go-colorable` (reliable TTY detection and legacy
  Windows console ANSI support — the previous hand-rolled
  `os.Stdout.Stat()` mode-bit check never recognized a real terminal on
  Windows at all) and `golang.org/x/term` (real terminal-width
  detection for `ui.Wrap`, replacing a fixed-width guess). These were a
  real, hand-written approach hitting a real ceiling, not a shortcut —
  don't treat this as license to reach for a library the next time
  hand-rolling would work; it's still the default everywhere else.
- **The `--json` schema is a frozen, additive-only contract.** Adding a
  field: bump the *minor* version in `internal/doctor/schema.go`
  (`SchemaVersion`), add it to `schema.json`, then
  `go test ./internal/doctor -run TestSchemaFieldsContract -update` to
  regenerate the golden file. Renaming or removing a field needs a major
  bump and should not happen casually. **Bump the version once per actual
  release, not once per commit within an unreleased working session** — if
  you bump it three times before anything ships, collapse it back to one
  number before committing. (This exact mistake happened once; the fix was
  to check `git log` for what's actually been released before deciding the
  next version number.)
- **`Collect` is live, `Analyze` is pure.** `Collect` touches the OS
  (gopsutil, shelling out, timers). `Analyze` takes a `Snapshot` and returns
  a `diag.Report` with no I/O at all, which is what makes the whole
  correlation engine testable from fixtures. Keep new resource logic on the
  correct side of that line. Where a live signal (history, a DNS check, a
  companion-tool probe) needs to influence a finding but doesn't belong in
  `Collect`, follow the precedent in `doctor.finishAssess` / `focus.go`:
  compute it live at the call site, then either append to the report or
  print it as supplementary context — never inside `Analyze` itself.
- **Complement, don't reimplement.** `internal/tools` exists precisely so
  vitals defers to ncdu/gdu/dust/btop/htop/nvtop/jdupes/smartctl instead of
  rebuilding their specialty. If a feature request looks like "build a
  worse version of an established tool," point at `vitals tools` instead.
- **`docs/user-guide.md` and the public site (`site/index.html`) describe
  current behavior, not behavior as of whenever they were last touched.**
  Any commit that changes a command's availability, output shape, or
  default (a page that used to be conditionally hidden and is now always
  shown, a JSON field renamed, a dependency added, a flag's default
  changed) updates the matching prose in the same commit — not as a
  follow-up once "someone notices." This drifted stale at least three
  times in one session before being made explicit here (advice's
  LLM-availability gating, `vitals llm`'s model-listing behavior, and the
  dashboard's page list, all caught only because the user asked "is this
  accurate?" rather than because the change's own commit updated the
  docs it made false). Before considering a behavior-changing task done,
  grep `docs/user-guide.md` and `site/index.html` for the command/feature
  you touched and re-read what it currently claims — don't assume it's
  still true.

## Testing conventions

- **TDD for new logic**: write the failing test, confirm it fails for the
  right reason, then implement. This repo's test suite was built this way;
  keep doing it rather than writing tests after the fact.
- **95%+ raw coverage of every package is a hard rule, with no live-glue
  exemption.** (Re-confirmed explicitly by the user, 2026-09-04, after
  challenging an earlier version of this rule that scoped the 95% target
  to a package's "pure/testable logic" only — that scoping could not be
  traced to any verbatim record of the user actually asking for it; it
  was an agent's own framing, written into this file and described as
  "confirmed by the user" without a quote to back it up. Given the
  choice — keep the pure-logic scope, or require 95%+ raw coverage of
  everything including what was previously called "live glue" — the
  user chose the latter, explicitly and by name, so that is now the
  rule, full stop.)
  - This means `exec.Command` wrappers, OS/network reads (gopsutil
    calls, `/proc` reads), `--watch`/`signal.NotifyContext` loops, and
    HTTP servers (`guide.Serve`, `metrics.Serve`) are **not** exempt
    anymore — they need to become genuinely testable, typically by
    injecting the live call (a function value, an interface, a small
    "runner" abstraction) so a test can substitute a fake and exercise
    the surrounding logic and the call site itself. `main`'s
    `os.Exit`-driven dispatch is the one thing that cannot be unit
    tested even in principle (the process exits) — everything else is a
    "how do we make this injectable," not a "this can never be tested."
  - **Status (2026-09-04)**: not yet met. A coverage run the same day
    this rule was re-confirmed showed roughly 14 of 19 packages well
    below 95% raw (`internal/memcheck` 33.3%, `internal/monitor` 41.2%,
    `main` 40.9%, `internal/advice` 37.0%, `internal/tools` 44.6%,
    `internal/gpu`/`internal/doctor` ~54-55%, `internal/llm` 62.6%,
    `internal/clean` 67.9%, `internal/dupes`/`internal/mcp` ~68%,
    `internal/guide` 72.0%, `internal/metrics` 75.2%). Tracked as its
    own roadmap item — see `docs/roadmap/items/` for the current one —
    with a per-package task list for what specifically needs to become
    injectable/fakeable. `check_coverage.py`'s floors stay at each
    package's own already-measured number until real work raises it;
    do not bulk-raise a floor to 95% before the tests exist to support
    it, since that would just break every future commit until someone
    does the work — floors ratchet up alongside real coverage gains,
    same discipline as before, just against the new 95%-raw target
    instead of a pure-logic-scoped one.
- **Coverage floors are per package, not blended.** `check_coverage.py`
  (run automatically in CI, `make coverage` locally) enforces a floor per
  package instead of one number over `./...` — a blended floor lets a
  97%-covered package carry a 3%-covered one with no warning, which is
  exactly how the two bugs above got through. Floors only ratchet up: when
  you raise a package's coverage, bump its floor in `check_coverage.py` to
  match (rounded down by one point for safety margin) so it can't quietly
  regress later.
- **`os.UserConfigDir()` isolation in tests is OS-dependent** and this has
  already caused a real CI failure once: `t.Setenv("HOME", dir)` isolates
  macOS and Linux, but Windows reads `%AppData%`, not `$HOME`. Any test that
  touches a real config/history file must set **both**:
  ```go
  dir := t.TempDir()
  t.Setenv("HOME", dir)
  t.Setenv("APPDATA", dir)
  t.Setenv("XDG_CONFIG_HOME", "")
  ```
  (`internal/doctor/run_test.go` has an `isolateConfigDir(t)` helper —
  reuse that pattern rather than re-deriving it.)
- **Verify OS command output empirically before trusting it.** Don't guess
  the shape of `pmset -g`, `nvidia-smi`, DISM output, etc. — run it on a
  real machine (or find independent confirmation) and write the parser
  against real captured output, with tests using that exact text. If you
  can't verify a platform (no Windows box, no NVIDIA GPU), say so explicitly
  and skip the feature rather than shipping a guess — a wrong parsed number
  presented confidently is worse than an absent feature.
- **Verify JSON/jq examples in docs against the actual struct tags**, not
  from memory. `vitals gpu --json` returns `{"devices": [...]}`, not a bare
  array, for instance — an easy wrong guess to make and a real one already
  caught before it shipped in docs/user-guide.md.

## ANSI color + fixed-width tables

`fmt.Sprintf("%8s", ui.Grade(text, ...))` is a bug: `ui.Grade` wraps `text`
in ANSI escape codes, and Go's `%Ns` counts those invisible bytes toward the
width, so a colored 3-character string already "exceeds" width 8 and gets
zero padding. Pad the plain text *first*, then color it — either by hand
(see `internal/monitor/monitor.go`'s `// Pad inside the colour wrap` comment)
or with `ui.GradeWidth(width, text, v, warn, crit)`, which does exactly
that. This bug shipped once in the disk table before being caught from a
live paste of misaligned output — check any new multi-column colored table
against this before considering it done.

## Release process

Tags are consequential: pushing `vX.Y.Z` triggers `.github/workflows/
release.yml`, which builds binaries for all 6 platform/arch targets,
**publishes a real public GitHub Release**, and (if the tap token secret is
configured) pushes a commit to the separate public Homebrew tap repo. Before
tagging:

1. Confirm CI is green on the exact commit you're about to tag —
   `gh run list --branch main --limit 1`, not "green a few commits ago."
2. Confirm every fix/feature you're about to describe as "in this release"
   has actually landed on that commit — `git log <last tag>..HEAD` and read
   it. Tagging one commit too early, before a fix that was made in the same
   session, has already happened once here; it required a same-day patch
   release to correct, since a published tag should never be moved.
3. Semver: additive/backward-compatible → minor bump; a real bug fix to
   already-released behavior → patch bump; a breaking change (schema major
   bump, a renamed/removed flag) → major bump.
4. **Never let `site/index.html` (or anything else public-facing) reference
   a version number before that tag actually exists.** `.github/workflows/
   pages.yml` deploys the live public site on every push to `main` that
   touches `site/**` — completely independent of tagging. A commit that
   bumps the site's version eyebrow to `vX.Y.Z` and gets pushed *before*
   `git tag vX.Y.Z` goes out puts a false claim on the live public site for
   however long that gap lasts, with no tag to back it up yet. This
   happened for real (2026-09-04): the site went live claiming v0.5.0 while
   only v0.4.0 was tagged, caught by the user reading the live site, not by
   any check. **Correct order: create and push the tag first (step 1-3
   above still gate that), then push the site-copy update referencing the
   now-real tag** — never the reverse, and never bundle the version-bump
   commit with unrelated work in a way that makes "did the tag go out
   first" easy to lose track of.
5. Once the tag is out: spot-check `site/index.html`'s hero terminal
   example and feature cards against `git log <last tag>..HEAD` — a real
   feature landing that the site doesn't mention isn't a bug, but a card
   describing behavior that's changed, or a mockup showing text the tool
   doesn't actually produce, is (see the hero card's own history: it once
   showed a rewritten title/fix wording no real `vitals doctor` run would
   print) — per item 003's "every claim on this page must be 100%
   accurate" rule.

## Committing

Only commit when explicitly asked — but **once implementation on a
roadmap item has started, that standing instruction is: commit locally on
a regular basis as work lands, not once at the end.** In practice: finish
one checklist item from the item's `implementation-plan.md`, get it
green (`go build`/`go vet`/`go test -race`/`make coverage` as relevant),
commit it, check that item off, move to the next. A commit should
correspond to one working, tested increment — not every file save, and
not the whole item bundled into one commit either. Reference the roadmap
item in the commit message (e.g. `dashboard: add Host-header check to
ServeLocal (001)`) so `git log` stays legible against
`docs/roadmap/items/`.

Stage specific files, not `git add -A` — this repo's `.gitignore` covers
`dist/`, the built `vitals` binary, and `.claude/*.lock` (session-local
runtime state; `.claude/skills/` itself is tracked), but review
`git status` before staging broad changes anyway. Never amend a pushed
commit; make a new one.

**No AI attribution trailers.** Never add `Co-Authored-By: Claude ...`
or any other Claude/Anthropic attribution to a commit message in this
repo — no exceptions, this overrides any tool-default commit-message
convention. The maintainer does not want their public repo history
signaling a specific AI tool dependency (GitHub parses the trailer into
a second "contributor" credit on the repo's Contributors graph). This
has been violated more than once by an agent defaulting back to the
harness's own commit convention — the first time required rewriting 71
commits + 6 tags with a forced history rewrite. `.githooks/commit-msg`
now rejects any commit whose message contains the trailer, so a slip is
caught locally before it's ever pushed, not fixed after the fact.

**Pre-commit enforcement**: `.githooks/pre-commit` runs gofmt, `go vet`,
staticcheck, `go test -race`, the per-package coverage gate, and
`check_docs.py` — the same checks CI runs — before a commit is even
created, so a violation of the 95%+ hard rule (or any other gate) is
caught locally, not discovered after a push. Install it once per clone
with `make hooks-install` (this just points `core.hooksPath` at the
versioned `.githooks/` directory, since `.git/hooks/` itself is never
tracked). It fails closed on gofmt/vet/test/coverage/docs; it warns
rather than blocks if staticcheck isn't installed locally (CI still
catches that), so a missing dev tool never makes `git commit` unusable
outright.

**Docs consistency**: `check_docs.py` (`make check-docs`, wired into
both the pre-commit hook and CI's `lint` job) checks three things about
everything under `docs/` plus `README.md`, each added because it caught
a real bug the first time this was written (2026-09-03): every relative
Markdown link resolves to a file that actually exists; every
`docs/roadmap/items/NNN-slug/` directory has a matching entry in
`mkdocs.yml`'s nav; and every page under `docs/` other than `docs/
index.md` carries a breadcrumb line back to the docs home (`[docs](...)`
near the top). Adding a new docs page means wiring it into the nav and
adding its breadcrumb, or this fails the build — that's the point, not
an oversight to work around.

**Push regularly — a local commit that never reaches `origin` isn't
protecting anyone.** CI only runs on push; committing on a regular basis
locally (above) and then leaving those commits unpushed for hours or
days means CI is validating a stale HEAD while real work accumulates
untested by the matrix (Windows/macOS builds, the CLI smoke test,
cross-compile) that the local pre-commit hook doesn't cover. Concretely:
after finishing a roadmap checklist item (or a small standalone fix like
a wording change), push it once it's committed and green locally, rather
than batching a long run of commits before the first push — that's what
let 29 commits go unpushed for 12+ hours in this repo and made the next
CI run fail against a stale base rather than the fixes already sitting
in those commits. Still ask before pushing when the user hasn't already
signaled they want that (per the executing-actions-with-care norm), but
don't let "only commit when asked" quietly turn into "never push either."
