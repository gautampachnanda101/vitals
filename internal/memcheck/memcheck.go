// Package memcheck prints an advanced memory / pressure overview using gopsutil
// only (no shelling out to vm_stat, free, or systeminfo), so it is fully
// cross-platform and self-contained.
package memcheck

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"vitals/internal/ui"
)

// Run prints the report.
func Run() error {
	ui.Header("1. MEMORY & PRESSURE OVERVIEW")

	if hi, err := host.Info(); err == nil {
		fmt.Printf("  Host      : %s (%s %s, %s)\n", hi.Hostname, hi.Platform, hi.PlatformVersion, hi.KernelArch)
		fmt.Printf("  Uptime    : %s\n", (time.Duration(hi.Uptime) * time.Second).Round(time.Minute))
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		return fmt.Errorf("read virtual memory: %w", err)
	}

	fmt.Printf("\n  %-28s %12s\n", "Total physical RAM:", ui.HumanBytes(int64(vm.Total)))
	fmt.Printf("  %-28s %12s  (%.1f%%)\n", "Used:", ui.HumanBytes(int64(vm.Used)), vm.UsedPercent)
	fmt.Printf("  %-28s %12s\n", "Available:", ui.HumanBytes(int64(vm.Available)))
	fmt.Printf("  %-28s %12s\n", "Free (unallocated):", ui.HumanBytes(int64(vm.Free)))

	fmt.Printf("\n%sDetailed breakdown:%s\n", ui.Bold, ui.Reset)
	printIf("Active", vm.Active)
	printIf("Inactive (reclaimable)", vm.Inactive)
	printIf("Wired / kernel", vm.Wired)
	printIf("Cached", vm.Cached)
	printIf("Buffers", vm.Buffers)
	printIf("Shared", vm.Shared)
	printIf("Slab", vm.Slab)

	ui.Header("2. SWAP / COMPRESSION")
	sw, err := mem.SwapMemory()
	if err != nil {
		ui.Warnf("swap stats unavailable: %v", err)
	} else {
		fmt.Printf("  %-28s %12s\n", "Swap total:", ui.HumanBytes(int64(sw.Total)))
		fmt.Printf("  %-28s %12s  (%.1f%%)\n", "Swap used:", ui.HumanBytes(int64(sw.Used)), sw.UsedPercent)
		if sw.Sin > 0 || sw.Sout > 0 {
			fmt.Printf("  %-28s %12s\n", "Cumulative swap-in:", ui.HumanBytes(int64(sw.Sin)))
			fmt.Printf("  %-28s %12s\n", "Cumulative swap-out:", ui.HumanBytes(int64(sw.Sout)))
		}
	}
	if sd, err := mem.SwapDevices(); err == nil && len(sd) > 0 {
		for _, d := range sd {
			fmt.Printf("  device %s: %s used of %s\n", d.Name,
				ui.HumanBytes(int64(d.UsedBytes)), ui.HumanBytes(int64(d.UsedBytes+d.FreeBytes)))
		}
	}

	ui.Header("3. DIAGNOSTIC VERDICT")
	verdict(vm, sw)
	return nil
}

func printIf(label string, v uint64) {
	if v == 0 {
		return
	}
	fmt.Printf("  %-28s %12s\n", label+":", ui.HumanBytes(int64(v)))
}

func verdict(vm *mem.VirtualMemoryStat, sw *mem.SwapMemoryStat) {
	switch {
	case vm.UsedPercent >= 90:
		ui.Errf("CRITICAL: %.1f%% RAM in use — active set exceeds comfortable capacity", vm.UsedPercent)
	case vm.UsedPercent >= 75:
		ui.Warnf("ELEVATED: %.1f%% RAM in use — close idle apps or add swap headroom", vm.UsedPercent)
	default:
		ui.Okf("HEALTHY: %.1f%% RAM in use", vm.UsedPercent)
	}

	if sw != nil && sw.Total > 0 {
		switch {
		case sw.UsedPercent >= 50:
			ui.Errf("CRITICAL: swap %.1f%% full — expect paging stalls and latency spikes", sw.UsedPercent)
		case sw.UsedPercent >= 15:
			ui.Warnf("WARNING: swap %.1f%% full — memory pressure is spilling to disk", sw.UsedPercent)
		default:
			ui.Okf("swap allocation is healthy (%.1f%%)", sw.UsedPercent)
		}
	}

	fmt.Println()
	fmt.Println("  Complement this snapshot with an established live monitor:")
	fmt.Println("    • htop / btop        — interactive process + memory view")
	fmt.Println("    • glances            — cross-platform system dashboard")
	fmt.Println("    • vm_stat 1 (macOS)  — live page + compressor counters")
	fmt.Println("    • free -m -s 1 (Linux)")
}
