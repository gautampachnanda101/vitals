// Package diag is the shared vocabulary for vitals' diagnostic output: a
// severity scale, a finding (a problem plus how to fix it), and a report that
// ranks findings and maps them to a process exit code. Both memcheck's verdict
// and, later, `vitals doctor` produce diag.Report values.
package diag

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Severity is how much a finding should worry the reader.
type Severity int

const (
	OK Severity = iota
	Warn
	Critical
)

func (s Severity) valid() bool { return s >= OK && s <= Critical }

// String renders the severity as a lowercase word.
func (s Severity) String() string {
	switch s {
	case Warn:
		return "warning"
	case Critical:
		return "critical"
	default:
		return "ok"
	}
}

// ExitCode is the process exit status a report at this severity should produce:
// 0 healthy, 1 warning, 2 critical.
func (s Severity) ExitCode() int {
	switch s {
	case Warn:
		return 1
	case Critical:
		return 2
	default:
		return 0
	}
}

// MarshalJSON renders the severity as its lowercase word.
func (s Severity) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON parses one of "ok", "warning", "critical" — the exact words
// MarshalJSON produces — back into a Severity. Anything else is an error
// rather than a silent fallback to OK, since a saved report with a garbled
// severity should be rejected, not misread as healthy.
func (s *Severity) UnmarshalJSON(data []byte) error {
	var word string
	if err := json.Unmarshal(data, &word); err != nil {
		return err
	}
	switch word {
	case "ok":
		*s = OK
	case "warning":
		*s = Warn
	case "critical":
		*s = Critical
	default:
		return fmt.Errorf("diag: unknown severity %q", word)
	}
	return nil
}

// Finding is one observation: what is wrong, the evidence, and concrete fixes.
type Finding struct {
	Severity Severity `json:"severity"`
	// ID is a short, stable kebab-case key for this finding kind
	// ("mem-pressure", "disk-low", ...), assigned by the builder in
	// analyze.go — never derived from Title. Used by `vitals heal
	// --only <id>` and by `--json` consumers / dashboard deep-links.
	// Empty for a finding kind that hasn't been given one yet.
	ID     string   `json:"id,omitempty"`
	Title  string   `json:"title"`
	Detail string   `json:"detail,omitempty"`
	Fixes  []string `json:"fixes,omitempty"`
	// Remedy is the machine-executable fix `vitals heal` would run for
	// this finding, or nil (the common case) when there is no safe
	// automatable action — then Fixes is advisory only. Built only by
	// hand-written builders next to the finding; never parsed from a
	// string, never accepted from outside the process.
	Remedy *Remedy `json:"remedy,omitempty"`
}

// RemedyKind is how `vitals heal` executes a Remedy.
type RemedyKind int

const (
	// RemedyManual: no automatable action — Fixes are advice only.
	RemedyManual RemedyKind = iota
	// RemedyExec: run Argv verbatim. heal refuses any Argv[0] not in its
	// own compile-time allowlist.
	RemedyExec
	// RemedyDelegate: run another vitals subcommand (Argv[0] == "vitals",
	// resolved to os.Executable()).
	RemedyDelegate
	// RemedySignal is defined for the schema's sake but NOT enabled in
	// heal v1 — a kill on a wrong/recycled pid is an immediate,
	// irreversible loss (review must-fix). heal rejects it.
	RemedySignal
)

// RemedyRisk is advisory metadata for heal's confirmation UI.
type RemedyRisk int

const (
	RiskLow    RemedyRisk = iota // reversible or trivially so
	RiskMedium                   // reversible with effort / a transient restart
	RiskHigh                     // irreversible, or a running app with unsaved state
)

// Remedy is a finding's machine-executable fix. heal acts only on Argv
// (RemedyExec/RemedyDelegate) or Signal+PID (RemedySignal, disabled in
// v1); Label and the risk metadata drive the confirmation prompt only.
type Remedy struct {
	Kind       RemedyKind `json:"kind"`
	Label      string     `json:"label"`
	Argv       []string   `json:"argv,omitempty"`
	Signal     string     `json:"signal,omitempty"`
	PID        int32      `json:"pid,omitempty"`
	Risk       RemedyRisk `json:"risk"`
	Reversible bool       `json:"reversible"`
}

func (k RemedyKind) String() string {
	switch k {
	case RemedyExec:
		return "exec"
	case RemedyDelegate:
		return "delegate"
	case RemedySignal:
		return "signal"
	default:
		return "manual"
	}
}

// MarshalJSON / UnmarshalJSON round-trip RemedyKind as a stable lowercase
// word, the same discipline Severity uses, so a saved report survives.
func (k RemedyKind) MarshalJSON() ([]byte, error) { return []byte(`"` + k.String() + `"`), nil }

func (k *RemedyKind) UnmarshalJSON(data []byte) error {
	var w string
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	switch w {
	case "manual":
		*k = RemedyManual
	case "exec":
		*k = RemedyExec
	case "delegate":
		*k = RemedyDelegate
	case "signal":
		*k = RemedySignal
	default:
		return fmt.Errorf("diag: unknown remedy kind %q", w)
	}
	return nil
}

func (r RemedyRisk) String() string {
	switch r {
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return "low"
	}
}

func (r RemedyRisk) MarshalJSON() ([]byte, error) { return []byte(`"` + r.String() + `"`), nil }

func (r *RemedyRisk) UnmarshalJSON(data []byte) error {
	var w string
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	switch w {
	case "low":
		*r = RiskLow
	case "medium":
		*r = RiskMedium
	case "high":
		*r = RiskHigh
	default:
		return fmt.Errorf("diag: unknown remedy risk %q", w)
	}
	return nil
}

// Report collects findings from one or more diagnostic passes.
type Report struct {
	Findings []Finding
}

// Add appends a finding, clamping an out-of-range severity to OK so a caller
// mistake can never inflate the exit code.
func (r *Report) Add(f Finding) {
	if !f.Severity.valid() {
		f.Severity = OK
	}
	r.Findings = append(r.Findings, f)
}

// Worst is the highest severity across all findings; OK for an empty report.
func (r Report) Worst() Severity {
	worst := OK
	for _, f := range r.Findings {
		if f.Severity > worst {
			worst = f.Severity
		}
	}
	return worst
}

// ExitCode is the exit status for the report as a whole.
func (r Report) ExitCode() int { return r.Worst().ExitCode() }

// SortedBySeverity returns the findings most-severe first, preserving the
// original order within each severity tier. The receiver is not modified.
func (r Report) SortedBySeverity() []Finding {
	out := make([]Finding, len(r.Findings))
	copy(out, r.Findings)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Severity > out[j].Severity
	})
	return out
}
