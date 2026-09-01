package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"vitals/internal/ui"
)

// FocusResources are the deep-dive command names RunFocus accepts.
var FocusResources = []string{"cpu", "mem", "disk", "net", "power"}

// RunFocus is `vitals <resource>` — the current numbers for one resource plus
// only that resource's findings. Exit code follows the findings.
func RunFocus(resource string, opts RunOptions) int {
	snap := Collect(Options{OllamaURL: opts.OllamaURL})
	report := AnalyzeResource(snap, resource)

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
		if s.CPU.FreqMHz > 0 {
			row("clock", fmt.Sprintf("%.0f MHz", s.CPU.FreqMHz))
		}
		if s.Thermal.CPUTempC > 0 {
			row("package", ui.Grade(fmt.Sprintf("%.0f°C", s.Thermal.CPUTempC), s.Thermal.CPUTempC, 80, 92)+throttleNote(s.Thermal.Throttling))
		}
	case "mem", "memory":
		row("RAM used", pct(s.Memory.UsedPct, 75, 90))
		row("swap used", pct(s.Memory.SwapUsedPct, 50, 80))
		row("swap in", ui.HumanBytes(int64(s.Memory.SwapInPerSec))+"/s")
		out := ui.HumanBytes(int64(s.Memory.SwapOutPerSec)) + "/s"
		if s.Memory.SwapOutPerSec > 0 {
			out = ui.Grade(out, 1, 0, 0) // any active swap-out is red
		}
		row("swap out", out)
	case "disk":
		fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("%-22s %8s %12s %7s %8s", "MOUNT", "USED", "FREE", "UTIL", "AWAIT")))
		for _, d := range s.Disks {
			fmt.Printf("  %-22s %8s %12s %7s %8s\n",
				ui.Truncate(d.Mount, 22),
				ui.Grade(fmt.Sprintf("%.0f%%", d.UsedPct), d.UsedPct, 85, 95),
				ui.HumanBytes(int64(d.FreeBytes)),
				ui.Grade(fmt.Sprintf("%.0f%%", d.UtilPct), d.UtilPct, 80, 95),
				ui.Grade(fmt.Sprintf("%.0fms", d.AwaitMS), d.AwaitMS, 20, 50))
		}
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
