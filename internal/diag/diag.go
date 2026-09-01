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
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Fixes    []string `json:"fixes,omitempty"`
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
