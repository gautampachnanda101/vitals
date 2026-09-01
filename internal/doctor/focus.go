package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"vitals/internal/clean"
	"vitals/internal/ui"
)

// FocusResources are the deep-dive command names RunFocus accepts.
var FocusResources = []string{"cpu", "mem", "disk", "net", "power"}

// RunFocus is `vitals <resource>` — the current numbers for one resource plus
// only that resource's findings. Exit code follows the findings.
func RunFocus(resource string, opts RunOptions) int {
	snap := Collect(Options{OllamaURL: opts.OllamaURL})
	report := AnalyzeResource(snap, resource)

	var dnsLatency time.Duration
	var dnsErr error
	isNet := resource == "net" || resource == "network"
	if isNet {
		dnsLatency, dnsErr = checkDNSLatency(2 * time.Second)
		if f := analyzeDNSLatency(dnsLatency, dnsErr); f != nil {
			report.Add(*f)
		}
	}

	if err := maybeWriteOutput(opts.Output, snap, report); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write --output file: %v\n", err)
	}

	if opts.Quiet {
		return report.ExitCode()
	}

	if opts.CI {
		fmt.Println(renderCI(report))
		return report.ExitCode()
	}

	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			Resource string `json:"resource"`
			JSONEnvelope
		}{resource, JSONReport(snap, report)})
		return report.ExitCode()
	}

	ui.Header(strings.ToUpper(resource))
	focusDetail(resource, snap)
	if isNet {
		if dnsErr != nil {
			fmt.Printf("  %s\n", ui.Key("DNS lookup: failed ("+dnsErr.Error()+")"))
		} else {
			fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("DNS lookup (%s): %s", dnsProbeHost, dnsLatency.Round(time.Millisecond))))
		}
	}

	if len(report.Findings) == 0 {
		fmt.Println()
		ui.Okf("no %s issue detected", resource)
		return 0
	}
	fmt.Println()
	printFindings(report.SortedBySeverity(), false)
	return report.ExitCode()
}

// pct formats and colour-grades a percentage where higher is worse.
func pct(v, warn, crit float64) string {
	return ui.Grade(fmt.Sprintf("%.0f%%", v), v, warn, crit)
}

func row(label, value string) {
	fmt.Printf("  %s %s\n", ui.Key(fmt.Sprintf("%-10s", label)), value)
}

