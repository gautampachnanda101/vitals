package dashboard

import (
	"fmt"
	"html/template"
	"strings"

	"vitals/internal/doctor"
	"vitals/internal/ui"
)

func init() {
	Register(Module{Slug: "", NavLabel: "Overview", Group: "Overview", Icon: iconOverview, Order: 0, Available: Always, Render: renderOverview})
}

// renderOverview is the cross-resource verdict — the same correlation
// `vitals doctor` prints in a terminal — plus, unlike the terminal
// report, an at-a-glance grid of every resource (each linking to its own
// full page), loaded LLM models, and quick links to the mutating tools
// (Clean/Duplicates). The terminal has no room for this; a page does.
func renderOverview(ctx PageContext) string {
	s := ctx.Snapshot
	headline := reportHeadline(ctx.Report, "Healthy — nothing needs attention")
	body := verdictBanner(headline, summaryLine(s), ctx.Report.Worst())

	body += `<div class="sectiontitle">Resources</div><div class="grid">`
	body += cpuCard(s)
	body += memCard(s)
	if d, ok := fullestDisk(s.Disks); ok {
		body += diskCard(d)
	}
	if n, ok := busiestNet(s.Net); ok {
		body += netCard(n)
	}
	if HasBattery(ctx) {
		body += powerCard(s)
	}
	if HasGPU(ctx) {
		body += gpuCard(s.GPUs[0])
	}
	body += `</div>`

	if len(s.LLM) > 0 {
		body += `<div class="sectiontitle">Loaded models</div>`
		for _, m := range s.LLM {
			body += modelCard(m)
		}
	}

	body += `<div class="sectiontitle">Quick actions</div><div class="qa">` +
		quickAction("/clean", "Clean", "measure reclaimable cache space") +
		quickAction("/dupes", "Duplicates", "find and hardlink duplicate files") +
		quickAction("/advice", "Advice", "ranked findings, plus AI commentary") +
		`</div>`

	if card := findingsCard(ctx.Report.SortedBySeverity()); card != "" {
		body += `<div class="sectiontitle">Findings</div>` + card
	}
	return body
}

// resourceSeverity is doctor.AnalyzeResource's own worst-finding severity
// for resource — the single source of truth an Overview card's color and
// the resource's own detail page's verdict banner both derive from, so
// the two can never show a card colored differently than what clicking
// through to it would report.
func resourceSeverity(s doctor.Snapshot, resource string) string {
	return doctor.AnalyzeResource(s, resource).Worst().String()
}

func cpuCard(s doctor.Snapshot) string {
	detail := fmt.Sprintf("load %.2f on %d cores", s.CPU.Load1, s.CPU.Cores)
	if s.CPU.TopProc.Name != "" {
		detail = fmt.Sprintf("%s — %.0f%%", s.CPU.TopProc.Name, s.CPU.TopProc.CPUPct)
	}
	return resourceCard(resourceCardData{
		Slug: "cpu", Label: "CPU", Icon: iconCPU,
		Value: fmt.Sprintf("%.0f%%", s.CPU.UsedPct), Pct: s.CPU.UsedPct,
		Severity: resourceSeverity(s, "cpu"), Detail: detail,
	})
}

func memCard(s doctor.Snapshot) string {
	detail := fmt.Sprintf("swap %.0f%% used", s.Memory.SwapUsedPct)
	if s.Memory.TopProc.Name != "" {
		detail = fmt.Sprintf("%s — %s RSS", s.Memory.TopProc.Name, ui.HumanBytes(int64(s.Memory.TopProc.RSSBytes)))
	}
	return resourceCard(resourceCardData{
		Slug: "mem", Label: "Memory", Icon: iconMemory,
		Value: fmt.Sprintf("%.0f%%", s.Memory.UsedPct), Pct: s.Memory.UsedPct,
		Severity: resourceSeverity(s, "mem"), Detail: detail,
	})
}

func diskCard(d doctor.Disk) string {
	return resourceCard(resourceCardData{
		Slug: "disk", Label: "Disk", Icon: iconDisk,
		Value: fmt.Sprintf("%.0f%%", d.UsedPct), Pct: d.UsedPct,
		// disk's AnalyzeResource covers every mount at once; a single
		// card can only show one, so its severity is derived from this
		// specific (fullest) mount's own usage rather than the whole
		// resource's worst finding, which might point at a different,
		// less-full-but-otherwise-unhealthy mount.
		Severity: diskCardSeverity(d.UsedPct),
		Detail:   fmt.Sprintf("%s — %s free", d.Mount, ui.HumanBytes(int64(d.FreeBytes))),
	})
}

// diskCardSeverity mirrors config.Default()'s own disk warn/crit
// thresholds (90%/97%) — see internal/config — close enough for an
// at-a-glance card color; the disk page's own findings (from
// AnalyzeResource, config-driven) remain the source of truth for what's
// actually wrong.
func diskCardSeverity(usedPct float64) string {
	switch {
	case usedPct >= 97:
		return "critical"
	case usedPct >= 90:
		return "warning"
	default:
		return "ok"
	}
}

