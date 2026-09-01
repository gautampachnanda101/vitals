// Package memcheck prints an advanced memory / pressure overview using gopsutil
// only (no shelling out to vm_stat, free, or systeminfo), so it is fully
// cross-platform and self-contained.
package memcheck

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"vitals/internal/diag"
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

	fmt.Printf("\n  %s %12s\n", ui.Key(fmt.Sprintf("%-28s", "Total physical RAM:")), ui.HumanBytes(int64(vm.Total)))
	fmt.Printf("  %s %12s  (%s)\n", ui.Key(fmt.Sprintf("%-28s", "Used:")), ui.HumanBytes(int64(vm.Used)),
		ui.Grade(fmt.Sprintf("%.1f%%", vm.UsedPercent), vm.UsedPercent, 75, 90))
	fmt.Printf("  %s %12s\n", ui.Key(fmt.Sprintf("%-28s", "Available:")), ui.HumanBytes(int64(vm.Available)))
	fmt.Printf("  %s %12s\n", ui.Key(fmt.Sprintf("%-28s", "Free (unallocated):")), ui.HumanBytes(int64(vm.Free)))

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
		fmt.Printf("  %s %12s\n", ui.Key(fmt.Sprintf("%-28s", "Swap total:")), ui.HumanBytes(int64(sw.Total)))
		fmt.Printf("  %s %12s  (%s)\n", ui.Key(fmt.Sprintf("%-28s", "Swap used:")), ui.HumanBytes(int64(sw.Used)),
			ui.Grade(fmt.Sprintf("%.1f%%", sw.UsedPercent), sw.UsedPercent, 50, 80))
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

// memVerdict turns a RAM + swap reading into ranked findings. Pure: it takes
// only the stat structs, so it is exercised entirely from fixtures.
func memVerdict(vm *mem.VirtualMemoryStat, sw *mem.SwapMemoryStat) diag.Report {
	var r diag.Report

	switch {
	case vm.UsedPercent >= 90:
		r.Add(diag.Finding{
			Severity: diag.Critical,
			Title:    "RAM near capacity",
			Detail:   fmt.Sprintf("%.1f%% of physical RAM in use — the active set exceeds comfortable capacity", vm.UsedPercent),
			Fixes:    []string{"close idle apps", "run `vitals memhogs` to see the biggest consumers"},
		})
	case vm.UsedPercent >= 75:
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "RAM elevated",
			Detail:   fmt.Sprintf("%.1f%% of physical RAM in use", vm.UsedPercent),
			Fixes:    []string{"close idle apps", "add swap headroom"},
		})
	default:
		r.Add(diag.Finding{
			Severity: diag.OK,
			Title:    "RAM healthy",
			Detail:   fmt.Sprintf("%.1f%% of physical RAM in use", vm.UsedPercent),
		})
	}

	if sw != nil && sw.Total > 0 {
		switch {
		case sw.UsedPercent >= 50:
			r.Add(diag.Finding{
				Severity: diag.Critical,
				Title:    "Swap nearly exhausted",
				Detail:   fmt.Sprintf("swap %.1f%% full — expect paging stalls and latency spikes", sw.UsedPercent),
				Fixes:    []string{"free RAM (quit apps, `vitals memhogs`)", "on macOS: `sudo purge`", "reboot if it stays pinned"},
			})
		case sw.UsedPercent >= 15:
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    "Swap in use",
				Detail:   fmt.Sprintf("swap %.1f%% full — memory pressure is spilling to disk", sw.UsedPercent),
				Fixes:    []string{"close memory-heavy apps before it worsens"},
			})
		default:
			r.Add(diag.Finding{
				Severity: diag.OK,
				Title:    "Swap healthy",
				Detail:   fmt.Sprintf("swap %.1f%% full", sw.UsedPercent),
			})
		}
	}

	return r
}

func verdict(vm *mem.VirtualMemoryStat, sw *mem.SwapMemoryStat) {
	rep := memVerdict(vm, sw)
	for _, f := range rep.SortedBySeverity() {
		line := f.Title
		if f.Detail != "" {
			line += " — " + f.Detail
		}
		switch f.Severity {
		case diag.Critical:
			ui.Errf("%s", line)
		case diag.Warn:
			ui.Warnf("%s", line)
		default:
			ui.Okf("%s", line)
		}
		for _, fix := range f.Fixes {
			fmt.Printf("      %s %s\n", ui.Actionf("→"), fix)
		}
	}

	fmt.Println()
	fmt.Println("  Complement this snapshot with an established live monitor:")
	fmt.Println("    • htop / btop        — interactive process + memory view")
	fmt.Println("    • glances            — cross-platform system dashboard")
	fmt.Println("    • vm_stat 1 (macOS)  — live page + compressor counters")
	fmt.Println("    • free -m -s 1 (Linux)")
}
