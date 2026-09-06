# Design — 012 Per-resource consumers: the deeper numbers

[docs](../../../index.md) / [Roadmap](../../index.md) / [012 — Per-resource consumers: the deeper numbers](index.md) / **Design**

**Status: shipped.** The first-pass per-resource consumer sections
shipped with the v0.8.0 dashboard redesign (see `index.md`'s "Shipped
with the redesign" table). The three deeper numbers were then reviewed
inline (§7) and implemented: disk per-process I/O rate and the macOS
per-process energy reading are built; per-process network bandwidth is
not achievable with the current dependency and stays the
active-connections list. §8 records what was built.

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

## 7. Decisions (inline review, 2026-09-06)

The three sub-features were reviewed together against this doc and the
existing `internal/monitor` / `internal/doctor` code before
implementation. Outcome:

1. **Disk I/O rate — build it.** `internal/monitor` samples per-process
   `IOCounters()` twice across the window `topProcesses` already sleeps
   for CPU% (open question 2: *ride the existing window, no opt-in
   flag* — the loop already walks every process for CPU priming, so the
   extra counter read is not a second sweep). Open question 1:
   *accept the macOS gap* — no `top` byte-count parser. On macOS every
   rate is zero and both renderers omit the section.
2. **Network bandwidth — accept the wall (option A).** gopsutil gives
   sockets, not per-process byte counts, on every platform; the shipped
   active-connections list stays the answer to "who's on the network".
   No code. Option D (an `internal/tools` opt-in over an installed
   `nethogs`-class tool) is left as a future enhancement, not v1.
3. **Power energy — build the macOS parser, run it eagerly.** Open
   question: *eager, not a button* — `vitals power` is already a
   deliberate deep-dive command (it runs a DNS probe on `net`), and the
   dashboard Power page caches the probe for 10s. `powermetrics` stays
   out (root).

No `--json` schema change for any of the three (§5 holds).

## 8. As built

**Disk — per-process I/O rate.** `monitor.ProcInfo` gained
`DiskReadBytesPerSec` / `DiskWriteBytesPerSec` (JSON
`disk_read_bytes_per_sec` / `disk_write_bytes_per_sec`, `omitempty`),
filled in `topProcesses` from a second `process.IOCounters()` read over
the existing sample window; the divisor is floored at 1ms and a
counter that goes backwards (PID reuse) clamps to zero.
`monitor.HasDiskIORates([]ProcInfo) bool` is the "is this real on this
platform" gate.

- Dashboard **Disk** page: new "Top processes by disk I/O" table
  (`diskIOProcessSection`, `internal/dashboard/modules_resource.go`),
  ranked by read+write, top 5, shown only when `HasDiskIORates` is true —
  so absent on macOS, present on Linux/Windows. The biggest-directories /
  biggest-files scan is unchanged and still shown.
- `vitals disk`: `printDiskIOProcs` (`internal/doctor/focus_diskio.go`)
  adds the same ranking under the mount table, via an injectable
  `diskIOSampler` seam over `monitor.Sample`; silent when no process
  moves any bytes.
- Per-OS behaviour: **Linux/Windows** — real rates, section shown.
  **macOS** — gopsutil returns all-zero, section omitted, no table of
  zeros and no CPU list mislabelled as disk activity.

**Network — per-process bandwidth.** Not built (see §7.2). The Network
page and `vitals net` keep the active-connections list.

**Power — real per-process energy (macOS).** New `internal/power`
package: `Sample(limit) ([]Proc, bool)` shells `top -l 2 -o power
-stats pid,command,power -n 40` (bounded at 4s), parses the **second**
sample block (`parseTopPower` — the first reports every process at
0.0), ranks by the power score. `deps{goos, run}` is the injected exec
seam; `ok=false` on any non-darwin platform and on any probe/parse
failure.

- Dashboard **Power** page: `powerImpactSection`
  (`internal/dashboard/power_impact.go`, behind a 10s single-flight
  `powerImpactCache`) renders "Energy impact by process" from the real
  reading and **replaces** the CPU estimate; the CPU-ranked estimate
  with its "CPU-based estimate … on this platform" caption is the
  fallback when `ok=false`.
- `vitals power`: `printPowerProcs` (`internal/doctor/focus_power.go`,
  injectable `powerSampler`) adds an "energy impact by process" table on
  macOS; nothing on Linux/Windows.

**Verification.** `internal/power` parser is fixture-tested against real
captured `top` output (both sample blocks, multi-word command names,
garbage/short/empty rows, an embedded escape sequence); every exec is
injected so CI needs no `top`; `internal/monitor`, `internal/doctor`
and `internal/dashboard` cover the new rate maths, the
platform-omission gate and the estimate/real fallback. `check_coverage.py`
gained a `vitals/internal/power` floor.

## Plan

[`implementation-plan.md`](implementation-plan.md) records the shipped
first pass and this second pass.
