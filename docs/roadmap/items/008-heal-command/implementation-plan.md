# Implementation plan — 008 `vitals heal`

[docs](../../../index.md) / [Roadmap](../../index.md) / [008 — `vitals heal`](index.md) / **Implementation plan**

Delivered 2026-09-06, built to [`design.md`](design.md) §3–§9 as amended
by the §12 review. What shipped:

- [x] **`diag`: `Remedy` type + `Finding.Remedy` / `Finding.ID`.**
      `Remedy{Kind, Label, Argv, Signal, PID, Risk, Reversible}`;
      `RemedyKind` (manual/exec/delegate/signal — signal defined but
      v1-disabled) and `RemedyRisk` (low/medium/high) with
      `MarshalJSON`/`UnmarshalJSON` round-tripping stable lowercase
      words, unknown word → error. `Finding` gained `ID string` and
      `Remedy *Remedy`, both `omitempty`. 100% covered.
- [x] **`doctor`: `--json` schema 1.3.0 → 1.4.0** — additive `id` +
      `remedy` under `findings[]`; `schema.json` updated, golden
      regenerated.
- [x] **`doctor`: finding ids + the two v1 remedy builders.**
      `mac-reclaimable` → `purgeRemedy()` (`RemedyExec`, `sudo purge`,
      RiskLow, reversible); `disk-low` / `disk-inodes` →
      `cleanDelegateRemedy()` (`RemedyDelegate`, `vitals clean`,
      RiskMedium). The `SIGTERM` remedy from the pre-review design was
      **not** built (review must-fix 1).
- [x] **`doctor.QuickAssess` / `Options.SkipProbes`** (landed in the
      008/011 shared-foundation PR) — the lightweight pre-apply re-check
      (review must-fix 2): skips GPU/power/thermal/LLM, does not write
      history.
- [x] **`internal/heal` package.** Injected `runner`
      (`assess`/`exec`/`confirm`/`isTTY`/`goos`/`out`). Apply loop:
      no-TTY → refuse (must-fix 5); re-assess; `--only <id>` selects
      one, unknown id → "nothing to do", exit 0 (must-fix 4); per
      finding with a v1-enabled remedy show label/argv/risk/reversible,
      confirm (unless `--yes`), run; **compile-time exec allowlist**
      `{vitals, sudo purge}` checked at apply (must-fix 3);
      `RemedyManual`/`RemedySignal` → print Fixes, run nothing; `sudo
      purge` gated to `runtime.GOOS == "darwin"`; `RemedyDelegate` runs
      `vitals clean --dry-run` → re-confirm → `vitals clean`, `vitals`
      resolved via `os.Executable()`. 97.4% raw, `check_coverage.py`
      floor added.
- [x] **`main.go`: `heal` subcommand** — `--dry-run` / `--only` /
      `--yes` / `--ollama-url`; `help.go` entry; `main_test.go` dispatch
      test; `--yes` is interactive-only (must-fix 5), no batch mode.
- [x] **`doctor`/`advice` hint** — not added: the current design has
      `doctor`/`advice` print `Fixes` already; a "run `vitals heal
      --only X`" nudge is a small follow-up, deferred to keep this PR
      focused (the finding ids and remedies are in `--json` now, which
      is the load-bearing part).
- [x] **Docs** — `docs/user-guide.md` gains a `vitals heal` section
      (incl. the `sudo` credential-cache note); `design.md` §13 "As
      built"; §10 Q5's "Assess runs a DNS probe" error corrected (it
      doesn't — only `vitals net` does).

## Not done in v1 (deliberate, per the review)

- No `RemedySignal` remedy — returns in a separately-reviewed pass.
- No batch / non-interactive mode.
- No `doctor`/`advice` "run heal" nudge yet (finding ids + remedies are
  in `--json`; the nudge is cosmetic).
- Manual verification of the real `sudo purge` / `vitals clean` paths on
  macOS + Linux still to be recorded here by the maintainer.
