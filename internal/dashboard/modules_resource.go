package dashboard

import (
	"fmt"
	"strings"

	"vitals/internal/doctor"
	"vitals/internal/ui"
)

func init() {
	Register(Module{Slug: "cpu", NavLabel: "CPU", Order: 10, Available: Always, Render: resourcePage("cpu", renderCPU)})
	Register(Module{Slug: "mem", NavLabel: "Memory", Order: 20, Available: Always, Render: resourcePage("mem", renderMem)})
	Register(Module{Slug: "disk", NavLabel: "Disk", Order: 30, Available: Always, Render: resourcePage("disk", renderDisk)})
	Register(Module{Slug: "net", NavLabel: "Network", Order: 40, Available: Always, Render: resourcePage("net", renderNet)})
	Register(Module{Slug: "power", NavLabel: "Power", Order: 50, Available: HasBattery, UnavailableReason: "no battery detected", Render: resourcePage("power", renderPower)})
	Register(Module{Slug: "gpu", NavLabel: "GPU", Order: 60, Available: HasGPU, UnavailableReason: "no GPU detected", Render: resourcePage("gpu", renderGPU)})
}

// resourcePage wraps a resource-specific renderer with the verdict banner
// every resource page shares — doctor.AnalyzeResource, not doctor.Analyze,
// so a resource page shows only its own findings, matching what
// `vitals cpu|mem|disk|net|power` prints in a terminal.
func resourcePage(resource string, body func(doctor.Snapshot) string) func(PageContext) string {
	return func(ctx PageContext) string {
		report := doctor.AnalyzeResource(ctx.Snapshot, resource)
		headline := reportHeadline(report, "No issues found")
		out := verdictBanner(headline, "", report.Worst())
		out += `<div class="card">` + body(ctx.Snapshot) + `</div>`
		out += `<div class="card">` + findingsList(report.SortedBySeverity()) + `</div>`
		return out
	}
}

func renderCPU(s doctor.Snapshot) string {
	out := row("Usage", fmt.Sprintf("%.0f%%", s.CPU.UsedPct))
	out += row("I/O wait", fmt.Sprintf("%.0f%%", s.CPU.IOWaitPct))
	out += row("Load (1 min)", fmt.Sprintf("%.2f on %d cores", s.CPU.Load1, s.CPU.Cores))
	if s.CPU.FreqMHz > 0 {
		out += row("Clock", fmt.Sprintf("%.0f MHz", s.CPU.FreqMHz))
	}
	if s.CPU.TopProc.Name != "" {
		out += row("Top process", fmt.Sprintf("%s (pid %d) — %.0f%%", s.CPU.TopProc.Name, s.CPU.TopProc.PID, s.CPU.TopProc.CPUPct))
	}
	return out
}

func renderMem(s doctor.Snapshot) string {
	out := row("RAM used", fmt.Sprintf("%.0f%%", s.Memory.UsedPct))
	if s.Memory.AvailablePct > 0 {
		out += row("Available", fmt.Sprintf("%.0f%%", s.Memory.AvailablePct))
	}
	out += row("Swap used", fmt.Sprintf("%.0f%%", s.Memory.SwapUsedPct))
	if s.Memory.TopProc.Name != "" {
		out += row("Top process", fmt.Sprintf("%s (pid %d) — %s RSS", s.Memory.TopProc.Name, s.Memory.TopProc.PID, ui.HumanBytes(int64(s.Memory.TopProc.RSSBytes))))
	}
	return out
}

func renderDisk(s doctor.Snapshot) string {
	if len(s.Disks) == 0 {
		return `<p class="unavailable">No disks reported.</p>`
	}
	var out string
	for _, d := range s.Disks {
		out += row(d.Mount, fmt.Sprintf("%.0f%% used, %s free", d.UsedPct, ui.HumanBytes(int64(d.FreeBytes))))
	}
	return out
}

func renderNet(s doctor.Snapshot) string {
	shown := 0
	var out string
	for _, n := range s.Net {
		if n.RxBytesPerSec < 1 && n.TxBytesPerSec < 1 {
			continue
		}
		out += row(n.Name, fmt.Sprintf("↓ %s/s   ↑ %s/s", ui.HumanBytes(int64(n.RxBytesPerSec)), ui.HumanBytes(int64(n.TxBytesPerSec))))
		shown++
	}
	if shown == 0 {
		return `<p class="unavailable">No interface is currently transferring data.</p>`
	}
	return out
}

func renderPower(s doctor.Snapshot) string {
	src := "AC power"
	if s.Power.OnBattery {
		src = "battery"
	}
	out := row("Source", src)
	if s.Power.Percent > 0 {
		out += row("Charge", fmt.Sprintf("%.0f%%", s.Power.Percent))
	}
	if s.Power.MinutesLeft > 0 {
		out += row("Remaining", fmt.Sprintf("~%d min", s.Power.MinutesLeft))
	}
	return out
}

// renderGPU mirrors internal/gpu/report.go's Run: it never prints a
// telemetry field vitals didn't actually get a real reading for. A bare
// "0% util, 0 B / 0 B VRAM" for a GPU with no discrete VRAM reading read
// as broken telemetry to a real user, not as "nothing separate to report."
//
// On Apple Silicon there's no separate VRAM pool to be missing in the
// first place — GPU and RAM are the same physical memory (see
// internal/gpu/gpu.go's parseIORegApple) — so "go check the Memory page"
// is a dead end, not an answer: this page shows that same live pressure
// directly, the same numbers/format renderMem uses, because for this GPU
// they ARE the GPU numbers.
func renderGPU(s doctor.Snapshot) string {
	if len(s.GPUs) == 0 {
		return `<p class="unavailable">No GPU detected.</p>`
	}
	var out string
	for _, g := range s.GPUs {
		switch {
		case g.VRAMTotal > 0:
			out += row(g.Name, fmt.Sprintf("%.0f%% util, %s / %s VRAM", g.UtilPct, ui.HumanBytes(int64(g.VRAMUsed)), ui.HumanBytes(int64(g.VRAMTotal))))
		case strings.HasPrefix(g.Name, "Apple"):
			out += row(g.Name, "unified memory — same pool as system RAM, shown below")
			out += renderMem(s)
		default:
			out += row(g.Name, "no utilisation/VRAM telemetry available for this GPU")
		}
	}
	return out
}
