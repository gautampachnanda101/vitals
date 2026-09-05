package dashboard

import (
	"fmt"
	"html/template"
	"sort"
	"strings"

	"vitals/internal/doctor"
	"vitals/internal/ui"
)

func init() {
	Register(Module{Slug: "cpu", NavLabel: "CPU", Group: "Resources", Icon: iconCPU, Order: 10, Available: Always, Render: resourcePage("cpu", renderCPU)})
	Register(Module{Slug: "mem", NavLabel: "Memory", Group: "Resources", Icon: iconMemory, Order: 20, Available: Always, Render: resourcePage("mem", renderMem)})
	Register(Module{Slug: "disk", NavLabel: "Disk", Group: "Resources", Icon: iconDisk, Order: 30, Available: Always, Render: resourcePage("disk", renderDisk)})
	Register(Module{Slug: "net", NavLabel: "Network", Group: "Resources", Icon: iconNetwork, Order: 40, Available: Always, Render: resourcePage("net", renderNet)})
	Register(Module{Slug: "power", NavLabel: "Power", Group: "Resources", Icon: iconPower, Order: 50, Available: HasBattery, UnavailableReason: "no battery detected", Render: resourcePage("power", renderPower)})
	Register(Module{Slug: "gpu", NavLabel: "GPU", Group: "Resources", Icon: iconGPU, Order: 60, Available: HasGPU, UnavailableReason: "no GPU detected", Render: resourcePage("gpu", renderGPU)})
}

// resourcePage wraps a resource-specific renderer with the verdict banner
// every resource page shares — doctor.AnalyzeResource, not doctor.Analyze,
// so a resource page shows only its own findings, matching what
// `vitals cpu|mem|disk|net|power` prints in a terminal. body owns its
// own card wrapping (possibly more than one — see renderCPU/renderMem's
// own "Top processes" section) rather than resourcePage wrapping a
// single implicit card around it, so a render function can add its own
// extra sections without a fragile close-the-caller's-div trick.
func resourcePage(resource string, body func(doctor.Snapshot) string) func(PageContext) string {
	return func(ctx PageContext) string {
		report := doctor.AnalyzeResource(ctx.Snapshot, resource)
		headline := reportHeadline(report, "No issues found")
		out := verdictBanner(headline, "", report.Worst())
		out += body(ctx.Snapshot)
		out += findingsCard(report.SortedBySeverity())
		return out
	}
}

// card wraps body's rows in a .card — the one wrapping every render*
// function below now does for itself (see resourcePage's own comment
// for why the wrapping moved here from the caller).
func card(body string) string { return `<div class="card">` + body + `</div>` }

// topProcessesSectionN is how many rows a resource page's own "Top
// processes" section shows — smaller than the dedicated Processes
// page's own display cap (processesDisplayTop, modules_processes.go):
// this is a supporting detail on a page about something else, not the
// page's whole point. Matches the user-facing ask for "top 5/10" — 5 is
// the value chosen; raise it if that turns out too tight in practice.
const topProcessesSectionN = 5

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
	out = card(out)
	if top := topProcessRows(topProcessesSectionN, false); top != "" {
		out += `<div class="sectiontitle">Top processes by CPU</div>` + card(top)
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
	out = card(out)
	if top := topProcessRows(topProcessesSectionN, true); top != "" {
		out += `<div class="sectiontitle">Top processes by memory</div>` + card(top)
	}
	return out
}

func renderDisk(s doctor.Snapshot) string {
	if len(s.Disks) == 0 {
		return card(`<p class="unavailable">No disks reported.</p>`)
	}
	var out string
	for _, d := range s.Disks {
		out += row(d.Mount, fmt.Sprintf("%.0f%% used, %s free", d.UsedPct, ui.HumanBytes(int64(d.FreeBytes))))
	}
	return card(out)
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
		return card(`<p class="unavailable">No interface is currently transferring data.</p>`)
	}
	return card(out)
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
	return card(out)
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
		return card(`<p class="unavailable">No GPU detected.</p>`)
	}
	// rows accumulates every GPU's own row into one shared card (matching
	// the original, pre-refactor behavior: multiple GPUs listed together
	// in a single card, not one card each); extra collects renderMem's
	// own already-self-wrapped card(s) for the Apple unified-memory case,
	// appended after — never inside — rows' own card, since renderMem
	// wraps its own output now (see renderMem's own comment) and nesting
	// a card inside a card would be wrong, not just redundant.
	var rows, extra string
	for _, g := range s.GPUs {
		switch {
		case g.VRAMTotal > 0:
			rows += row(g.Name, fmt.Sprintf("%.0f%% util, %s / %s VRAM", g.UtilPct, ui.HumanBytes(int64(g.VRAMUsed)), ui.HumanBytes(int64(g.VRAMTotal))))
		case strings.HasPrefix(g.Name, "Apple"):
			rows += row(g.Name, "unified memory — same pool as system RAM, shown below")
			extra += renderMem(s)
		default:
			rows += row(g.Name, "no utilisation/VRAM telemetry available for this GPU")
		}
		extra += gpuProcessSection(g)
	}
	return card(rows) + extra
}

// gpuProcessSection renders a "Processes holding VRAM" table for one GPU
// when vitals actually got a per-process VRAM reading for it — today that
// means an NVIDIA card via nvidia-smi's compute-apps list (see
// internal/gpu). "" for every other GPU, so the section simply doesn't
// appear rather than showing an empty table.
func gpuProcessSection(g doctor.GPU) string {
	if len(g.Processes) == 0 {
		return ""
	}
	procs := append([]doctor.GPUProc(nil), g.Processes...)
	sort.Slice(procs, func(i, j int) bool { return procs[i].VRAMUsed > procs[j].VRAMUsed })
	if len(procs) > topProcessesSectionN {
		procs = procs[:topProcessesSectionN]
	}
	rows := make([]gpuProcRow, len(procs))
	for i, p := range procs {
		rows[i] = gpuProcRow{PID: p.PID, Name: p.Name, VRAM: ui.HumanBytes(int64(p.VRAMUsed))}
	}
	return `<div class="sectiontitle">Processes holding VRAM on ` + template.HTMLEscapeString(g.Name) + `</div>` + card(mustExecute(gpuProcessesTmpl, rows))
}

type gpuProcRow struct {
	PID  int32
	Name string
	VRAM string
}

var gpuProcessesTmpl = template.Must(template.New("gpuProcesses").Parse(`<table style="width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--line);border-radius:10px;overflow:hidden;font-size:.86rem">` +
	`<tr style="background:var(--surface-2)"><th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Process</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">VRAM</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">PID</th></tr>` +
	`{{range .}}<tr>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);font-weight:600">{{.Name}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.VRAM}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.PID}}</td>` +
	`</tr>{{end}}</table>`))
