package doctor

import (
	"fmt"
	"sort"

	"vitals/internal/monitor"
	"vitals/internal/ui"
)

// diskIOSampler is the process-table sample printDiskIOProcs reads from,
// pulled out so a test drives it with a canned slice instead of the real
// machine. Production wires monitor.Sample.
var diskIOSampler = func(top int) ([]monitor.ProcInfo, error) {
	snap, err := monitor.Sample(monitor.Options{Top: top, SortBy: "cpu"})
	if err != nil {
		return nil, err
	}
	return snap.Processes, nil
}

// printDiskIOProcs adds a "top processes by disk I/O" table to `vitals
// disk`, ranked by the live per-process read+write rate monitor.Sample
// measures over its sample window. It prints nothing when no process has
// a non-zero rate — that is the normal case on macOS, where gopsutil has
// no per-process I/O counter, so the section stays absent rather than
// showing a table of zeros or a CPU ranking mislabelled as disk activity
// (roadmap item 012).
func printDiskIOProcs(verbose bool) {
	limit := 5
	captureTop := 200
	if verbose {
		limit = 15
		captureTop = 500
	}
	procs, err := diskIOSampler(captureTop)
	if err != nil || !monitor.HasDiskIORates(procs) {
		return
	}
	ranked := append([]monitor.ProcInfo(nil), procs...)
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].DiskReadBytesPerSec+ranked[i].DiskWriteBytesPerSec >
			ranked[j].DiskReadBytesPerSec+ranked[j].DiskWriteBytesPerSec
	})

	fmt.Println()
	fmt.Printf("  %s\n", ui.Key("top processes by disk I/O (this sample):"))
	fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("%-24s %12s %12s %8s", "PROCESS", "READ/s", "WRITE/s", "PID")))
	shown := 0
	for _, p := range ranked {
		if p.DiskReadBytesPerSec+p.DiskWriteBytesPerSec <= 0 {
			break
		}
		fmt.Printf("  %-24s %12s %12s %8d\n",
			ui.Truncate(p.Name, 24),
			ui.HumanBytes(int64(p.DiskReadBytesPerSec)),
			ui.HumanBytes(int64(p.DiskWriteBytesPerSec)),
			p.PID)
		if shown++; shown >= limit {
			break
		}
	}
}
