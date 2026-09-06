# Implementation plan — 012 Per-resource consumers: the deeper numbers

[docs](../../../index.md) / [Roadmap](../../index.md) / [012 — Per-resource consumers: the deeper numbers](index.md) / **Implementation plan**

The first-pass per-resource sections shipped with the v0.8.0 dashboard
redesign (see `index.md`'s "Shipped with the redesign" table).

The three deeper numbers were reviewed inline (`design.md` §7) and
resolved:

| Sub-feature | Outcome | Where |
|---|---|---|
| Per-process disk I/O rate | **Shipped.** `monitor.ProcInfo.DiskRead/WriteBytesPerSec` from a second `IOCounters()` read on the existing sample window; `monitor.HasDiskIORates` gates the section. Dashboard Disk page (`diskIOProcessSection`) + `vitals disk` (`printDiskIOProcs`). Real on Linux/Windows; omitted on macOS (gopsutil returns all-zero). | `internal/monitor`, `internal/dashboard/modules_resource.go`, `internal/doctor/focus_diskio.go` |
| Per-process network bandwidth | **Not built.** gopsutil exposes sockets, not per-process byte counts, on every platform. The active-connections list stays the answer. An `internal/tools` opt-in over an installed `nethogs`-class tool is a possible future enhancement. | — |
| Real per-process energy | **Shipped (macOS).** New `internal/power` — `Sample()` parses the 2nd sample block of `top -l 2 -o power`. Dashboard Power page (`powerImpactSection`, 10s cache) replaces the CPU estimate; `vitals power` (`printPowerProcs`) adds the table. CPU-ranked estimate stays the Linux/Windows fallback. | `internal/power`, `internal/dashboard/power_impact.go`, `internal/doctor/focus_power.go` |

No `--json` schema change — all three are presentation-only.
