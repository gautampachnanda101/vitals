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
			p := d.MemUsedPct()
			fmt.Printf("  %s %s / %s  (%s)\n", ui.Key("VRAM       "),
				ui.HumanBytes(int64(d.MemUsedB)), ui.HumanBytes(int64(d.MemTotalB)),
				ui.Grade(fmt.Sprintf("%.0f%%", p), p, 85, 95))
		}
		if d.UtilPct > 0 || d.TempC > 0 {
			fmt.Printf("  %s %s    %s %s\n", ui.Key("Utilisation"),
				ui.Emph(fmt.Sprintf("%.0f%%", d.UtilPct)),
				ui.Key("Temp"), ui.Grade(fmt.Sprintf("%.0f°C", d.TempC), d.TempC, 80, 90))
		}
		if d.PowerLimitW > 0 {
			pp := d.PowerW / d.PowerLimitW * 100
			fmt.Printf("  %s %s / %.0f W\n", ui.Key("Power      "),
				ui.Grade(fmt.Sprintf("%.0f W", d.PowerW), pp, 90, 99), d.PowerLimitW)
		}
		if d.ClockMaxMHz > 0 {
			note := ""
			if d.ClockMHz < 0.9*d.ClockMaxMHz && d.TempC >= 80 {
				note = ui.Actionf("  ← throttling on heat")
			}
			fmt.Printf("  %s %.0f MHz / %.0f MHz%s\n", ui.Key("Clock      "), d.ClockMHz, d.ClockMaxMHz, note)
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
