package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vitals/internal/diag"
)

func writeEnvelope(t *testing.T, path string, env JSONEnvelope) {
	t.Helper()
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadJSONEnvelopeRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	want := JSONEnvelope{SchemaVersion: SchemaVersion, Verdict: "warning", ExitCode: 1,
		Findings: []diag.Finding{{Severity: diag.Warn, Title: "disk full"}}}
	writeEnvelope(t, path, want)

	got, err := LoadJSONEnvelope(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != "warning" || len(got.Findings) != 1 || got.Findings[0].Title != "disk full" {
		t.Errorf("loaded = %+v, want a match for %+v", got, want)
	}
}

func TestCompareEnvelopesDetectsAppearedAndResolved(t *testing.T) {
	old := JSONEnvelope{Verdict: "ok", Findings: []diag.Finding{
		{Severity: diag.Warn, Title: "swap heavily used"},
	}}
	newer := JSONEnvelope{Verdict: "critical", Findings: []diag.Finding{
		{Severity: diag.Critical, Title: "disk / nearly full"},
	}}

	cmp := compareEnvelopes(old, newer)
	if cmp.OldVerdict != "ok" || cmp.NewVerdict != "critical" {
		t.Errorf("verdicts = %+v", cmp)
	}
	if len(cmp.Appeared) != 1 || cmp.Appeared[0] != "disk / nearly full" {
		t.Errorf("appeared = %v, want [disk / nearly full]", cmp.Appeared)
	}
	if len(cmp.Resolved) != 1 || cmp.Resolved[0] != "swap heavily used" {
		t.Errorf("resolved = %v, want [swap heavily used]", cmp.Resolved)
	}
}

func TestCompareEnvelopesIgnoresFindingsPresentInBoth(t *testing.T) {
	shared := diag.Finding{Severity: diag.Warn, Title: "RAM elevated"}
	old := JSONEnvelope{Findings: []diag.Finding{shared}}
	newer := JSONEnvelope{Findings: []diag.Finding{shared}}

	cmp := compareEnvelopes(old, newer)
	if len(cmp.Appeared) != 0 || len(cmp.Resolved) != 0 {
		t.Errorf("an unchanged finding should be neither appeared nor resolved: %+v", cmp)
	}
}

func TestRenderCompareExportedWrapperMatchesInternalPipeline(t *testing.T) {
	old := JSONEnvelope{Verdict: "ok"}
	newer := JSONEnvelope{Verdict: "critical", Findings: []diag.Finding{{Severity: diag.Critical, Title: "disk full"}}}
	got := RenderCompare(old, newer)
	want := renderCompare(compareEnvelopes(old, newer))
	if got != want {
		t.Errorf("RenderCompare = %q, want %q", got, want)
	}
}

func TestRenderCompareMentionsBothVerdictsAndChanges(t *testing.T) {
	cmp := CompareResult{OldVerdict: "ok", NewVerdict: "critical", Appeared: []string{"disk / nearly full"}, Resolved: []string{"swap heavily used"}}
	out := renderCompare(cmp)
	for _, want := range []string{"ok", "critical", "disk / nearly full", "swap heavily used"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCompare output missing %q:\n%s", want, out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return len(sub) == 0
}
