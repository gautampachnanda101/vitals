# AGENTS.md

Working notes for any AI coding agent (or human) making changes in this repo.
Read this before writing code, not after something breaks.

## What this is

`vitals` is a single static Go binary: cross-platform system diagnostics that
correlate resources into a verdict + fix, rather than just drawing gauges.
One runtime dependency: `github.com/shirou/gopsutil/v4`. No other dependency
gets added without a real reason — see "Don't add a dependency" below.

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
internal/guide        Markdown -> ANSI / -> HTML renderers for `vitals guide`
internal/monitor      `top`
internal/memhogs      `memhogs` (OS-native app-family resolution + families.json)
internal/memcheck     `memcheck`
internal/metrics      Prometheus exporter (OTel semconv names)
internal/mcp          Model Context Protocol server
internal/ui           terminal formatting helpers (color, tables, human-readable units)
internal/help         command docs, contextual help, shell completion
```

## Non-negotiable principles

- **One dependency.** gopsutil, and that's it. Before reaching for a library
  (a config format, a Markdown renderer, a color/terminal package), check
  whether the actual feature surface used is small enough to hand-write —
  it usually is. `internal/config`'s flat `key = value` format and
  `internal/guide`'s Markdown renderers exist because of this rule, not
  because hand-rolling is inherently better.
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

## Testing conventions

- **TDD for new logic**: write the failing test, confirm it fails for the
  right reason, then implement. This repo's test suite was built this way;
  keep doing it rather than writing tests after the fact.
- **Pure functions get unit tests. Live loops don't** — `--watch` loops,
  `exec.Command` wrappers, and HTTP servers (`guide.Serve`, `metrics.Serve`)
  are thin and untested directly; the logic they call (parsers, formatters,
  request builders) is tested instead. Don't try to unit-test a `signal.
  NotifyContext` loop; extract the interesting part and test that.
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
  caught before it shipped in USERGUIDE.md.

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

## Committing

Only commit when explicitly asked. Stage specific files, not `git add -A` —
this repo's `.gitignore` covers `dist/`, the built `vitals` binary, and
`.claude/`, but review `git status` before staging broad changes anyway.
Never amend a pushed commit; make a new one.
