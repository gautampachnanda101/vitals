// Package consoleview renders the whole machine on one terminal screen —
// `vitals` with no subcommand (on a TTY), or `vitals view`. The verdict
// leads; the resource panels are the supporting evidence, not the
// headline. It is the terminal counterpart to internal/dashboard's web
// pages and shares only the model (doctor.Snapshot / diag.Report /
// monitor.Snapshot), never the rendering.
//
// Render is a pure function over already-collected inputs (roadmap item
// 011 design, and the review that followed): no I/O, so every layout
// branch is a table test. consoleview.Run is the thin live glue.
//
// v1 scope decisions (from the review's open questions):
//   - static snapshot only; no refresh loop, no TUI dependency.
//   - no "events / just-crossed" strip — doctor's history stores
//     resource samples, not prior findings.
//   - ASCII-safe column widths via rune count (ui.Truncate); a wide
//     (CJK / emoji) cell can be a column off. Taking a display-width
//     dependency (golang.org/x/text/width) is a deliberate future
//     decision, not a v1 default — the "one dependency" claim holds.
package consoleview

import (
	"fmt"
	"io"
	"strings"
	"time"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/monitor"
	"vitals/internal/ui"
)

// DefaultHeight is the row budget when the real terminal height can't be
// read (ui.TermSize's fallback) — the classic 24-line default.
const DefaultHeight = 24

// minPanelWidth is the narrowest a resource panel is allowed to be; the
// grid drops from 2-up to 1-up rather than squeezing below this.
const minPanelWidth = 34

// defaultProcRows caps the process table when the terminal height is
// unknown (piped output); with a known height, fit-to-height shows as
// many as fit.
const defaultProcRows = 15

// Input is everything Render needs — collected by the caller, so Render
// itself is pure.
type Input struct {
	Snapshot doctor.Snapshot
	Report   diag.Report
	Procs    monitor.Snapshot
	Version  string
	Now      time.Time
}

// Render lays the whole screen out as one string. width/height are the
// terminal size. fitHeight false (an unknown terminal size) renders
// everything and lets the pager scroll; true trims the compressible
// budget — process rows first, then whole optional panels, then the
// least-severe whole findings — so the verdict and the remaining
// findings are never pushed off-screen.
func Render(in Input, width, height int, fitHeight bool) string {
	if width < 20 {
		width = ui.DefaultWrapWidth
	}

	var lines []string
	lines = append(lines, headerLine(in, width))
	lines = append(lines, "")
	lines = append(lines, verdictLine(in.Report))
	lines = append(lines, "  "+clip(ui.Sanitize(doctor.SummaryLine(in.Snapshot)), width-2))
	lines = append(lines, "")

	findings := in.Report.SortedBySeverity()
	// A lone "No bottleneck detected" OK finding reads better as one
	// green line than as a findings block.
	if len(findings) == 1 && findings[0].Severity == diag.OK {
		lines = append(lines, ui.GradeSeverity("ok", "  ✓ "+findings[0].Title))
		findings = nil
	}

	panels := resourcePanels(in, width)
	procRows := processRows(in.Procs, width)

	// With a known terminal height, fit-to-height decides how many
	// process rows to show. Without one, cap at a sensible default
	// rather than dumping the whole (wide) capture.
	shownProc := len(procRows)
	if !fitHeight && shownProc > defaultProcRows {
		shownProc = defaultProcRows
	}

	body := assemble(lines, findings, panels, procRows, width, shownProc)
	if !fitHeight || countLines(body) <= height {
		return body + footerLine(in, width)
	}

	// Trim, in priority order, recomputing the height each step.
	nProc := len(procRows)
	for nProc > 0 {
		nProc--
		body = assemble(lines, findings, panels, procRows, width, nProc)
		if countLines(body)+2 <= height { // +2 leaves room for the footer + its blank line
			return body + footerLine(in, width)
		}
	}
	for len(panels) > 0 {
		panels = panels[:len(panels)-1]
		body = assemble(lines, findings, panels, procRows, width, 0)
		if countLines(body)+2 <= height {
			return body + footerLine(in, width)
		}
	}
	// Panels and the process table are gone; drop the least-severe
	// whole findings last, with a tail line, but never the verdict.
	for len(findings) > 1 {
		dropped := len(in.Report.SortedBySeverity()) - (len(findings) - 1)
		trimmed := findings[:len(findings)-1]
		body = assemble(lines, trimmed, nil, nil, width, 0)
		body += fmt.Sprintf("\n  … %d more finding(s) — run `vitals doctor`\n", dropped)
		if countLines(body)+2 <= height {
			return body + footerLine(in, width)
		}
		findings = trimmed
	}
	// Even the verdict + one finding overflows a tiny terminal: emit it
	// anyway (a target, not a guarantee — the screen scrolls).
	return assemble(lines, findings, nil, nil, width, 0) + footerLine(in, width)
}

