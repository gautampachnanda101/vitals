# 012 — Per-resource consumers: the deeper numbers

[docs](../../../index.md) / [Roadmap](../../index.md) / **012 — Per-resource consumers: the deeper numbers**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: First pass shipped with v0.8.0; [`design.md`](design.md)
drafted 2026-09-05 for the three deeper numbers (per-process disk I/O
rate, per-process network bandwidth, real per-process energy) —
pre-review.
**Depends on**: `internal/monitor` sampling (existing);
overlaps [010](../010-companion-tools-integration/)
**Target release**: not yet
**Architecture**: [`design.md`](design.md) — the per-process disk-I/O rate, the per-process-network-bandwidth wall and its options, and real macOS `top -stats power` energy; each a separate sub-feature with its own platform story. Pre-review.

## Shipped with the redesign

Each resource page got a section ranked by, or listing, that resource's
own metric — no CPU list mislabelled as something else:

| Page | Section | Source |
|---|---|---|
| CPU | Top processes by CPU | `monitor.Sample` |
| Memory | Top processes by RSS | `monitor.Sample` |
| GPU | Processes holding VRAM (NVIDIA); top-by-RSS for Apple unified memory | `doctor.GPU.Processes` / `monitor.Sample` |
| Disk | Biggest directories + biggest single files in the home folder | bounded concurrent `WalkDir`, 90s cache |
| Network | Active connections (process → remote host:port, state) | `net.Connections`, 5s cache |
| Power | "Likely energy impact" — CPU-ranked, **captioned as an estimate** | `monitor.Sample` |

## What's left — the deeper numbers

Three of those are proxies or partials. The real versions each need a
new data source:

1. **Disk — real per-process I/O rate.** The shipped section answers
   "what's taking space"; it doesn't answer "what's hammering the disk
   right now". gopsutil has per-process cumulative I/O counters but
   they return **all-zero on macOS** (validated 2026-09-05, 432 of 593
   processes) — no Darwin implementation — and even on Linux/Windows
   they need a two-sample rate diff added to `topProcesses`. Also on
   the table for this page: a proper depth-drillable size view rather
   than a one-level home-folder scan, and whether an installed
   size-scanner companion tool should back it when present
   ([010](../010-companion-tools-integration/)).

2. **Network — bandwidth per process.** The shipped section lists
   *which* connections are open; it can't say which process is moving
   the most bytes, because gopsutil exposes sockets, not per-process
   byte counters, on any platform. A real answer needs a
   platform-specific probe that isn't in the codebase.

3. **Energy — a real per-process power number.** The shipped section is
   an explicit CPU-based estimate. macOS's `top -l 1 -stats pid,command,power`
   gives the same per-process power score the OS's own tools show,
   **without sudo** — that's the Darwin target (`powermetrics` is more
   detailed but needs root, so it's out for a passive dashboard).
   Linux and Windows have no equivalent no-privilege per-process
   energy number; the design says what they show instead.

## Reference, not a copy

Activity Monitor / Task Manager per-tab consumer views are the bar for
*what information is useful* — biggest disk users, who's on the network,
what's draining the battery. vitals reaches those answers with its own
data sources, layout and wording; nothing is lifted. Same rule
[011](../011-console-at-a-glance-view/) states for the console view.

## Plan

[`implementation-plan.md`](implementation-plan.md) — empty until the
remaining three are designed and reviewed.
