# Implementation plan — 010 Companion tools integration

[docs](../../../index.md) / [Roadmap](../../index.md) / [010 — Companion tools integration](index.md) / **Implementation plan**

This file shows what's **left**. Check off a task as it lands.

- [x] `nvtop` → `vitals gpu --live` — `main.go`'s `gpu` case gained a
      `--live` flag that hands off to `tools.Launch("GPU monitor", nil)`,
      the same pattern `explore`/`live` already use. Deliberately
      untested at the dispatch level, matching `explore`/`live`'s own
      precedent (item 009's already-settled conclusion for `main`'s
      dispatch: `cli_smoke_test.go` is the validation layer for this
      class of live-glue command, and `explore`/`live` are themselves
      excluded even from that — a real, unpredictable block on any CI
      runner where the target happens to be pre-installed — the same
      reasoning applies here). Note: `nvtop` genuinely has no Windows or
      macOS package upstream (Linux-only, NVIDIA/AMD/Intel discrete GPUs) —
      confirmed against `internal/tools`' own registry, not assumed;
      `--live` correctly reports "not installed" on those OSes, which is
      the tool's real absence, not a vitals gap.

- [x] `jdupes` as an optional accelerated `vitals dupes` backend —
      shipped as `vitals dupes --fast` (`internal/dupes/jdupes.go`).
      Opt-in per run, not automatic: the built-in `Scan` stays the
      default because the two aren't perfectly equivalent (see the two
      open questions below and how each was resolved). The **dashboard**
      dupes path is deliberately *not* wired to jdupes — it uses
      `dupes.ScanContext` with a hard file budget (`dupesFileBudget`)
      that jdupes has no equivalent for; losing that bound to gain
      speed on a background web request is the wrong trade.
      - **Resolved — scanned-file/byte total:** `Result` gained a
        `Backend` field (`"jdupes"` vs `""`/built-in). When jdupes ran,
        `ScannedFiles`/`ScannedBytes` stay 0 and every renderer
        (`render`, and `--json` consumers via the field) shows
        `scanned <root> via jdupes (fast backend — no file/byte total
        reported)` instead of a "scanned 0 files" line. No fabricated
        number, and the `--json` shape gains only an additive
        `backend` field.
      - **Resolved — `skipDirNames` parity:** a Go-side post-filter
        over jdupes' `matchSets` (`pathHasSkippedDir`), keeping
        `skipDirNames` the single source of truth rather than a
        parallel set of `-X nostr:` flags to keep in sync. A group
        whose surviving path count drops below 2 after filtering is
        dropped entirely. Accepts that jdupes still walks those dirs
        (wasted work, not wrong output).
      - Fallback: a jdupes error (non-zero exit, or output that
        doesn't parse as the expected JSON) makes `run` fall through
        to the built-in `Scan` silently rather than surface a partial
        result — validated behaviour from the plan below.

      **Original validated output** (`jdupes -r -j`,
      `jdupes -r -j -X size+=:<n>` against real temp trees, both
      duplicate-found and no-duplicates-found cases, and a hard error
      on a nonexistent path):
      - `-j`/`--json` gives clean, stable JSON:
        `{"exitStatus": 0, "matchSets": [{"fileSize": N, "fileList":
        [{"filePath": "..."}, ...]}, ...]}` — `matchSets` is `[]` (not
        `null`) when nothing is found, confirmed.
      - `-X size+=:<bytes>` is the min-size filter, confirmed exactly
        equivalent to `dupes.Options.MinSize`/`Scan`'s own
        `minSize` semantics (a sub-threshold pair correctly excluded,
        an at-or-above-threshold pair correctly included).
      - A real error (bad path) exits 1 with **no JSON at all** — plain
        text on stdout — so the parse path must treat "exit status
        nonzero" and "output doesn't parse as the expected JSON shape"
        as the same signal: fall back to `dupes.Scan`, never surface a
        partial/wrong result as if it succeeded.
      - Symlinks: not followed by default (matches `Scan`'s own
        default — `-s`/`--symlinks` is opt-in on jdupes' side, left
        off).
      - **Open question, not yet resolved**: jdupes' JSON gives no
        total-scanned-file/byte count, only the duplicate groups it
        found — but `dupes.Result.ScannedFiles`/`ScannedBytes` back a
        real "scanned N files under X" UI line in both the CLI and the
        dashboard (`renderDupesPreview`). Silently reporting `0` there
        when jdupes was actually the backend would be a real
        regression (a status line with no honest content behind it,
        the exact class of bug this session's own `AGENTS.md`
        Non-negotiable principles rule out) — needs a deliberate
        decision (omit the line entirely when the fast backend is
        used and say so; do a cheap separate count pass; something
        else) before this can ship, not a rushed default.
      - **Open question, not yet resolved**: `internal/dupes.Scan`'s
        own `skipDirNames` (`.git`, `node_modules`, `.cache`, trash
        cans, ...) has no jdupes-side equivalent applied yet — jdupes
        would need either its own `-X nostr:` filters per entry (more
        surface to keep in sync with `skipDirNames`) or a Go-side
        post-filter over `matchSets` (single source of truth, but
        means fully trusting jdupes' walk before filtering rather than
        pruning early) — not yet decided which.

- [x] `smartctl` → real S.M.A.R.T. health/wear on `vitals disk` and the
      dashboard — shipped as the `internal/smart` package
      (`smart.Probe`), wired into `doctor` via `source.smartProbe` and
      `attachSMART`. `doctor.Disk` gained an optional `SMART *DiskSMART`
      (`--json` schema **1.2.0 → 1.3.0**, additive `snapshot.disks[].smart`).
      New findings in `analyzeDisks`: `smart_status.passed == false` →
      **critical** ("back up now, plan replacement"); NVMe
      `percentage_used >= 90` → warn, `>= 100` → critical. `vitals disk`
      and the dashboard Disk page each show a S.M.A.R.T. line when data
      is available.
      - **Resolved — device resolution.** macOS: `diskutil info <mount>`
        → "Part of Whole" → `/dev/diskN` (the validated path; smartctl
        rejects the gopsutil `/dev/diskNsM` partition node). Linux:
        strip the partition suffix off the gopsutil device
        (`linuxWholeDisk` — `/dev/sda1`→`/dev/sda`,
        `/dev/nvme0n1p1`→`/dev/nvme0n1`, `/dev/mmcblk0p1`→`/dev/mmcblk0`;
        an unrecognised shape like an LVM/mapper path is passed through
        unchanged and simply fails the probe if smartctl can't address
        it). The regex is CI-tested; the live Linux `smartctl` call
        itself still hasn't run on real hardware this project has
        access to, but a wrong device just yields no `smart_status` and
        the mount is skipped — it can't produce a wrong reading.
      - **Resolved — Windows scope.** `probe` returns nothing on
        `windows`: `\\.\PhysicalDriveN` addressing is unvalidated, so
        rather than guess, the Disk page/`vitals disk` simply show no
        S.M.A.R.T. line there. Revisit with a real Windows box.
      - **Resolved — ATA wear.** Not parsed (vendor-inconsistent
        attribute table, no SATA hardware to validate). `Health.WearPct`
        is `-1` for non-NVMe drives and `matchSMART` maps that to "no
        wear value" rather than a bogus 0; only `smart_status` /
        `temperature` apply to ATA drives.

      **Original validated output** (`smartctl -a -j` against this
      machine's real NVMe SSD):
      - `smart_status.passed` (bool) is documented and confirmed as the
        universal pass/fail field across NVMe *and* ATA/SATA smartctl
        JSON output — the safe baseline signal for any drive type.
      - `temperature.current` likewise universal.
      - `nvme_smart_health_information_log.percentage_used` (0-100,
        NVMe's own wear indicator) is real and present — but **NVMe-only**,
        confirmed by its own field name and absence from the generic
        top-level schema; a SATA/ATA drive reports wear very
        differently (a `ata_smart_attributes` ID/VALUE/WORST/THRESH
        table, industry-inconsistent across vendors) that this session
        had no real SATA hardware to validate against, so ATA-specific
        wear parsing is explicitly out of scope for now — only the
        universal `smart_status`/`temperature` fields apply there.
      - **Real, validated device-resolution problem, not
        hypothetical**: `gopsutil`'s own `disk.Partitions()` `Device`
        field (e.g. `/dev/disk2s1s1` for `/` on this machine) is
        **not** what `smartctl` accepts — confirmed live:
        `smartctl -H /dev/disk2s1s1` fails outright ("Unable to detect
        device type"), while `smartctl -H /dev/disk2` (one level up —
        the APFS *container*, which `diskutil info / `'s own "Part of
        Whole" field names) succeeds and correctly resolves through to
        the physical NVMe controller. So macOS needs a real
        `diskutil info <mount>` call to resolve "Part of Whole" before
        `smartctl` can be pointed at anything — not just the raw
        gopsutil path.
      - **Windows explicitly out of scope for now.** `smartmontools`
        does ship a real Windows package (confirmed:
        `internal/tools.Registry`'s own `Winget: "smartmontools"`
        entry) — so this isn't "the tool doesn't exist there" the way
        `nvtop` is. It's that Windows' own device-addressing syntax
        (`\\.\PhysicalDriveN`, distinct from the `C:`-style drive
        letter gopsutil reports) is completely different from
        macOS/Linux, and this session had no Windows machine to
        validate a resolution path against — same "don't guess at
        unverifiable behavior" discipline this item's own ground rules
        state, applied here rather than shipping a guess.
      - Linux device resolution not yet validated either (no Linux
        machine in this session) — the common case (a plain partition
        device name like `/dev/sda1` or `/dev/nvme0n1p1`) is expected
        to need at most a documented, tested-in-CI regex strip down to
        the whole-disk device, but this is a plan, not yet confirmed
        behavior.

## Exit criteria

Each of `jdupes`/`smartctl` has its open questions above resolved by an
explicit decision (recorded here), implemented, and tested per this
repo's usual gate — before either ships, matching how `jdupes` is
correctly excluded from `vitals dupes`'/the dashboard's default path
until its `ScannedFiles` gap is deliberately resolved, not silently
degraded.
