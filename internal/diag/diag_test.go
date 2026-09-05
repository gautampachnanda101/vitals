package diag

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestSeverityUnmarshalJSON(t *testing.T) {
	cases := map[string]Severity{`"ok"`: OK, `"warning"`: Warn, `"critical"`: Critical}
	for raw, want := range cases {
		var s Severity
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatalf("Unmarshal(%s): %v", raw, err)
		}
		if s != want {
			t.Errorf("Unmarshal(%s) = %v, want %v", raw, s, want)
		}
	}
}

func TestSeverityUnmarshalJSONRejectsUnknownWord(t *testing.T) {
	var s Severity
	if err := json.Unmarshal([]byte(`"catastrophic"`), &s); err == nil {
		t.Error("an unrecognised severity word should error, not silently become OK")
	}
}

func TestSeverityRoundTripsThroughJSON(t *testing.T) {
	want := Finding{Severity: Critical, Title: "disk full", Detail: "x", Fixes: []string{"y"}}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Finding
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-tripped = %+v, want %+v", got, want)
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

func TestRemedyKindJSONRoundTrip(t *testing.T) {
	for k, word := range map[RemedyKind]string{
		RemedyManual: "manual", RemedyExec: "exec", RemedyDelegate: "delegate", RemedySignal: "signal",
	} {
		b, err := json.Marshal(k)
		if err != nil || string(b) != `"`+word+`"` {
			t.Fatalf("Marshal(%v) = %s, %v; want %q", k, b, err, word)
		}
		var got RemedyKind
		if err := json.Unmarshal(b, &got); err != nil || got != k {
			t.Errorf("Unmarshal(%s) = %v, %v; want %v", b, got, err, k)
		}
	}
	var k RemedyKind
	if err := json.Unmarshal([]byte(`"bogus"`), &k); err == nil {
		t.Error("an unknown remedy kind must be an error, not a silent zero")
	}
}

func TestRemedyRiskJSONRoundTrip(t *testing.T) {
	for r, word := range map[RemedyRisk]string{RiskLow: "low", RiskMedium: "medium", RiskHigh: "high"} {
		b, _ := json.Marshal(r)
		if string(b) != `"`+word+`"` {
			t.Errorf("Marshal(%v) = %s, want %q", r, b, word)
		}
		var got RemedyRisk
		if json.Unmarshal(b, &got) != nil || got != r {
			t.Errorf("round-trip %v failed: got %v", r, got)
		}
	}
	var r RemedyRisk
	if json.Unmarshal([]byte(`"extreme"`), &r) == nil {
		t.Error("an unknown risk must be an error")
	}
}

func TestFindingRemedyRoundTrips(t *testing.T) {
	f := Finding{
		Severity: Critical, ID: "disk-low", Title: "Disk / nearly full",
		Fixes: []string{"clean it"},
		Remedy: &Remedy{
			Kind: RemedyDelegate, Label: "run vitals clean",
			Argv: []string{"vitals", "clean"}, Risk: RiskMedium, Reversible: false,
		},
	}
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var got Finding
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "disk-low" || got.Remedy == nil || got.Remedy.Kind != RemedyDelegate ||
		got.Remedy.Risk != RiskMedium || len(got.Remedy.Argv) != 2 {
		t.Errorf("Finding did not round-trip: %+v", got)
	}
	plain, _ := json.Marshal(Finding{Severity: OK, Title: "fine"})
	if strings.Contains(string(plain), "remedy") || strings.Contains(string(plain), `"id"`) {
		t.Errorf("empty id/remedy should be omitted: %s", plain)
	}
}

func TestRemedyEnumsRejectMalformedJSON(t *testing.T) {
	var k RemedyKind
	if json.Unmarshal([]byte(`123`), &k) == nil {
		t.Error("RemedyKind.UnmarshalJSON should reject a non-string")
	}
	var r RemedyRisk
	if json.Unmarshal([]byte(`{}`), &r) == nil {
		t.Error("RemedyRisk.UnmarshalJSON should reject a non-string")
	}
	var s Severity
	if json.Unmarshal([]byte(`[1]`), &s) == nil {
		t.Error("Severity.UnmarshalJSON should reject a non-string")
	}
}