func focusDetail(resource string, s Snapshot) {
	switch resource {
	case "cpu":
		row("usage", pct(s.CPU.UsedPct, 70, 90)+ui.Key("  (user+sys)"))
		row("io-wait", pct(s.CPU.IOWaitPct, 15, 30))
		if s.CPU.StealPct > 0 {
			row("steal", pct(s.CPU.StealPct, 5, 15))
		}
		loadTxt := fmt.Sprintf("%.2f on %d cores", s.CPU.Load1, s.CPU.Cores)
		if s.CPU.Cores > 0 {
			loadTxt = ui.Grade(loadTxt, s.CPU.Load1, float64(s.CPU.Cores), float64(2*s.CPU.Cores))
		}
		row("load1", loadTxt)
		if lo, hi, ok := coreSpread(s.CPU.PerCorePct); ok {
			row("cores", fmt.Sprintf("busiest %s, next %s, of %d",
				ui.Grade(fmt.Sprintf("%.0f%%", hi), hi, 70, 90),
				ui.Grade(fmt.Sprintf("%.0f%%", lo), lo, 70, 90), len(s.CPU.PerCorePct)))
		}
		if s.CPU.FreqMHz > 0 {
			row("clock", fmt.Sprintf("%.0f MHz", s.CPU.FreqMHz))
		}
		if s.Thermal.CPUTempC > 0 {
			row("package", ui.Grade(fmt.Sprintf("%.0f°C", s.Thermal.CPUTempC), s.Thermal.CPUTempC, 80, 92)+throttleNote(s.Thermal.Throttling))
		}
		if s.CPU.TopProc.Name != "" {
			row("top proc", fmt.Sprintf("%s (pid %d) — %.0f%% CPU", s.CPU.TopProc.Name, s.CPU.TopProc.PID, s.CPU.TopProc.CPUPct))
		}
	case "mem", "memory":
		row("RAM used", pct(s.Memory.UsedPct, 75, 90))
		if s.Memory.AvailablePct > 0 {
			row("available", ui.GradeLow(fmt.Sprintf("%.0f%%", s.Memory.AvailablePct), s.Memory.AvailablePct, 20, 8)+
				ui.Key("  (allocatable before swapping)"))
		}
		row("swap used", pct(s.Memory.SwapUsedPct, 50, 80))
		row("swap in", ui.HumanBytes(int64(s.Memory.SwapInPerSec))+"/s")
		out := ui.HumanBytes(int64(s.Memory.SwapOutPerSec)) + "/s"
		if s.Memory.SwapOutPerSec > 0 {
			out = ui.Grade(out, 1, 0, 0) // any active swap-out is red
		}
		row("swap out", out)
		if s.Memory.TopProc.Name != "" {
			row("top proc", fmt.Sprintf("%s (pid %d) — %s RSS", s.Memory.TopProc.Name, s.Memory.TopProc.PID, ui.HumanBytes(int64(s.Memory.TopProc.RSSBytes))))
		}
	case "disk":
		fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("%-22s %8s %12s %7s %8s %8s %7s", "MOUNT", "USED", "FREE", "UTIL", "AWAIT", "IOPS", "INODES")))
		for _, d := range s.Disks {
			fmt.Printf("  %-22s %8s %12s %7s %8s %8s %7s\n",
				ui.Truncate(d.Mount, 22),
				ui.Grade(fmt.Sprintf("%.0f%%", d.UsedPct), d.UsedPct, 85, 95),
				ui.HumanBytes(int64(d.FreeBytes)),
				ui.Grade(fmt.Sprintf("%.0f%%", d.UtilPct), d.UtilPct, 80, 95),
				ui.Grade(fmt.Sprintf("%.0fms", d.AwaitMS), d.AwaitMS, 20, 50),
				fmt.Sprintf("%.0f", d.IOPS),
				ui.Grade(fmt.Sprintf("%.0f%%", d.InodesUsedPct), d.InodesUsedPct, 85, 95))
			if d.GrowthBytesPerSec > 0 {
				secs := float64(d.FreeBytes) / d.GrowthBytesPerSec
				fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("  filling at %s/hr — %s", ui.HumanBytes(int64(d.GrowthBytesPerSec*3600)), timeToFull(secs))))
			}
		}
		printReclaimable()
	case "net", "network":
		fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("%-12s %12s %12s", "IFACE", "RX/s", "TX/s")))
		shown := 0
		for _, n := range s.Net {
			if n.RxBytesPerSec < 1 && n.TxBytesPerSec < 1 {
				continue
			}
			fmt.Printf("  %-12s %12s %12s\n", n.Name,
				ui.HumanBytes(int64(n.RxBytesPerSec)), ui.HumanBytes(int64(n.TxBytesPerSec)))
			shown++
		}
		if shown == 0 {
			fmt.Println(ui.Key("  (no interface is currently transferring data)"))
		}
		if peers := topRemotePeers(5); len(peers) > 0 {
			fmt.Println()
			fmt.Printf("  %s\n", ui.Key("top remote peers (established TCP):"))
			for _, p := range peers {
				fmt.Printf("    %-30s %d connection(s)\n", p.Host, p.Count)
			}
		}
	case "power", "battery":
		p := s.Power
		src := "AC power"
		if p.OnBattery {
			src = "battery"
		}
		row("source", src)
		if p.Percent > 0 {
			row("charge", ui.GradeLow(fmt.Sprintf("%.0f%%", p.Percent), p.Percent, 20, 8))
		}
		if p.MinutesLeft > 0 {
			row("remaining", fmt.Sprintf("~%d min", p.MinutesLeft))
		}
		if p.DesignCapacityF > 0 {
			row("health", ui.GradeLow(fmt.Sprintf("%.0f%% of design", p.DesignCapacityF*100), p.DesignCapacityF*100, 80, 60))
		}
		if p.LowPowerMode {
			row("low power", ui.Key("ON")+"  (CPU/GPU clocks capped — this, not a problem, explains reduced performance)")
		}
		if p.Percent == 0 && !p.OnBattery {
			fmt.Println(ui.Key("  (no battery detected)"))
		}
	}
}

func throttleNote(throttling bool) string {
	if throttling {
		return ui.Actionf("  ← throttling")
	}
	return ""
}

// printReclaimable measures the same cache/log/trash directories `vitals
// clean` targets and prints what's actually reclaimable — the concrete answer
// to "what can I delete", not just a free-space percentage. Bounded to a short
// budget so a huge cache can't stall an interactive command.
func printReclaimable() {
	entries, complete := clean.ReclaimableSummary(1500 * time.Millisecond)
	var total int64
	for _, e := range entries {
		total += e.Bytes
	}
	if total == 0 {
		return
	}
	fmt.Println()
	suffix := ""
	if !complete {
		suffix = ui.Key(" (partial scan — actual total is at least this much)")
	}
	fmt.Printf("  %s %s%s\n", ui.Key("reclaimable (caches/logs/trash):"), ui.Actionf("%s", ui.HumanBytes(total)), suffix)
	shown := 0
	for _, e := range entries {
		if shown >= 5 {
			break
		}
		fmt.Printf("    %8s  %s\n", ui.HumanBytes(e.Bytes), e.Path)
		shown++
	}
	fmt.Printf("  %s `vitals clean --dry-run` to see the full plan, then apply\n", ui.Actionf("→"))
}