func netCard(n doctor.NetIface) string {
	total := n.RxBytesPerSec + n.TxBytesPerSec
	// No natural 0-100 "used%" for throughput — cap the bar's own visual
	// fill at a generous 100 MB/s so a real burst still reads as "busy"
	// without needing a live, machine-specific link-speed baseline.
	const capBps = 100 << 20
	pct := total / capBps * 100
	return resourceCard(resourceCardData{
		Slug: "net", Label: "Network", Icon: iconNetwork,
		Value: ui.HumanBytes(int64(total)) + "/s", Pct: pct,
		Severity: "ok", // no analyze* rule keys off raw throughput; nothing to color red over
		Detail:   fmt.Sprintf("↓ %s/s   ↑ %s/s — %s", ui.HumanBytes(int64(n.RxBytesPerSec)), ui.HumanBytes(int64(n.TxBytesPerSec)), n.Name),
	})
}

func powerCard(s doctor.Snapshot) string {
	detail := "on AC power"
	if s.Power.OnBattery {
		detail = "on battery"
		if s.Power.MinutesLeft > 0 {
			detail = fmt.Sprintf("on battery — ~%dh %dm left", s.Power.MinutesLeft/60, s.Power.MinutesLeft%60)
		}
	}
	return resourceCard(resourceCardData{
		Slug: "power", Label: "Power", Icon: iconPower,
		Value: fmt.Sprintf("%.0f%%", s.Power.Percent), Pct: s.Power.Percent,
		Severity: resourceSeverity(s, "power"), Detail: detail,
	})
}

func gpuCard(g doctor.GPU) string {
	detail := "no utilisation telemetry available"
	pct := g.UtilPct
	if g.VRAMTotal > 0 {
		detail = fmt.Sprintf("%s / %s VRAM", ui.HumanBytes(int64(g.VRAMUsed)), ui.HumanBytes(int64(g.VRAMTotal)))
	} else if strings.HasPrefix(g.Name, "Apple") {
		detail = "unified memory — see the Memory page"
	}
	return resourceCard(resourceCardData{
		Slug: "gpu", Label: "GPU", Icon: iconGPU,
		Value: fmt.Sprintf("%.0f%%", g.UtilPct), Pct: pct,
		Severity: "ok", // GPU has no analyze* rule of its own yet to key a severity off
		Detail:   g.Name + " — " + detail,
	})
}

// modelCardTmpl renders one loaded-LLM-model summary line — a condensed
// version of the LLM Insight page's own fuller modelcard, sized for the
// Overview's own limited space.
var modelCardTmpl = template.Must(template.New("overviewModelCard").Parse(
	`<div class="modelcard"><div><div class="mname">{{.Name}}</div><div class="msub">{{.Provider}}</div></div>` +
		`<span class="pill {{.PillClass}}">{{.PillText}}</span></div>`))

func modelCard(m doctor.LLMModel) string {
	pillClass, pillText := "ok", fmt.Sprintf("%.0f%% GPU", m.OffloadPct)
	switch {
	case m.OffloadPct >= 99.5:
		pillClass, pillText = "ok", "fully on GPU"
	case m.OffloadPct > 0:
		pillClass = "warn"
	default:
		pillClass, pillText = "crit", "CPU-only"
	}
	return mustExecute(modelCardTmpl, struct {
		Name, Provider, PillClass, PillText string
	}{m.Name, "Ollama", pillClass, pillText})
}

// quickActionTmpl links to a mutating tool's own page — Overview never
// runs an action itself, it only points at the page that does (Clean/
// Duplicates each already gate their own destructive call behind a
// confirm step, on both this dashboard and the CLI).
var quickActionTmpl = template.Must(template.New("quickAction").Parse(
	`<a href="{{.Href}}"><span class="qname">{{.Name}}</span><span class="qdesc">{{.Desc}}</span></a>`))

func quickAction(href, name, desc string) string {
	return mustExecute(quickActionTmpl, struct{ Href, Name, Desc string }{href, name, desc})
}

// summaryLine mirrors doctor's own terminal summary — the at-a-glance
// numbers a verdict has to be checkable against, not just asserted.
func summaryLine(s doctor.Snapshot) string {
	out := fmt.Sprintf("cpu %.0f%%   mem %.0f%%", s.CPU.UsedPct, s.Memory.UsedPct)
	if d, ok := fullestDisk(s.Disks); ok {
		out += fmt.Sprintf("   disk %.0f%% (%s)", d.UsedPct, d.Mount)
	}
	if s.Power.OnBattery && s.Power.Percent > 0 {
		out += fmt.Sprintf("   battery %.0f%%", s.Power.Percent)
	}
	return out
}

// fullestDisk returns the real mount with the highest usage — same
// selection doctor.Run's own summary line uses.
func fullestDisk(ds []doctor.Disk) (doctor.Disk, bool) {
	var best doctor.Disk
	found := false
	for _, d := range ds {
		if !found || d.UsedPct > best.UsedPct {
			best, found = d, true
		}
	}
	return best, found
}

// busiestNet returns the interface with the most combined throughput —
// the one worth a headline card, out of however many interfaces this
// machine has. Interfaces with no traffic at all are excluded, matching
// renderNet's own "nothing currently transferring" filter.
func busiestNet(ns []doctor.NetIface) (doctor.NetIface, bool) {
	var best doctor.NetIface
	found := false
	for _, n := range ns {
		if n.RxBytesPerSec < 1 && n.TxBytesPerSec < 1 {
			continue
		}
		if !found || (n.RxBytesPerSec+n.TxBytesPerSec) > (best.RxBytesPerSec+best.TxBytesPerSec) {
			best, found = n, true
		}
	}
	return best, found
}
