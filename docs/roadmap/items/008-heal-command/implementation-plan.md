# Implementation plan — 008 `vitals heal`

[docs](../../../index.md) / [Roadmap](../../index.md) / [008 — `vitals heal`](index.md) / **Implementation plan**

Written from [`design.md`](design.md) §3–§9 **as amended by the §12
review**. Not started — awaiting the maintainer's go on the reviewed
design before code, since every task here executes remediation
(`sudo purge`, a `clean` subprocess) and the standing rule is that
destructive actions get explicit sign-off. Each task is one
working, tested increment; check it off as it lands.

## v1 surface (post-review)

`vitals heal` — interactive TTY only. Remedies: `sudo purge`
(`RemedyExec`, low-risk, reversible) and `vitals clean` via delegate
(`RemedyDelegate`, gated by `clean`'s own confirm + audit trail).
**No `RemedySignal` in v1.** Flags: `--dry-run`, `--only <id>`, `-y`
(pre-answers the prompt; still requires a TTY).

## Tasks

- [ ] **`diag`: `Remedy` type + `Finding.Remedy` / `Finding.ID`.**
      Add `Remedy`, `RemedyKind` (incl. `RemedySignal` defined but
      unused in v1), `RemedyRisk`, and `MarshalJSON`/`UnmarshalJSON`
      for the enums emitting stable lowercase words — mirror
      `diag.Severity`'s existing round-trip. `Finding` gains
      `Remedy *Remedy` and `ID string`. Round-trip + unknown-word-is-
      error tests, mirroring `Severity`'s.
- [ ] **`doctor`: schema bump.** `remedy` + `id` are additive →
      one minor `SchemaVersion` bump, `schema.json` updated, golden
      regenerated (`-run TestSchemaFieldsContract -update`). One bump
      for the release that ships `heal`.
- [ ] **`doctor`: finding ids + remedy builders.** A kebab-case `ID`
      constant per finding site in `analyze.go` (`mem-pressure`,
      `swap-thrash`, `disk-low`, `disk-inodes`, `mac-reclaimable`, …).
      Pure builder funcs for the two v1 remedies (`macReclaimable →
      *Remedy{Exec, ["sudo","purge"], RiskLow, reversible}`;
      `diskLow/diskInodes → *Remedy{Delegate, ["vitals","clean",
      "--dry-run"], RiskMedium}`). Existing `Analyze` fixtures that
      assert on `Fixes` gain a `Remedy` kind/risk assertion (or `nil`).
- [ ] **`doctor`: lightweight pre-apply assess (review must-fix 2).**
      `CollectOptions{SkipProbes []string}` (or `QuickAssess`) that
      skips the DNS-latency probe, the network retransmit probe, and
      the LLM provider probe. Tested that those probes don't run.
- [ ] **`internal/heal` package (new).** Injected `runner` seam:
      `run func(argv []string) error`, `confirm func(prompt string)
      bool`, `assess func() (diag.Report, error)`, `isTTY func() bool`.
      `defaultRunner` wires the real calls; `Run(opts)` is a one-liner.
      Apply loop:
      - re-assess (lightweight) → select findings (all, or the one
        `--only <id>` names; error "no current finding `<id>`" + exit 0
        if it's gone — review must-fix 4).
      - per finding with a non-nil, v1-enabled `Remedy`: show
        `Label` / exact `Argv` / `Risk` / `Reversible`, prompt (unless
        `-y`), then run via `run`.
      - **compile-time exec allowlist** (review must-fix 3): refuse any
        `Argv[0]` not in `{"sudo","vitals"}`, and `sudo` only followed
        by `purge`.
      - `RemedyManual` (and any `RemedySignal`): print the `Fixes`
        text, do nothing (review "not blocking" note: still print the
        fixes).
      - `--dry-run`: print resolved actions, touch nothing, exit 0.
      - no TTY: refuse with a pointer to run interactively (review
        must-fix 5) — regardless of `-y`.
      - `RemedyDelegate`: `run(["vitals","clean","--dry-run"])`, then on
        confirm `run(["vitals","clean"])`, `vitals` path from
        `os.Executable()`.
      95%+ raw coverage from the first commit; `check_coverage.py`
      floor added. No real `sudo purge` / `clean` in any test.
- [ ] **`main.go`: `heal` subcommand** — flag parsing → `heal.Run`.
      `cli_smoke_test.go`: `vitals heal --dry-run` and `vitals heal -h`
      only, never a real apply.
- [ ] **`doctor` / `advice` hint** — for a finding whose `Remedy` is
      non-nil *and* v1-enabled, add "run `vitals heal --only <id>` to
      act on this" to the output. Never for a disabled `RemedySignal`
      finding (review "not blocking" note).
- [ ] **Docs** — `docs/user-guide.md` gains a `vitals heal` section
      (incl. the `sudo` 5-minute credential-cache note from the
      review); `site/index.html` feature card + version footnote if a
      release ships it; `design.md` gets its "As built" appendix.
- [ ] **Manual verification** — `vitals heal --dry-run` end to end on
      macOS and Linux; the real `sudo purge` and `clean` delegate
      paths exercised by hand (not CI) and the result recorded in
      `design.md`.

## Exit criteria

Per `design.md` §11, as amended: the five §12 must-fix changes are in;
the schema change is one additive minor bump; `internal/heal` is at
95%+ raw from commit one; docs updated in the same commits; `--dry-run`
demonstrated on macOS + Linux with the real paths hand-verified.