// assemble joins the fixed head lines, the findings block, the panel
// grid and up to nProc process rows into one string.
func assemble(head []string, findings []diag.Finding, panels [][]string, procRows []string, width, nProc int) string {
	var b strings.Builder
	for _, l := range head {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if s := findingsBlock(findings, width); s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	if s := panelGrid(panels, width); s != "" {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	if nProc > 0 && len(procRows) > 0 {
		b.WriteString(ui.GradeSeverity("", sectionTitle("Top processes by CPU")))
		b.WriteByte('\n')
		for i, r := range procRows {
			if i >= nProc {
				break
			}
			b.WriteString(r)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func countLines(s string) int { return strings.Count(s, "\n") }

// --- head / verdict / footer -------------------------------------------

func headerLine(in Input, width int) string {
	h := in.Procs.Host
	parts := []string{ui.Sanitize(nz(h.Hostname, "this machine"))}
	if h.OS != "" {
		parts = append(parts, ui.Sanitize(h.OS+" "+h.Kernel))
	}
	if h.Uptime > 0 {
		parts = append(parts, "up "+shortDuration(time.Duration(h.Uptime)*time.Second))
	}
	if c := in.Snapshot.CPU.Cores; c > 0 {
		parts = append(parts, fmt.Sprintf("%d cores", c))
	}
	parts = append(parts, "vitals "+nz(in.Version, "dev"))
	return ui.Key(clip(strings.Join(parts, "  ·  "), width))
}

func verdictLine(r diag.Report) string {
	switch r.Worst() {
	case diag.Critical:
		return ui.GradeSeverity("critical", "  ✗ CRITICAL — address the finding below now")
	case diag.Warn:
		return ui.GradeSeverity("warning", "  ⚠ needs attention")
	default:
		return ui.GradeSeverity("ok", "  ✓ healthy — nothing needs attention")
	}
}

func footerLine(in Input, width int) string {
	ts := in.Now
	if ts.IsZero() {
		ts = time.Now()
	}
	return "\n" + ui.Key(clip(
		ts.Format("2006-01-02 15:04:05")+"  ·  `vitals doctor --json` for the full report  ·  `vitals <resource>` to drill in",
		width))
}

// --- findings ---------------------------------------------------------

func findingsBlock(findings []diag.Finding, width int) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	for i, f := range findings {
		if i > 0 {
			b.WriteByte('\n')
		}
		mark := map[diag.Severity]string{diag.Critical: "✗", diag.Warn: "⚠", diag.OK: "✓"}[f.Severity]
		b.WriteString("  " + ui.GradeSeverity(f.Severity.String(), mark+" "+ui.Sanitize(f.Title)) + "\n")
		for _, line := range ui.Wrap(ui.Sanitize(f.Detail), width-6) {
			b.WriteString("     " + ui.Key(line) + "\n")
		}
		for _, fix := range f.Fixes {
			wl := ui.Wrap(ui.Sanitize(fix), width-8)
			b.WriteString("     " + ui.GradeSeverity("", "→ ") + wl[0] + "\n")
			for _, c := range wl[1:] {
				b.WriteString("       " + c + "\n")
			}
		}
	}
	return b.String()
}

// --- resource panels ------------------------------------------------------

// resourcePanels returns one line-slice per available resource, each
// already framed to panelWidthCols(width). Severity per panel comes from
// doctor.AnalyzeResource — the single source of truth, so no threshold
// constant is duplicated here.
func resourcePanels(in Input, width int) [][]string {
	s := in.Snapshot
	pw := panelWidthCols(width)
	var out [][]string

	out = append(out, panel(pw, "CPU", sev(s, "cpu"), []kv{
		{"used", fmt.Sprintf("%.0f%%", s.CPU.UsedPct)},
		{"load", fmt.Sprintf("%.2f / %d", s.CPU.Load1, s.CPU.Cores)},
		{"iowait", fmt.Sprintf("%.0f%%", s.CPU.IOWaitPct)},
		{"top", procRefShort(s.CPU.TopProc, false)},
	}))
	out = append(out, panel(pw, "MEMORY", sev(s, "mem"), []kv{
		{"used", fmt.Sprintf("%.0f%%", s.Memory.UsedPct)},
		{"avail", fmt.Sprintf("%.0f%%", s.Memory.AvailablePct)},
		{"swap", fmt.Sprintf("%.0f%%", s.Memory.SwapUsedPct)},
		{"top", procRefShort(s.Memory.TopProc, true)},
	}))
	if d, ok := fullestDisk(s.Disks); ok {
		rows := []kv{
			{"used", fmt.Sprintf("%.0f%%", d.UsedPct)},
			{"free", ui.HumanBytes(int64(d.FreeBytes))},
			{"mount", ui.Sanitize(d.Mount)},
		}
		if d.SMART != nil {
			st := "S.M.A.R.T. ok"
			if !d.SMART.Passed {
				st = "S.M.A.R.T. FAILED"
			}
			rows = append(rows, kv{"health", st})
		}
		out = append(out, panel(pw, "DISK", diskSev(d.UsedPct), rows))
	}
	if n, ok := busiestNet(s.Net); ok {
		out = append(out, panel(pw, "NETWORK", "ok", []kv{
			{"iface", ui.Sanitize(n.Name)},
			{"down", ui.HumanBytes(int64(n.RxBytesPerSec)) + "/s"},
			{"up", ui.HumanBytes(int64(n.TxBytesPerSec)) + "/s"},
		}))
	}
	if s.Power.OnBattery || s.Power.Percent > 0 {
		src := "AC"
		if s.Power.OnBattery {
			src = "battery"
		}
		rows := []kv{{"source", src}, {"charge", fmt.Sprintf("%.0f%%", s.Power.Percent)}}
		if s.Power.MinutesLeft > 0 {
			rows = append(rows, kv{"left", fmt.Sprintf("~%dh %dm", s.Power.MinutesLeft/60, s.Power.MinutesLeft%60)})
		}
		out = append(out, panel(pw, "POWER", sev(s, "power"), rows))
	}
	if len(s.GPUs) > 0 {
		g := s.GPUs[0]
		rows := []kv{{"name", ui.Sanitize(g.Name)}, {"util", fmt.Sprintf("%.0f%%", g.UtilPct)}}
		if g.VRAMTotal > 0 {
			rows = append(rows, kv{"vram", ui.HumanBytes(int64(g.VRAMUsed)) + " / " + ui.HumanBytes(int64(g.VRAMTotal))})
		}
		out = append(out, panel(pw, "GPU", "ok", rows))
	}
	return out
}

func sev(s doctor.Snapshot, resource string) string {
	return doctor.AnalyzeResource(s, resource).Worst().String()
}

func diskSev(usedPct float64) string {
	switch {
	case usedPct >= 97:
		return "critical"
	case usedPct >= 90:
		return "warning"
	default:
		return "ok"
	}
}

type kv struct{ k, v string }

// panel frames one resource as a fixed-width box. The title carries the
// severity colour; body rows are plain.
func panel(cols int, title, severity string, rows []kv) []string {
	inner := cols - 4 // "│ " + " │"
	if inner < 8 {
		inner = 8
	}
	top := "┌" + strings.Repeat("─", cols-2) + "┐"
	bot := "└" + strings.Repeat("─", cols-2) + "┘"
	head := "│ " + padRight(ui.GradeSeverity(severity, title), inner, len([]rune(title))) + " │"
	out := []string{top, head, "│ " + strings.Repeat("─", inner) + " │"}
	for _, r := range rows {
		label := ui.Key(r.k)
		val := ui.Truncate(r.v, inner-len([]rune(r.k))-1)
		line := padRight(label+" "+val, inner, len([]rune(r.k))+1+len([]rune(val)))
		out = append(out, "│ "+line+" │")
	}
	out = append(out, bot)
	return out
}

// panelGrid lays panels 2-up (or 1-up on a narrow terminal), padding the
// shorter of a pair so the two boxes align.
func panelGrid(panels [][]string, width int) string {
	if len(panels) == 0 {
		return ""
	}
	perRow := 2
	if width < 2*minPanelWidth+3 {
		perRow = 1
	}
	var b strings.Builder
	for i := 0; i < len(panels); i += perRow {
		row := panels[i:min(i+perRow, len(panels))]
		h := 0
		for _, p := range row {
			h = max(h, len(p))
		}
		for line := 0; line < h; line++ {
			for j, p := range row {
				if j > 0 {
					b.WriteByte(' ')
				}
				if line < len(p) {
					b.WriteString(p[line])
				} else {
					b.WriteString(strings.Repeat(" ", visualWidth(p[0])))
				}
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func panelWidthCols(width int) int {
	w := (width - 3) / 2 // two panels + one space between
	if w < minPanelWidth {
		w = min(width, 60)
	}
	if w > 60 {
		w = 60
	}
	return w
}

// --- process table ------------------------------------------------------

func processRows(m monitor.Snapshot, width int) []string {
	if len(m.Processes) == 0 {
		return nil
	}
	// "  " + name + "  " + cpu(5) + "  " + rss(10) + "  " + pid(7)
	const fixed = 2 + 2 + 5 + 2 + 10 + 2 + 7
	nameW := width - fixed
	if nameW > 32 {
		nameW = 32
	}
	if nameW < 8 {
		nameW = 8
	}
	rows := make([]string, 0, len(m.Processes))
	for _, p := range m.Processes {
		row := fmt.Sprintf("  %-*s  %5s  %10s  %7d",
			nameW, ui.Truncate(ui.Sanitize(p.Name), nameW),
			fmt.Sprintf("%.0f%%", p.CPUPct), ui.HumanBytes(int64(p.RSSBytes)), p.PID)
		rows = append(rows, clip(row, width))
	}
	return rows
}

// --- small helpers ----------------------------------------------------

func sectionTitle(s string) string {
	return "\n  " + strings.ToUpper(s)
}

func procRefShort(p doctor.ProcRef, byMem bool) string {
	if p.Name == "" {
		return "—"
	}
	if byMem {
		return ui.Truncate(ui.Sanitize(p.Name), 18) + " " + ui.HumanBytes(int64(p.RSSBytes))
	}
	return ui.Truncate(ui.Sanitize(p.Name), 18) + fmt.Sprintf(" %.0f%%", p.CPUPct)
}

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

func nz(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// padRight pads s to width visible columns. plainLen is the rune length
// of the *uncoloured* text (s may carry ANSI codes that don't take
// columns), so the pad math ignores the invisible bytes.
func padRight(s string, width, plainLen int) string {
	if plainLen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-plainLen)
}

func visualWidth(s string) int { return len([]rune(ui.StripANSI(s))) }

func clip(s string, width int) string {
	if width <= 1 || len([]rune(s)) <= width {
		return s
	}
	return string([]rune(s)[:width-1]) + "…"
}

func shortDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hrs := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hrs)
	case hrs > 0:
		return fmt.Sprintf("%dh %dm", hrs, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- live glue -------------------------------------------------------

// Options configures a Run.
type Options struct {
	OllamaURL string
}

// Run collects the two snapshots concurrently under a wall-clock ceiling,
// renders, writes to w, and returns doctor's verdict exit code. size
// reports the terminal (cols, rows, ok); ok=false means "unknown size —
// render everything".
func Run(w io.Writer, size func() (int, int, bool), opts Options) int {
	type quick struct {
		snap   doctor.Snapshot
		report diag.Report
	}
	qc := make(chan quick, 1)
	pc := make(chan monitor.Snapshot, 1)

	go func() {
		s, r := doctor.QuickAssess(doctor.RunOptions{OllamaURL: opts.OllamaURL})
		qc <- quick{s, r}
	}()
	go func() {
		m, _ := monitor.Sample(monitor.Options{Top: 200, SortBy: "cpu"})
		pc <- m
	}()

	deadline := time.After(3 * time.Second)
	var q quick
	var procs monitor.Snapshot
	gotQ, gotP := false, false
	for !(gotQ && gotP) {
		select {
		case q = <-qc:
			gotQ = true
		case procs = <-pc:
			gotP = true
		case <-deadline:
			gotQ, gotP = true, true // render with whatever arrived
		}
	}

	cols, rows, ok := size()
	if cols == 0 {
		cols = ui.DefaultWrapWidth
	}
	if rows == 0 {
		rows = DefaultHeight
	}
	out := Render(Input{
		Snapshot: q.snap, Report: q.report, Procs: procs,
		Version: version(), Now: time.Now(),
	}, cols, rows, ok)
	fmt.Fprint(w, out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(w)
	}
	return q.report.ExitCode()
}

// version is overridden by main via SetVersion so the header can show it
// without this package importing package main.
var ver = "dev"

// SetVersion is called once from main before Run.
func SetVersion(v string) {
	if v != "" {
		ver = v
	}
}
func version() string { return ver }

// DefaultSize is the terminal-size probe Run gets from main.
func DefaultSize() (int, int, bool) { return ui.TermSize() }
