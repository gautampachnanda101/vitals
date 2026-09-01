package doctor

import (
	"fmt"
	"strings"

	gdisk "github.com/shirou/gopsutil/v4/disk"

	"vitals/internal/clean"
	"vitals/internal/ui"
)

// reclaimableLines formats entries (already sorted largest-first by
// clean.ReclaimableSummary) as "  size  path" lines, capped at limit — 0 or
// negative means unlimited, the --verbose case. remaining is how many
// entries were left out by the cap, 0 when nothing was.
func reclaimableLines(entries []clean.CacheEntry, limit int) (lines []string, shown, remaining int) {
	n := len(entries)
	if limit > 0 && limit < n {
		n = limit
	}
	for _, e := range entries[:n] {
		lines = append(lines, fmt.Sprintf("    %8s  %s", ui.HumanBytes(e.Bytes), e.Path))
	}
	return lines, n, len(entries) - n
}

// excludedMount is one partition the default disk table's filter dropped.
type excludedMount struct {
	Mountpoint string
	Fstype     string
	TotalBytes uint64
	Reason     string
}

// formatExcludedMount renders one excludedMount as a display line.
func formatExcludedMount(m excludedMount) string {
	return fmt.Sprintf("%-30s %-8s %10s  %s", m.Mountpoint, m.Fstype, ui.HumanBytes(int64(m.TotalBytes)), m.Reason)
}

// listExcludedMounts enumerates every partition disk.Partitions(true)
// reports and were NOT shown in the main table, with why. Live (uses the
// same timeout-guarded diskUsage as collectDisks, so a dead network mount
// can't hang this either) — --verbose only, since walking every partition
// including ones the default view intentionally hides is exactly the extra
// detail --verbose exists for.
func listExcludedMounts() []excludedMount {
	parts, err := gdisk.Partitions(true)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []excludedMount
	for _, p := range parts {
		if seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		u, ok := diskUsage(p.Mountpoint)
		if !ok || u.Total == 0 {
			continue
		}
		reason := filesystemFilterReason(p.Fstype, p.Mountpoint, u.Total)
		if reason == "" {
			continue // shown in the main table already
		}
		out = append(out, excludedMount{Mountpoint: p.Mountpoint, Fstype: p.Fstype, TotalBytes: u.Total, Reason: reason})
	}
	return out
}

// printExcludedMounts prints every excluded mount the default disk view
// silently drops — --verbose's answer to "why don't I see mount X".
func printExcludedMounts() {
	excluded := listExcludedMounts()
	if len(excluded) == 0 {
		return
	}
	fmt.Println()
	fmt.Println(ui.Key("  filtered out (pseudo/system/too-small — not shown above):"))
	for _, m := range excluded {
		fmt.Printf("    %s\n", ui.Key(formatExcludedMount(m)))
	}
}

// allCoresLine renders every core's usage, graded, as "1: NN%  2: NN% ...".
// Used by `cpu --verbose` — the default view only shows the busiest core
// and its runner-up, enough to spot a single-thread bottleneck but not to
// see the whole machine's per-core spread at a glance.
func allCoresLine(cores []float64) string {
	if len(cores) == 0 {
		return ""
	}
	parts := make([]string, len(cores))
	for i, v := range cores {
		parts[i] = fmt.Sprintf("%d: %s", i+1, ui.Grade(fmt.Sprintf("%.0f%%", v), v, 70, 90))
	}
	return strings.Join(parts, "  ")
}
