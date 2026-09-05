# 012 — Per-resource consumers: disk, network, energy

[docs](../../../index.md) / [Roadmap](../../index.md) / **012 — Per-resource consumers: disk, network, energy**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Not started — captured 2026-09-05 from the maintainer's
"every panel should show top 5/10, appropriate to the panel" feedback on
the redesigned dashboard, with Activity Monitor's per-tab consumer
tables as the reference for the bar to clear (inspiration, not a copy).
**Depends on**: `internal/monitor` sampling (existing);
overlaps [010](../010-companion-tools-integration/) (a size scanner and
an energy reading are both natural companion-tool / OS-tool handoffs)
**Target release**: not yet
**Architecture**: not yet written

## What

The redesigned dashboard added a per-process "top consumers" table to
the pages where the ranking metric was genuinely available from one
snapshot — **CPU** (by CPU%), **Memory** (by RSS), **GPU** (by VRAM,
where a real per-process reading exists). Three pages still have no
consumer table because the honest per-resource metric isn't available
from the current data source, and a CPU-ranked list mislabelled as that
resource's activity was explicitly rejected. This item is those three,
each with the shape the maintainer chose:

1. **Disk → biggest files / directories.** "What's eating my disk"
   answered with real paths, not just a used-percentage. vitals has no
   filesystem size scanner and must not grow a naive recursive walker
   (unbounded time and I/O on a large tree). Design picks one of: a
   bounded depth- and time-limited walker owned by vitals, or a parsed
   handoff to an installed size-scanner companion tool (the `explore`
   command already delegates to one interactively — this is the
   non-interactive, parsed-result version). Any "reveal / delete this
   path" affordance on a result triggers the destructive-action confirm
   rule and is out of scope for a first cut.

   *Per-process disk I/O rate* is a secondary option here, not the
   primary: gopsutil exposes cumulative per-process I/O counters, but
   validated 2026-09-05 they return **no error and all-zero values on
   macOS** (432 of 593 live processes) — no Darwin implementation. It's
   a Linux/Windows-only signal that also needs a two-sample rate diff.
   Biggest-paths is the cross-platform answer and the priority.

2. **Network → active connections list.** Not a ranked process table —
   the actual open connections: process, remote host:port, state, the
   way `netstat` / Activity Monitor's Network detail shows them.
   gopsutil's `net.Connections` returns sockets with owning PIDs in one
   call; join to process names from the existing process cache. Needs a
   small TTL cache of its own (one `net.Connections` call is not free)
   and a decision on how much of the connection list to show and how to
   order it. Per-process transferred **bytes** remain out of reach
   (gopsutil gives sockets, not byte counts) — connections is the
   available, network-appropriate answer.

3. **Energy → real per-process energy impact.** Activity Monitor's
   Energy tab shows a real "Energy Impact" score per app (and 12-hour
   power, App Nap, preventing-sleep). That comes from macOS power
   accounting, not from CPU%. `top -l 1 -stats pid,command,power` on
   macOS exposes the same per-process power score **without sudo** —
   that's the target on Darwin. `powermetrics` is more detailed but
   needs root, so it's not an option for a passive dashboard. Linux and
   Windows have no equivalent no-privilege per-process energy number;
   the design says what those platforms show instead.

   **Interim, shipped with the dashboard redesign:** the Power page
   carries a CPU-ranked list under "Likely energy impact" with a caption
   stating plainly it is a CPU-based estimate, not a power reading. That
   caption goes away once the real number lands.

## Why this is its own item, not part of the dashboard redesign

The redesign added consumer tables only where the data was *already*
in one `doctor`/`monitor` snapshot. All three gaps here are genuinely
new data acquisition, each platform-specific:

- biggest paths = a new bounded scanner or a parsed companion-tool
  handoff, plus the destructive-action question if a result ever gets a
  "delete" affordance;
- active connections = a new `net.Connections` sample with its own
  cache and an ordering/volume decision;
- real energy = parsing a macOS OS tool's output, with no cross-platform
  equivalent, so a per-OS story.

Each goes through a design doc and a `review-panel` pass first, per
`AGENTS.md`'s "Roadmap discipline".

## Reference, not a copy

Activity Monitor's per-tab consumer tables are the bar for *what
information is useful* — biggest disk users, who's on the network, what's
draining the battery. vitals reaches those answers with its own data
sources, its own layout, and its own wording; nothing is lifted. Same
rule [011](../011-console-at-a-glance-view/) states for the console view.

## Plan

[`implementation-plan.md`](implementation-plan.md) — empty until this is
designed and reviewed.
