# Design — 012 Per-resource consumers: the deeper numbers

[docs](../../../index.md) / [Roadmap](../../index.md) / [012 — Per-resource consumers: the deeper numbers](index.md) / **Design**

**Status: draft, pre-review.** The first-pass per-resource consumer
sections shipped with the v0.8.0 dashboard redesign (see `index.md`'s
"Shipped with the redesign" table). This doc is for the three that are
still proxies or partials — each needs a new, platform-specific data
source, so per `AGENTS.md`'s "Roadmap discipline" it gets a design +
`review-panel` pass before code. An "As built" section will be appended
after implementation.

## 1. What's already shipped, and what this is

Every resource page has a "what's using it" section. Three are honest
proxies today:

| Page | Shipped | The gap this doc closes |
|---|---|---|
| Disk | biggest dirs/files in `$HOME` (bounded scan) | *which process is hammering the disk right now* |
| Network | active connection list (process → remote host) | *which process is moving the most bytes* |
| Power | CPU-ranked "likely energy impact", captioned as an estimate | *a real per-process energy figure* |

Each is a separate sub-feature with its own data source and its own
platform story. They share nothing but this doc; the review can green-
light them independently.

## 2. Disk — per-process I/O rate

### Data source

gopsutil's `process.IOCounters()` returns cumulative `ReadBytes` /
`WriteBytes` per process. A *rate* needs the same two-sample diff over a
window that per-process CPU% already uses in `internal/monitor`'s
`topProcesses` (prime, `time.Sleep(window)`, re-read, delta / secs).

**Validated 2026-09-05:** on macOS, `IOCounters()` returns **no error
and all-zero values** for every readable process (432 of 593) — gopsutil
has no Darwin implementation. So this is a **Linux/Windows-only** signal
with the current dependency.

### Proposal

- `internal/monitor`: add `DiskReadPerSec` / `DiskWritePerSec` to
  `ProcInfo`, filled from a second `IOCounters()` read on the sample
  window `topProcesses` already sleeps. Zero-cost — it rides the
  existing window.
- On macOS the fields stay zero; the renderer omits the "Top processes
  by disk I/O" section when every value is zero (same "degrade
  honestly" rule the S.M.A.R.T. work used), rather than showing a table
  of zeros or a CPU list mislabelled as disk.
- Dashboard Disk page + `vitals disk` gain the section on the platforms
  where it's real.
- `--json`: additive `processes[].disk_read_bytes_per_sec` /
  `disk_write_bytes_per_sec` under a new `snapshot.processes`? **No** —
  the `--json` snapshot has no per-process array today (`ProcRef` is
  one top-consumer per resource). Adding a full process table to the
  frozen schema is a bigger decision than this item; v1 keeps the disk
  I/O rate a *presentation-only* signal (like `topRemotePeers`), not a
  schema field. The review confirms.

### Open questions

1. macOS: accept the gap (Linux/Windows only, documented), or add a
   `top -l 2 -stats pid,command,bytes_read,bytes_written` parse as a
   companion-tool-style handoff (`top` is always present on macOS, no
   sudo)? The latter is a real parser to own and keep in sync.
2. Is a second `IOCounters()` sweep over the whole process table on
   *every* `doctor`/dashboard refresh acceptable cost, or does it gate
   behind an opt-in like `vitals dupes --fast`?

## 3. Network — per-process bandwidth

### The wall

gopsutil exposes a process's **sockets** (`process.Connections()`),
never its transferred **byte counts**, on any platform. The shipped
"active connections" list is the honest answer to "who's on the
network"; "who's moving the most bytes" is genuinely out of reach with
the current dependency.

### Options for the review

- **A — accept the limitation.** Keep the connection list; document
  that per-process throughput isn't available. Lowest cost, no new
  dependency, no new privilege.
- **B — macOS `nettop` handoff.** `nettop -P -L 1 -x` gives per-process
  bytes in/out without sudo. A parser to own; macOS-only; `nettop` is
  always present.
- **C — Linux `/proc/<pid>/net/dev`** is per-network-namespace, not
  per-process, so it doesn't actually answer this on Linux either.
  eBPF would, but that's a hard dependency and a privilege escalation —
  out of scope for a passive tool.
- **D — the `internal/tools` pattern**: if `nethogs` (Linux) or a
  similar tool is installed, parse it; otherwise show the connection
  list. Consistent with how `jdupes`/`smartctl` are handled.

Recommendation to the panel: **A for the cross-platform baseline, D as
the opt-in enhancement.** B only if macOS parity is judged worth a
dedicated parser.

## 4. Power — real per-process energy

### Data source

Activity Monitor's "Energy Impact" is real OS power accounting, not
CPU%. `top -l 1 -stats pid,command,power` on macOS exposes the **same
per-process power score without sudo** (`powermetrics` is more detailed
but needs root — out for a passive tool). Linux and Windows have no
equivalent no-privilege per-process energy number.

### Proposal

- macOS: a `top -stats power` parse (bounded, one-shot), feeding a real
  "Energy impact" column that replaces the CPU-based estimate and drops
  its "this is an estimate" caption.
- Linux/Windows: keep the CPU-ranked estimate with its honest caption
  (there is no better signal available without a hard dependency).
- This is presentation-only, no schema change.

### Open question

`top -l 1` on macOS has its own ~1–2s settle for accurate percentages.
Does the Power page run it eagerly (adding that latency to the page) or
behind a "Measure energy" button like the Clean page's Preview? The
button keeps the page fast and matches the destructive-action-style
deliberate-action pattern, even though this one only *reads*.

## 5. Cross-cutting

- **No hard new dependency.** Every option above is either a parser
  over an always-present OS tool (`top`, `nettop`) or an
  `internal/tools`-registry opt-in (`nethogs`). Nothing vendors a
  library. That keeps the "one dependency (gopsutil)" claim intact —
  the review confirms this per sub-feature.
- **Honest degradation everywhere.** A section that can't be filled for
  real on this platform is omitted, never faked and never filled with
  a mislabelled proxy. This is the rule the S.M.A.R.T. and
  dashboard-redesign work already set.
- **Presentation-only, no `--json` growth in v1.** None of these add a
  field to the frozen schema; a full per-process array in `--json` is
  its own separate decision.

## 6. Verification gates

Standard repo gates. Per sub-feature: pure fixture-tested parsers for
any `top`/`nettop` output (real captured output as the fixture, both
populated and empty cases, and a hard error); the injected-exec seam
(`internal/tools` / `internal/smart` pattern) so CI needs neither tool;
`vitals disk` / `vitals net` / `vitals power` and the matching
dashboard pages exercised end to end on macOS + Linux before the
sub-feature is called done, with the per-OS behaviour (real number vs.
omitted section vs. captioned estimate) recorded here.

## Plan

[`implementation-plan.md`](implementation-plan.md) — stays as-is (the
shipped first pass is recorded there) until this doc's `review-panel`
pass converges and its must-fix findings are folded in, at which point
the three sub-features get task lists.
