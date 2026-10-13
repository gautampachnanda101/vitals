package doctor

import (
	"fmt"
	"strings"

	"vitals/internal/diskusage"
	"vitals/internal/ui"
)

func printDiskUsage(result diskusage.Result, scanErr error) {
	fmt.Printf("\n  Read-only disk usage: %s\n", diskusage.DisplayPath(result.Root))
	status := "Complete"
	if !result.Complete {
		status = "Partial - largest observed; directory bytes may be lower bounds"
	}
	if scanErr != nil {
		fmt.Printf("  Scan failed: %s; choose an accessible real directory with --root.\n", diskusage.DisplayPath(scanErr.Error()))
	}
	fmt.Printf("  %s; %d entries, %.2fs, %s observed logical bytes\n",
		status, result.Entries, float64(result.ElapsedMS)/1000, ui.HumanBytes(result.ObservedBytes))
	if len(result.StopReasons) > 0 {
		fmt.Printf("  Stopped: %s. Narrow --root or increase --scan-budget (up to 5m).\n",
			diskusage.DisplayPath(strings.Join(result.StopReasons, ", ")))
	}
	printUsageRows("Biggest directories (recursive)", result.Directories)
	printUsageRows("Files directly in scan root", result.RootFiles)
	printUsageRows("Biggest single files", result.Files)
	if result.Complete && result.ObservedBytes == 0 {
		fmt.Println("  No regular-file bytes found in this scope; choose another --root to investigate elsewhere.")
	}
	fmt.Printf("  Skipped: %d symlinks, %d special files, %d nested mounts; %d access errors.\n",
		result.SkippedSymlinks, result.SkippedSpecial, result.SkippedMounts, result.ErrorCount)
	for _, issue := range result.Errors {
		fmt.Printf("    %s: %s\n", diskusage.DisplayPath(issue.Path), diskusage.DisplayPath(issue.Message))
	}
	if !result.BoundaryDetectionAvailable {
		fmt.Println("  Mount-boundary detection unavailable; this is a path-tree total, not a filesystem-exclusive total.")
	}
	fmt.Println("  Drill down: vitals disk --scan --root PATH (quote the chosen directory in your shell).")
	fmt.Println("  Logical sizes are not guaranteed reclaim: hardlinks, sparse files, clones and snapshots differ.")
	fmt.Println("  Scope excludes unvisited system data, other users and snapshots; do not add directory and file rows.")
	fmt.Println("  Caches: vitals clean --dry-run. Models/VMs/personal files: review in the owning application, not direct blob deletion. See vitals guide.")
}

func printUsageRows(title string, rows []diskusage.Row) {
	if len(rows) == 0 {
		return
	}
	fmt.Printf("\n  %s\n", title)
	for _, row := range rows {
		qualifier := ""
		if !row.Complete {
			qualifier = " (at least; incomplete)"
		}
		fmt.Printf("    %12s  %s%s\n", ui.HumanBytes(row.Bytes), diskusage.DisplayPath(row.Path), qualifier)
	}
}
