package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"vitals/internal/diag"
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
	for _, f := range report.SortedBySeverity() {
		switch f.Severity {
		case diag.Critical:
			ui.Errf("%s", f.Title)
		case diag.Warn:
			ui.Warnf("%s", f.Title)
		default:
			ui.Okf("%s", f.Title)
		}
		if f.Detail != "" {
			fmt.Printf("     %s\n", f.Detail)
		}
		for _, fix := range f.Fixes {
			fmt.Printf("     %s %s\n", ui.Actionf("→"), fix)
		}
	}
	return report.ExitCode()
}

func focusDetail(resource string, s Snapshot) {
	switch resource {
	case "cpu":
		fmt.Printf("  usage      %.0f%%   (user+sys)\n", s.CPU.UsedPct)
		fmt.Printf("  io-wait    %.0f%%\n", s.CPU.IOWaitPct)
		if s.CPU.StealPct > 0 {
			fmt.Printf("  steal      %.0f%%\n", s.CPU.StealPct)
		}
		fmt.Printf("  load1      %.2f on %d cores\n", s.CPU.Load1, s.CPU.Cores)
		if s.CPU.FreqMHz > 0 {
			fmt.Printf("  clock      %.0f MHz\n", s.CPU.FreqMHz)
		}
		if s.Thermal.CPUTempC > 0 {
			fmt.Printf("  package    %.0f°C%s\n", s.Thermal.CPUTempC, throttleNote(s.Thermal.Throttling))
		}
	case "mem", "memory":
		fmt.Printf("  RAM used   %.0f%%\n", s.Memory.UsedPct)
		fmt.Printf("  swap used  %.0f%%\n", s.Memory.SwapUsedPct)
		fmt.Printf("  swap in    %s/s\n", ui.HumanBytes(int64(s.Memory.SwapInPerSec)))
		fmt.Printf("  swap out   %s/s\n", ui.HumanBytes(int64(s.Memory.SwapOutPerSec)))
	case "disk":
		fmt.Printf("  %-22s %8s %12s %7s %8s\n", "MOUNT", "USED", "FREE", "UTIL", "AWAIT")
		for _, d := range s.Disks {
			fmt.Printf("  %-22s %7.0f%% %12s %6.0f%% %6.0fms\n",
				ui.Truncate(d.Mount, 22), d.UsedPct,
				ui.HumanBytes(int64(d.FreeBytes)), d.UtilPct, d.AwaitMS)
		}
	case "net", "network":
		fmt.Printf("  %-12s %12s %12s\n", "IFACE", "RX/s", "TX/s")
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
			fmt.Println("  (no interface is currently transferring data)")
		}
	case "power", "battery":
		p := s.Power
		src := "AC power"
		if p.OnBattery {
			src = "battery"
		}
		fmt.Printf("  source     %s\n", src)
		if p.Percent > 0 {
			fmt.Printf("  charge     %.0f%%\n", p.Percent)
		}
		if p.MinutesLeft > 0 {
			fmt.Printf("  remaining  ~%d min\n", p.MinutesLeft)
		}
		if p.DesignCapacityF > 0 {
			fmt.Printf("  health     %.0f%% of design capacity\n", p.DesignCapacityF*100)
		}
		if p.Percent == 0 && !p.OnBattery {
			fmt.Println("  (no battery detected)")
		}
	}
}

func throttleNote(throttling bool) string {
	if throttling {
		return ui.Actionf("  ← throttling")
	}
	return ""
}
