# 012 — Disk consumers: what's using space and I/O

[docs](../../../index.md) / [Roadmap](../../index.md) / **012 — Disk consumers: what's using space and I/O**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: Not started — captured 2026-09-05 from the maintainer's
"I see no top 5 in disk/network" feedback on the redesigned dashboard.
The GPU half of that feedback (per-process VRAM) shipped with the
dashboard redesign; this item is the disk half, which needs new
sampling infrastructure and so is its own scoped piece of work.
**Depends on**: `internal/monitor`'s process sampling (existing);
overlaps [010](../010-companion-tools-integration/) (a size scanner is
a natural companion-tool handoff)
**Target release**: not yet
**Architecture**: not yet written

## What

The Disk resource page and `vitals disk` show per-filesystem capacity
and aggregate I/O rates, but nothing about *which* processes or *which
paths* account for them. Two related gaps:

1. **Top processes by disk I/O** — a per-process read/write rate table,
   the same shape the CPU and Memory pages already have. gopsutil
   exposes per-process cumulative I/O byte counters; turning those into
   a *rate* needs a two-sample diff over a short window, the same way
   per-process CPU% is already sampled in `topProcesses`
   (`internal/monitor`). It also needs a real check of what those
   counters actually return per OS — on some platforms per-process I/O
   is permission-gated or simply zero for processes the caller doesn't
   own, and the page must degrade honestly (omit the section) rather
   than show a table of zeros.

2. **Biggest files / directories** — "what's eating my disk" answered
   with actual paths, not just a used-percentage. vitals has no
   filesystem size scanner today and should not grow a naive
   recursive walker (unbounded time and I/O on a large tree). The two
   viable paths, to be settled in the design: a bounded, depth- and
   time-limited walker owned by vitals, or a handoff to an installed
   size-scanner companion tool (the `explore` command already delegates
   to one for interactive use — this would be the non-interactive,
   parsed-result version). [010](../010-companion-tools-integration/)
   is where the companion-tool side of that decision lives.

## Why this is its own item, not part of the dashboard redesign

The dashboard redesign added per-process tables to the pages whose data
was *already available* from one `doctor`/`monitor` snapshot (CPU,
memory, and GPU VRAM once it was threaded through). Disk I/O rate and
path-level size are both genuinely new data acquisition:

- per-process I/O rate = a new sampled field on `ProcInfo`, a new
  source function, a second timed read in the sample window, and
  per-OS validation of what the counters mean;
- biggest paths = either a new bounded scanner or a new parsed
  companion-tool handoff, plus the destructive-action question if any
  "reveal / delete this" affordance is ever attached to a result.

Neither is a quick pattern application, so both go through a design doc
and a `review-panel` pass first, per `AGENTS.md`'s "Roadmap discipline".

## Not in scope

- **Per-process network throughput.** The same feedback asked for it;
  it is out of reach with the current data source — gopsutil exposes a
  process's open sockets, not its transferred byte counts, so a
  per-process network table can't be built without a platform-specific
  probe that doesn't exist in the codebase. If it's ever done it's a
  separate item with its own dependency discussion, not this one.

## Plan

[`implementation-plan.md`](implementation-plan.md) — empty until this is
designed and reviewed.
