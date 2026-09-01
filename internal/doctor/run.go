package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"vitals/internal/diag"
	"vitals/internal/ui"
)

// RunOptions configures the CLI entry point.
type RunOptions struct {
	OllamaURL string
	JSON      bool
}

// Assess collects a snapshot and analyses it, returning both. Shared by the
// CLI, the metrics exporter and the MCP server.
func Assess(opts RunOptions) (Snapshot, diag.Report) {
	snap := Collect(Options{OllamaURL: opts.OllamaURL})
	return snap, Analyze(snap)
}

// Run collects a snapshot, analyses it, prints the verdict, and returns the
// process exit code (0 healthy, 1 warning, 2 critical).
func Run(opts RunOptions) int {
	snap, report := Assess(opts)

	if opts.JSON {
		emitJSON(snap, report)
		return report.ExitCode()
	}

	ui.Header("DOCTOR")
	fmt.Printf("  %s\n\n", ui.Key(time.Now().Format("2006-01-02 15:04:05")))

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
