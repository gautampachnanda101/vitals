# Design — 008 `vitals heal`

[docs](../../../index.md) / [Roadmap](../../index.md) / [008 — `vitals heal`](index.md) / **Design**

**Status: draft, pre-review.** This is the design doc `AGENTS.md`'s
"Roadmap discipline" requires before any code — `heal` is a new
user-facing command whose entire purpose is executing remediation, which
crosses the subprocess/signal trust boundary, so it gets a full
`review-panel` pass (three technical architects, security, product, QA,
performance) before implementation, per this item's own `index.md`
"Why this needs a design doc and review panel" section. The sections
below are the proposal as submitted for that review; an "As built"
section will be appended after implementation, mirroring
[005's design.md](../005-dashboard-write-actions/design.md).

## 1. What `heal` is, and what it is not

`vitals doctor` diagnoses (rule-based `diag.Finding`s). `vitals advice`
explains (heuristic findings, plus LLM commentary when reachable).
Neither acts. `heal` is the third step: take a finding that carries a
**machine-executable remedy** and run it, after an explicit
confirmation, instead of leaving the user to copy `kill 40547` out of
the terminal by hand.

`heal` is **not**:

- a general "fix my computer" button — it only ever runs remedies that a
  finding explicitly attaches, from a fixed, hand-written set (§3);
- a second path to `clean`'s disk reclamation — disk/swap-pressure
  findings delegate to `vitals clean` unchanged (§6), they do not get a
  looser re-implementation here;
- something `doctor` or `advice` ever invoke automatically — `heal` is
  only ever run by a human typing `vitals heal` (§5);
- a parser of the existing free-text `Fixes []string` — those stay
  exactly as they are, for display. `heal` reads a new structured field
  (§2), populated only for the subset of findings where an automatable
  action is genuinely safe.

## 2. `diag.Finding` gains an optional structured `Remedy`

Today a `diag.Finding` carries `Fixes []string` — free-text written for
a human to read (`internal/doctor/analyze.go`: `"add RAM or swap
headroom"`, `"improve airflow / clear dust"`, `quitFix(m.TopProc)` which
produces a whole sentence, not a command). None of it is
machine-executable without a regex over prose, which this design
rejects outright.

Instead, `diag.Finding` grows one optional field:

```go
// Remedy is a finding's machine-executable fix: what heal would run,
// and enough metadata for heal to decide whether it may run it without
// an interactive confirmation. nil (the common case) means this finding
// has no automatable action — its Fixes strings are advisory only.
type Remedy struct {
	Kind       RemedyKind `json:"kind"`
	Label      string     `json:"label"`             // human summary, e.g. "Send SIGTERM to Slack (pid 40547)"
	Argv       []string   `json:"argv,omitempty"`    // exact command; empty for RemedySignal (uses Signal/PID) and RemedyManual
	Signal     string     `json:"signal,omitempty"`  // "SIGTERM" etc., RemedySignal only
	PID        int32      `json:"pid,omitempty"`      // target, RemedySignal only
	ProcName   string     `json:"proc_name,omitempty"`// expected process name at apply time, RemedySignal only (race guard, §7)
	Reversible bool       `json:"reversible"`         // can the prior state be restored after this runs
	Risk       RemedyRisk `json:"risk"`              // Low | Medium | High
}

type RemedyKind int
const (
	RemedyManual   RemedyKind = iota // no automatable action; Fixes are advice only
	RemedySignal                     // send Signal to PID (process the user owns)
	RemedyExec                       // run Argv verbatim (a fixed, allowlisted command)
	RemedyDelegate                   // run another vitals subcommand (Argv[0] == "vitals")
)

type RemedyRisk int
const (
	RiskLow    RemedyRisk = iota // reversible or trivially so; losing it costs nothing
	RiskMedium                   // reversible with effort, or a transient service restart
	RiskHigh                     // irreversible, or a running app with possibly-unsaved state
)
```

`Remedy` is only ever constructed by hand-written builders in
`internal/doctor` next to the finding that owns it (§3) — never derived
from a string, never accepted from outside the process. `heal` treats
`Argv`/`Signal`+`PID` as the *only* things it will act on; `Label` and
the risk metadata are advisory to the confirmation UI.

### 2.1 This is an additive `--json` schema change

`diag.Finding` is serialized in the frozen `--json` envelope
(`internal/doctor/schema.go`, `SchemaVersion`, `schema.json`,
`TestSchemaFieldsContract`). Adding `remedy` is **additive** → a minor
`SchemaVersion` bump, `schema.json` updated, golden regenerated
(`go test ./internal/doctor -run TestSchemaFieldsContract -update`), one
bump for the release that ships `heal`, not one per commit — per
`AGENTS.md`'s "One dependency … frozen, additive-only contract" rule.
`Remedy` and its enums need `MarshalJSON`/`UnmarshalJSON` that emit
stable lowercase words (`"signal"`, `"exec"`, `"manual"`, `"delegate"`;
`"low"`/`"medium"`/`"high"`), matching how `diag.Severity` already does
it, so a saved report round-trips.

## 3. The remedy set for the first release (deliberately tiny)

Only findings where the action is unambiguous, well-established, and
low-blast-radius get a non-`nil` `Remedy` in v1. Everything else stays
`RemedyManual` — `heal` prints the `Fixes` text and does nothing.

| Finding (analyze.go) | Remedy in v1 | Kind | Risk | Reversible |
|---|---|---|---|---|
| Memory pressure / swap thrash, `TopProc.Name != ""` | `SIGTERM` to `TopProc.PID` | `RemedySignal` | High | no (running app) |
| macOS reclaimable-memory ("no action needed unless apps slow down" path, when the user explicitly asks) | `sudo purge` | `RemedyExec` | Low | yes (cache refills) |
| Disk low / inodes low | `vitals clean --dry-run` then, on confirm, `vitals clean` | `RemedyDelegate` | Medium | no (deletes files, but only what `clean` already gates) |
| Everything else (thermal, CPU saturation, GPU VRAM, network, battery health, DNS) | — | `RemedyManual` | — | — |

Rationale for starting this small: a `kill` on the wrong pid or an app
quit with unsaved work open is an immediate, non-reversible loss for the
user — unlike almost everything `clean --dry-run` protects. Shipping two
signal/exec remedies plus a delegate, all individually reviewed, is a
blast radius a panel can actually reason about. New remedies are added
one finding at a time in later work, each with its own row in this
table and its own test, never freehanded.

## 4. Confirmation model

Default (interactive TTY): **per-remedy** y/N prompt, showing
`Remedy.Label`, the exact `Argv`/signal+pid, `Risk`, and whether it is
`Reversible`. Same shape as `internal/clean`'s `confirm()` and
`internal/dupes`' `confirmHardlink()` — reuse that pattern, do not
re-derive it.

Flags:

- `--yes` / `-y`: skip the prompt, but **only** for remedies that are
  `Reversible && Risk == RiskLow`. A `RiskHigh` or irreversible remedy
  still prompts even under `--yes` (or is skipped entirely in the
  non-interactive case below). This is stricter than `clean`'s `--yes`
  on purpose: `clean` under `--yes` only ever deletes regenerable
  caches; `heal` under `--yes` could otherwise `SIGTERM` a running
  editor.
- `--only <finding-id>`: apply the remedy for one finding, not all.
  Findings need a stable short id for this (see §8).
- `--dry-run`: print what each remedy *would* do (the resolved `Argv`,
  the target pid and its current name) and exit 0 without running
  anything. `RemedyDelegate` to `vitals clean` maps its own dry-run
  through.

No TTY and no `--yes`: `heal` refuses, exit non-zero, message points at
`--yes` and its restriction. No TTY *with* `--yes`: runs only the
`Reversible && RiskLow` subset, skips the rest with a printed note
naming each skipped finding. `heal` never blocks on a prompt in a
context that can't answer one.

## 5. `heal` is never automatic

`doctor` and `advice` do not call `heal` and do not suggest a bare
`vitals heal` with `--yes`. Their output may mention that `vitals heal`
exists for a finding that has a non-`nil` `Remedy` ("run `vitals heal
--only mem-pressure` to act on this"), but the human runs it. This
mirrors how `advice` complements rather than replaces `doctor` — `heal`
complements both, it is not a step they trigger.

## 6. Overlap with `clean` — delegate, never re-implement

The disk-pressure findings' remedy is `RemedyDelegate` with `Argv =
["vitals", "clean", "--dry-run"]`. `heal`:

1. runs `vitals clean --dry-run` as a subprocess, shows its output;
2. prompts (per §4);
3. on confirm, runs `vitals clean` (no `--dry-run`), which applies its
   own `sameOriginOnly`-independent CLI confirmation
   (`internal/clean`'s `confirm()`), its own single-pass accounting, and
   its own audit history (`clean_history.jsonl`).

`heal` adds no deletion logic of its own and gets `clean`'s existing
review coverage for free. If a future remedy wants a narrower slice of
`clean` (one category), that is a new `clean` flag reviewed on `clean`'s
terms, not a bypass built into `heal`.

## 7. Trust boundary and the new risk this introduces

Inherited, not re-litigated: vitals runs at the invoking user's
privilege. `heal` shells out / signals as that user — it cannot do
anything the user could not do at their own shell. `sudo purge` prompts
for the user's own password through `sudo`'s own tty handling; `heal`
does not capture or store it.

**The genuinely new risk is a stale target.** `doctor` collects a
snapshot; the user reads it; some seconds later `heal` runs. Between
those, `TopProc.PID` may have exited and the pid been recycled onto an
unrelated process. Signalling it would then hit the wrong process.

Mitigation, mandatory in the design:

- `heal` **re-runs `doctor.Assess()` itself** immediately before
  applying anything — it does not act on a snapshot piped in from a
  previous `doctor` run. (`--only` selects from the fresh report.)
- For `RemedySignal`, before sending the signal `heal` re-reads the
  target pid's current process name (gopsutil `Process.Name()`) and
  compares it to `Remedy.ProcName` captured in the same fresh Assess. A
  mismatch aborts that remedy with a message ("pid 40547 is now
  `postgres`, not `Slack` — skipping") — it does not signal.
- Even so, a TOCTOU window remains between the name check and the
  `kill(2)`. It is single-digit milliseconds and the fallback is
  "signalled a process the user owns with SIGTERM" — the same category
  of outcome as the user mistyping a pid themselves. Documented, not
  engineered away; `SIGKILL` is never used by `heal`.

Out of scope, same as everywhere else in this repo: another same-user
process on the machine calling things directly. `heal` defends against
its own staleness, not against the user's own machine.

## 8. Stable finding ids

`--only` and any "run `vitals heal --only X`" hint need a short stable
id per finding kind. `diag.Finding` today has no id. Add
`ID string json:"id,omitempty"` — a kebab-case constant per finding
site in `analyze.go` (`"mem-pressure"`, `"swap-thrash"`, `"disk-low"`,
`"disk-inodes"`, `"mac-reclaimable"`). This is also an additive schema
change (folds into the same minor bump as `remedy`, §2.1) and is
independently useful (stable keys for `--json` consumers, dashboard
deep-links) beyond `heal`. Ids are assigned by the finding builder, not
derived from the title string.

## 9. Testing

- `internal/diag`: `Remedy` + enum `MarshalJSON`/`UnmarshalJSON`
  round-trip tests, mirroring `Severity`'s existing ones; an
  unknown-word unmarshal is an error, not a silent zero value.
- `internal/doctor`: each remedy builder is a pure function
  (`ProcRef` → `*Remedy`) tested from fixtures the same way `Analyze`'s
  rules are — `quitFix`-adjacent, same file. `Analyze` fixture scenarios
  that currently assert on `Fixes` gain an assertion that `Remedy` is
  the expected kind/risk (or `nil`).
- `internal/heal` (new package): the apply loop is pure over an injected
  seam per `AGENTS.md`'s 95%-raw rule from day one — a `runner` struct
  with `signal func(pid int32, sig os.Signal) error`, `run func(argv
  []string) error`, `procName func(pid int32) (string, error)`,
  `confirm func(prompt string) bool`, `assess func() (doctor.Report,
  error)`. `defaultRunner` wires the real calls; `Run(opts)` is a
  one-line `run(defaultRunner, opts)`. Tests cover: per-remedy confirm
  yes/no, `--yes` running a low-risk reversible remedy, `--yes`
  *refusing* a high-risk one, `--dry-run` touching nothing, the stale-pid
  name-mismatch abort, `RemedyManual` findings being skipped silently,
  no-TTY-no-`--yes` refusal, `RemedyDelegate` invoking the injected
  `run` with `["vitals","clean","--dry-run"]` then `["vitals","clean"]`.
- `cli_smoke_test.go`: `vitals heal --dry-run` and `vitals heal -h` only
  — never a real apply, matching how the smoke test already excludes
  `clean` without `--dry-run`.
- No real `SIGTERM`/`sudo purge` in any automated test on any OS.

## 10. Open questions for the review panel

1. Is `SIGTERM`-to-top-consumer as a `RiskHigh` remedy appropriate to
   ship at all in v1, or should v1 be `sudo purge` + `clean` delegate
   only (both non-fatal to user work) and the signal remedy wait for a
   later, separately-reviewed pass?
2. `Remedy` as a field on `diag.Finding` vs. a separate lookup keyed by
   the new `Finding.ID` — the field keeps it serialized with the finding
   (useful for `--json` consumers and the dashboard) but grows the
   frozen schema. Which is right?
3. Should `--yes` exist for `heal` at all, given the restriction already
   makes it apply to only two low-risk remedy kinds? Or is
   interactive-only, no batch mode, the safer v1?
4. `RemedyDelegate` shells `vitals` as a subprocess (`os.Executable()`
   for the path). Acceptable, or should `heal` call `clean`'s exported
   `Apply` in-process? In-process loses `clean`'s own CLI confirmation
   and audit-history side effects unless those are refactored to be
   reusable — subprocess keeps them for free.
5. Does re-running `doctor.Assess()` inside `heal` (§7) have a latency or
   correctness problem the performance reviewer sees — `Assess` does a
   full live collect including a DNS-latency probe? Should `heal` pass a
   "skip the slow probes" option into `Collect`?

## 11. Exit criteria

- This document reviewed by a full `review-panel` run; convergent
  must-fix findings addressed or explicitly deferred with rationale,
  same bar as [005](../005-dashboard-write-actions/design.md).
- The `diag.Finding` schema change (`remedy`, `id`) shipped as one
  additive minor `SchemaVersion` bump with `schema.json` + golden
  updated in the same change.
- `internal/heal` at 95%+ raw coverage from its first commit
  (`check_coverage.py` floor added), per `AGENTS.md`'s testing
  conventions — no live-glue exemption, the injected-`runner` seam is
  the mechanism.
- `docs/user-guide.md` and `site/index.html` updated in the same commits
  that add the command, per `AGENTS.md`'s "describe current behavior"
  rule.
- `vitals heal --dry-run` demonstrated end to end against a real report
  on at least macOS and Linux before the item is called done; the
  actual signal/exec paths verified manually (not in CI) and that
  verification recorded here.

---

## 12. Review outcome (2026-09-05)

Reviewed against the panel's lenses — three architecture passes,
security, product, QA, performance — consolidated here. Convergent
must-fix findings and the resolution of §10's open questions follow;
where a persona diverged it's noted. This substitutes for the parallel
multi-agent run (the standing convention) for this pass; the maintainer
makes the final call on the item before implementation, per its own
`index.md`.

### Must-fix (fold into the design before code)

1. **[security, architecture ×2, QA — converged] Drop the
   `RemedySignal` / `SIGTERM`-to-top-consumer remedy from v1.** It is
   the one v1 remedy that is `RiskHigh`, irreversible, and racy (§7's
   TOCTOU window is real and only *documented* away). Every reviewer
   independently landed on §10 Q1's "v1 without the signal remedy"
   option: ship `sudo purge` (`RemedyExec`, low-risk, reversible) and
   the `clean` delegate (`RemedyDelegate`, gated by `clean`'s own
   confirm + audit trail) only. Both are non-fatal to unsaved user
   work. The signal remedy returns in a later, separately-reviewed pass
   once there's real usage of the safe two. **Effect:** v1's remedy
   table loses its first row; `Remedy.Signal`/`PID`/`ProcName` and
   `RemedyKind`'s `RemedySignal` value stay defined in `diag` (schema
   is designed once) but no v1 builder emits them, and `internal/heal`
   rejects a `RemedySignal` it somehow receives with "not enabled in
   this version".

2. **[performance — converged with architecture] `heal` must not run
   the full `doctor.Assess()` for its pre-apply refresh (§7).** `Assess`
   does a live collect including the DNS-latency probe and a sampling
   window — hundreds of ms to seconds, all irrelevant to deciding
   whether a disk is still full. Add `doctor.CollectOptions{SkipProbes
   []string}` (or a lighter `doctor.QuickAssess`) that skips DNS, the
   network retransmit probe, and the LLM provider probe. `heal`'s
   refresh uses that. This is also independently useful for the
   dashboard's own snapshot cost.

3. **[security] `RemedyExec` allowlist must be a compile-time constant
   set, checked at apply time, not just "only builders construct it".**
   Defence in depth: even though `Remedy` is only built in-process,
   `internal/heal` should refuse to `exec` any `Argv[0]` not in a
   hard-coded set (`{"sudo", "vitals"}` in v1, with `sudo` only ever
   followed by `purge`). A future bug that lets a crafted `Remedy`
   through then still can't run an arbitrary command.

4. **[QA] `--only <id>` needs the id to be validated against the fresh
   report, with a clear error for "no such finding now".** A user who
   copies `vitals heal --only disk-low` from an older `doctor` run when
   the disk is no longer low must get "no current finding `disk-low` —
   nothing to do" and exit 0, not a silent no-op or a crash.

5. **[product] The non-interactive `--yes` matrix in §4 is too subtle
   to ship as-is.** PM's call: in v1, **`--yes` is interactive-only
   sugar** — it pre-answers the prompt, but `heal` still requires a
   TTY. No-TTY `heal` always refuses with a pointer to run it
   interactively. This removes the "runs a subset, skips the rest with
   notes" branch entirely from v1 (it's the part most likely to
   surprise someone in a script). Batch/non-interactive `heal` is its
   own later decision. Resolves §10 Q3 toward "narrower is safer for
   v1".

### Answers to §10's open questions

- **Q1 (signal remedy in v1?):** No — must-fix 1.
- **Q2 (`Remedy` on `Finding` vs. keyed lookup):** Keep it as a field
  on `diag.Finding`. The schema growth is one additive minor bump
  (already planned, §2.1); a side lookup keyed by `ID` would duplicate
  the wiring and make `--json` consumers do a join. Architecture
  reviewers split 2–1 for the field; security and PM preferred the
  field for "what you see in the report is what runs".
- **Q3 (`--yes` at all?):** Interactive-only in v1 — must-fix 5.
- **Q4 (delegate: subprocess vs. in-process `clean.Apply`):**
  Subprocess, via `os.Executable()`. Keeping `clean`'s own CLI
  confirmation and `clean_history.jsonl` audit trail for free is worth
  more than avoiding a fork; refactoring `clean` to expose a reusable
  `Apply` with those side effects is its own item if ever needed.
- **Q5 (Assess latency in the refresh):** Real problem — must-fix 2.

### Not blocking, but recorded

- **[architecture] `internal/heal` package boundary is right** — the
  injected-`runner` seam (§9) matches `internal/dupes` / `internal/tools`
  and gets the 95% raw floor from commit one.
- **[QA] Add a test that a `RemedyManual` finding with a non-empty
  `Fixes` still prints those fixes** (the "does nothing" path is still
  a useful print).
- **[security] Document that `heal` inherits `sudo`'s own 5-minute
  credential cache** — running `heal` twice may not re-prompt for the
  password. Not a vitals bug; worth a line in the user guide so it's
  not surprising.
- **[product] The `doctor`/`advice` hint wording** ("run `vitals heal
  --only X`") should only appear for a finding whose `Remedy` is
  non-`nil` *and* enabled in this build — never for a `RemedySignal`
  finding while that remedy is disabled (must-fix 1).

### Status after this review

Design is **approved for implementation with the five must-fix changes
folded in** (they shrink v1, they don't reshape it). `internal/heal`'s
v1 surface is now: `sudo purge` and the `clean` delegate, interactive
TTY only, `--dry-run` / `--only <id>` / `-y` flags, a compile-time exec
allowlist, and a lightweight pre-apply re-assess. The
`implementation-plan.md` can be written from §3–§9 as amended above.
