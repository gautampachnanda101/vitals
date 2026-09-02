package dashboard

import (
	"strings"
	"testing"

	"vitals/internal/diag"
)

func TestLayoutHighlightsActiveNavItem(t *testing.T) {
	out := layout("Overview", "cpu", []Module{
		{Slug: "", NavLabel: "Overview"},
		{Slug: "cpu", NavLabel: "CPU"},
	}, "<p>body</p>")

	if !strings.Contains(out, `<a href="/cpu" class="active">CPU</a>`) {
		t.Errorf("active nav item not marked, got:\n%s", out)
	}
	if !strings.Contains(out, `<a href="/">Overview</a>`) {
		t.Errorf("inactive nav item should have no class, got:\n%s", out)
	}
	if !strings.Contains(out, "<p>body</p>") {
		t.Error("body content missing from rendered page")
	}
}

func TestLayoutEscapesNavLabels(t *testing.T) {
	out := layout("t", "x", []Module{{Slug: "x", NavLabel: "<script>alert(1)</script>"}}, "")
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("nav label was not escaped: %s", out)
	}
}

func TestVerdictBannerReflectsSeverity(t *testing.T) {
	out := verdictBanner("All healthy", "cpu 4%  mem 40%", diag.OK)
	if !strings.Contains(out, `verdict ok`) || !strings.Contains(out, "All healthy") {
		t.Errorf("verdictBanner(ok) = %s", out)
	}
	out = verdictBanner("Bottleneck found", "swap 91%", diag.Critical)
	if !strings.Contains(out, `verdict critical`) {
		t.Errorf("verdictBanner(critical) missing the critical class: %s", out)
	}
}

func TestFindingsListEmptyIsFriendly(t *testing.T) {
	out := findingsList(nil)
	if !strings.Contains(strings.ToLower(out), "no findings") {
		t.Errorf("empty findings list should say so plainly, got: %s", out)
	}
}

func TestFindingsListRendersEachFindingWithItsFixes(t *testing.T) {
	out := findingsList([]diag.Finding{
		{Severity: diag.Critical, Title: "Swap thrashing", Detail: "91% full", Fixes: []string{"quit X", "reboot"}},
	})
	for _, want := range []string{"Swap thrashing", "91% full", "quit X", "reboot", "finding critical"} {
		if !strings.Contains(out, want) {
			t.Errorf("findingsList missing %q, got: %s", want, out)
		}
	}
}

func TestFindingsListEscapesUntrustedText(t *testing.T) {
	out := findingsList([]diag.Finding{{Title: "<img src=x onerror=alert(1)>"}})
	if strings.Contains(out, "<img") {
		t.Errorf("finding title was not escaped: %s", out)
	}
}

func TestRowEscapesBothSides(t *testing.T) {
	out := row("<b>label</b>", "<b>value</b>")
	if strings.Contains(out, "<b>label</b>") || strings.Contains(out, "<b>value</b>") {
		t.Errorf("row did not escape its inputs: %s", out)
	}
}

func TestUnavailablePageNamesTheModuleAndReason(t *testing.T) {
	out := unavailablePage("Advice", "no local or cloud LLM is reachable")
	if !strings.Contains(out, "Advice") || !strings.Contains(out, "no local or cloud LLM is reachable") {
		t.Errorf("unavailablePage = %s", out)
	}
}
