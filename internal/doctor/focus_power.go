package doctor

import (
	"fmt"

	"vitals/internal/power"
	"vitals/internal/ui"
)

// powerSampler is the per-process energy source printPowerProcs reads
// from, pulled out so a test drives it without shelling out to `top`.
var powerSampler = power.Sample

// printPowerProcs adds a real per-process energy table to `vitals power`
// on macOS, where `top` exposes the same power score Activity Monitor
// shows without sudo. It prints nothing on Linux/Windows (no
// no-privilege per-process energy source) or if the probe fails —
// roadmap item 012.
func printPowerProcs(verbose bool) {
	limit := 5
	if verbose {
		limit = 15
	}
	procs, ok := powerSampler(limit)
	if !ok || len(procs) == 0 {
		return
	}
	fmt.Println()
	fmt.Printf("  %s\n", ui.Key("energy impact by process (macOS power score, not watts):"))
	fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("%-26s %10s %8s", "PROCESS", "IMPACT", "PID")))
	for _, p := range procs {
		fmt.Printf("  %-26s %10.1f %8d\n", ui.Truncate(p.Name, 26), p.Power, p.PID)
	}
}
