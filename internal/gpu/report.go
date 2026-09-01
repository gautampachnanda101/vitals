package gpu

import (
	"encoding/json"
	"fmt"
	"os"

	"vitals/internal/ui"
)

// Run prints a GPU telemetry report, or JSON with --json. It returns nil even
// when no GPU is found — absence is information, not an error.
func Run(asJSON bool) error {
	devs := Probe()

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Devices []Device `json:"devices"`
		}{devs})
	}

	ui.Header("GPU TELEMETRY")
	if len(devs) == 0 {
		ui.Warnf("no GPU tooling found (nvidia-smi / rocm-smi / ioreg)")
		fmt.Println("  For a live per-process view, install nvtop.")
		return nil
	}

	for _, d := range devs {
		fmt.Printf("\n%s[%d] %s%s  %s(%s)%s\n", ui.Bold, d.Index, d.Name, ui.Reset, ui.Dim, d.Vendor, ui.Reset)
		if d.MemTotalB > 0 {
			fmt.Printf("  VRAM        %s / %s  (%.0f%%)\n",
				ui.HumanBytes(int64(d.MemUsedB)), ui.HumanBytes(int64(d.MemTotalB)), d.MemUsedPct())
		}
		if d.UtilPct > 0 || d.TempC > 0 {
			fmt.Printf("  Utilisation %.0f%%    Temp %.0f°C\n", d.UtilPct, d.TempC)
		}
		if d.PowerLimitW > 0 {
			fmt.Printf("  Power       %.0f W / %.0f W\n", d.PowerW, d.PowerLimitW)
		}
		if d.ClockMaxMHz > 0 {
			note := ""
			if d.ClockMHz < 0.9*d.ClockMaxMHz && d.TempC >= 80 {
				note = ui.Actionf("  ← throttling on heat")
			}
			fmt.Printf("  Clock       %.0f MHz / %.0f MHz%s\n", d.ClockMHz, d.ClockMaxMHz, note)
		}
		if len(d.Processes) > 0 {
			fmt.Printf("  %sProcesses holding VRAM%s\n", ui.Bold, ui.Reset)
			for _, p := range d.Processes {
				fmt.Printf("    %-8d %-24s %s\n", p.PID, ui.Truncate(p.Name, 24), ui.HumanBytes(int64(p.MemUseB)))
			}
		}
	}
	fmt.Printf("\n%sComplement with nvtop for a live per-process view.%s\n", ui.Dim, ui.Reset)
	return nil
}
