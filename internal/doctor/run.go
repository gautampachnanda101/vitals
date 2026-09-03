package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"vitals/internal/diag"
	"vitals/internal/ui"
)

// RunOptions configures the CLI entry point.
type RunOptions struct {
	OllamaURL            string
	JSON                 bool
	Output               string // if set, also write the JSON envelope here regardless of JSON/human stdout mode
	CI                   bool   // print one grep-friendly line ("CRITICAL: <finding>") instead of the full report
	Quiet                bool   // print nothing at all; only the exit code carries the verdict
	Webhook              string // if set, POST the JSON envelope here when the verdict needs attention
	WebhookAllowInsecure bool   // allow plain http and loopback/private/link-local --webhook targets (refused by default)
	Verbose              bool   // show more than the default view has room for (every core, the full reclaimable list, ...)
}

// Assess collects a snapshot and analyses it, returning both. Shared by the
// CLI and the MCP server (the metrics exporter calls Collect/Analyze
// directly on every scrape, deliberately bypassing the history write below).
func Assess(opts RunOptions) (Snapshot, diag.Report) {
	snap := Collect(Options{OllamaURL: opts.OllamaURL})
	return finishAssess(snap)
}

// finishAssess is the non-live remainder of Assess, split out so it can be
// exercised against a fixture Snapshot in tests without a real Collect()
// call. It records the snapshot into the trend history (best effort) and
// returns the analyzed report, augmented with a memory-growth finding when
// the trend history shows one — a signal Analyze can never see on its own
// since it only ever looks at a single Snapshot.
func finishAssess(snap Snapshot) (Snapshot, diag.Report) {
	recordHistory(snap)
	report := Analyze(snap)
	addLeakFinding(&report, LoadHistory())
	return snap, report
}

const (
	leakMinSamples     = 5
	leakMinGrowthBytes = 200 << 20 // 200 MB
)

// addLeakFinding appends a warning when history shows a sustained RSS climb,
// dropping Analyze's lone "No bottleneck detected" placeholder first so a
// real finding never sits next to a stale "all healthy" one.
func addLeakFinding(report *diag.Report, history []HistoryPoint) {
	proc, growth, ok := DetectMemoryGrowth(history, leakMinSamples, leakMinGrowthBytes)
	if !ok {
		return
	}
	if len(report.Findings) == 1 && report.Findings[0].Severity == diag.OK {
		report.Findings = nil
	}
	report.Add(diag.Finding{
		Severity: diag.Warn,
		Title:    fmt.Sprintf("%s is steadily climbing in memory", proc.Name),
		Detail: fmt.Sprintf("pid %d grew by %s over the recorded history with no drop back down — a likely memory leak, not normal usage",
			proc.PID, ui.HumanBytes(int64(growth))),
		Fixes: []string{
			fmt.Sprintf("restart it: `kill %d` (or quit the app normally)", proc.PID),
			"if it recurs, file it as a leak with the app's maintainer",
		},
	})
}

// Run collects a snapshot, analyses it, prints the verdict, and returns the
// process exit code (0 healthy, 1 warning, 2 critical).
func Run(opts RunOptions) int {
	snap, report := Assess(opts)

	if err := maybeWriteOutput(opts.Output, snap, report); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write --output file: %v\n", err)
	}
	if err := maybeNotify(opts.Webhook, snap, report, opts.WebhookAllowInsecure); err != nil {
		fmt.Fprintf(os.Stderr, "warning: webhook notification failed: %v\n", err)
	}

	if opts.Quiet {
		return report.ExitCode()
	}

	if opts.CI {
		fmt.Println(renderCI(report))
		return report.ExitCode()
	}

	if opts.JSON {
		emitJSON(snap, report)
		return report.ExitCode()
	}

	ui.Header("DOCTOR")
	fmt.Printf("  %s\n\n", ui.Key(time.Now().Format("2006-01-02 15:04:05")))
	fmt.Printf("  %s\n\n", summaryLine(snap))

	printFindings(report.SortedBySeverity(), true)

	fmt.Println()
	switch report.Worst() {
	case diag.Critical:
		ui.Errf("verdict: CRITICAL — address the finding above now")
	case diag.Warn:
		ui.Warnf("verdict: needs attention")
	default:
		ui.Okf("verdict: healthy")
	}
	fmt.Println()
	fmt.Println(ui.Key("  browse this and every resource at a glance: vitals dashboard"))
	return report.ExitCode()
}

// printFindings renders a ranked finding list: a severity-coloured title, a
// dim detail line, and yellow-arrowed fixes. spaced adds a blank line between
// findings.
func printFindings(findings []diag.Finding, spaced bool) {
	for i, f := range findings {
		if spaced && i > 0 {
			fmt.Println()
		}
		switch f.Severity {
		case diag.Critical:
			ui.Errf("%s", f.Title)
		case diag.Warn:
			ui.Warnf("%s", f.Title)
		default:
			ui.Okf("%s", f.Title)
		}
		if f.Detail != "" {
			fmt.Printf("     %s\n", ui.Key(f.Detail))
		}
		for _, fix := range f.Fixes {
			fmt.Printf("     %s %s\n", ui.Actionf("→"), fix)
		}
	}
}

// summaryLine renders the at-a-glance numbers behind the verdict — cpu/mem/
// disk/power — so "healthy" is independently checkable instead of a bare
// assertion, and a warning has real numbers to compare against instantly.
func summaryLine(s Snapshot) string {
	parts := []string{
		fmt.Sprintf("cpu %s", pct(s.CPU.UsedPct, 70, 90)),
		fmt.Sprintf("mem %s", pct(s.Memory.UsedPct, thresholds.RAMWarnPercent, thresholds.RAMHighPercent)),
	}
	if d, ok := fullestDisk(s.Disks); ok {
		parts = append(parts, fmt.Sprintf("disk %s (%s)",
			pct(d.UsedPct, thresholds.DiskWarnPercent, thresholds.DiskCriticalPercent), d.Mount))
	}
	if s.Power.OnBattery && s.Power.Percent > 0 {
		parts = append(parts, fmt.Sprintf("battery %s", ui.GradeLow(fmt.Sprintf("%.0f%%", s.Power.Percent), s.Power.Percent, 20, 8)))
	}
	return strings.Join(parts, "   ")
}

// fullestDisk returns the real mount with the highest space usage — the one
// number from the disk list worth showing at a glance, since capacity (not
// I/O busy-ness) is what "how's my disk doing" usually means.
func fullestDisk(ds []Disk) (Disk, bool) {
	var best Disk
	found := false
	for _, d := range ds {
		if !found || d.UsedPct > best.UsedPct {
			best, found = d, true
		}
	}
	return best, found
}

type JSONEnvelope struct {
	SchemaVersion string         `json:"schema_version"`
	Timestamp     string         `json:"timestamp"`
	Verdict       string         `json:"verdict"`
	ExitCode      int            `json:"exit_code"`
	Findings      []diag.Finding `json:"findings"`
	Snapshot      Snapshot       `json:"snapshot"`
}

// JSONReport builds the canonical `--json` envelope for a snapshot + verdict.
func JSONReport(s Snapshot, r diag.Report) JSONEnvelope {
	return JSONEnvelope{
		SchemaVersion: SchemaVersion,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Verdict:       r.Worst().String(),
		ExitCode:      r.ExitCode(),
		Findings:      r.SortedBySeverity(),
		Snapshot:      s,
	}
}

func emitJSON(s Snapshot, r diag.Report) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(JSONReport(s, r))
}
