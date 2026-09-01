package diag

import (
	"encoding/json"
	"testing"
)

func TestSeverityMarshalJSON(t *testing.T) {
	b, err := json.Marshal(Finding{Severity: Critical, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"severity":"critical","title":"x"}` {
		t.Errorf("marshalled to %s", got)
	}
}

func TestSeverityString(t *testing.T) {
	cases := map[Severity]string{OK: "ok", Warn: "warning", Critical: "critical"}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Severity(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestSeverityExitCode(t *testing.T) {
	cases := map[Severity]int{OK: 0, Warn: 1, Critical: 2}
	for s, want := range cases {
		if got := s.ExitCode(); got != want {
			t.Errorf("Severity(%d).ExitCode() = %d, want %d", s, got, want)
		}
	}
}

func TestReportWorst(t *testing.T) {
	t.Run("empty report is OK", func(t *testing.T) {
		var r Report
		if r.Worst() != OK || r.ExitCode() != 0 {
			t.Errorf("empty report: worst=%v exit=%d", r.Worst(), r.ExitCode())
		}
	})
	t.Run("worst wins", func(t *testing.T) {
		var r Report
		r.Add(Finding{Severity: OK, Title: "fine"})
		r.Add(Finding{Severity: Critical, Title: "bad"})
		r.Add(Finding{Severity: Warn, Title: "meh"})
		if r.Worst() != Critical || r.ExitCode() != 2 {
			t.Errorf("worst=%v exit=%d, want critical/2", r.Worst(), r.ExitCode())
		}
	})
}

func TestSortedBySeverity(t *testing.T) {
	var r Report
	r.Add(Finding{Severity: Warn, Title: "w1"})
	r.Add(Finding{Severity: Critical, Title: "c1"})
	r.Add(Finding{Severity: OK, Title: "o1"})
	r.Add(Finding{Severity: Critical, Title: "c2"})
	r.Add(Finding{Severity: Warn, Title: "w2"})

	got := r.SortedBySeverity()
	var order []string
	for _, f := range got {
		order = append(order, f.Title)
	}
	// Critical first, then Warn, then OK; original order preserved within a tier.
	want := []string{"c1", "c2", "w1", "w2", "o1"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	// Original slice is untouched.
	if r.Findings[0].Title != "w1" {
		t.Errorf("SortedBySeverity mutated the report")
	}
}

func TestReportAddNormalizesUnknownSeverity(t *testing.T) {
	var r Report
	r.Add(Finding{Severity: Severity(99), Title: "weird"})
	if r.Findings[0].Severity != OK {
		t.Errorf("out-of-range severity should clamp to OK, got %v", r.Findings[0].Severity)
	}
}
