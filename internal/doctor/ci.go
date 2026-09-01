package doctor

import (
	"strings"

	"vitals/internal/diag"
)

// renderCI renders a report as a single grep-friendly line for CI logs —
// "CRITICAL: <worst finding title>" or "OK: healthy" — instead of the full
// multi-line human report. The full --json contract already serves
// machine consumption; --ci serves the much more common case of a CI log
// someone will read (or grep) as plain text.
func renderCI(r diag.Report) string {
	worst := r.Worst()
	if worst == diag.OK {
		return "OK: healthy"
	}
	title := ""
	for _, f := range r.SortedBySeverity() {
		if f.Severity == worst {
			title = f.Title
			break
		}
	}
	return strings.ToUpper(worst.String()) + ": " + title
}
