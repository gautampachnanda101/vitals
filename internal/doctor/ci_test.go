package doctor

import (
	"strings"
	"testing"

	"vitals/internal/diag"
)

func TestRenderCIHealthy(t *testing.T) {
	got := renderCI(diag.Report{})
	if got != "OK: healthy" {
		t.Errorf("renderCI(healthy) = %q, want %q", got, "OK: healthy")
	}
}

func TestRenderCINamesTheWorstFinding(t *testing.T) {
	var r diag.Report
	r.Add(diag.Finding{Severity: diag.Warn, Title: "RAM elevated"})
	r.Add(diag.Finding{Severity: diag.Critical, Title: "Disk / nearly full"})

	got := renderCI(r)
	if !strings.HasPrefix(got, "CRITICAL:") {
		t.Errorf("renderCI should lead with the worst severity, got %q", got)
	}
	if !strings.Contains(got, "Disk / nearly full") {
		t.Errorf("renderCI should name the critical finding over the warning, got %q", got)
	}
	if strings.Contains(got, "RAM elevated") {
		t.Errorf("renderCI should be a single line naming only the worst finding, got %q", got)
	}
}

func TestRenderCIIsASingleLine(t *testing.T) {
	var r diag.Report
	r.Add(diag.Finding{Severity: diag.Warn, Title: "RAM elevated", Detail: "line one\nline two"})
	if got := renderCI(r); strings.Contains(got, "\n") {
		t.Errorf("renderCI must be grep-friendly (one line), got %q", got)
	}
}
