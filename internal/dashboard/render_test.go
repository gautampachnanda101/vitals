package dashboard

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"vitals/internal/diag"
)

// wcagContrast returns the WCAG contrast ratio between two "#rrggbb"
// colors (https://www.w3.org/TR/WCAG21/#contrast-minimum). Used to pin
// that the dashboard's palette actually holds AAA-level contrast (>=7:1
// for normal text) rather than trusting an eyeballed color choice.
func wcagContrast(hex1, hex2 string) float64 {
	l1, l2 := relativeLuminance(hex1), relativeLuminance(hex2)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	r := channel(hex[0:2])
	g := channel(hex[2:4])
	b := channel(hex[4:6])
	return 0.2126*r + 0.7152*g + 0.0722*b
}

func channel(hexByte string) float64 {
	v, _ := strconv.ParseInt(hexByte, 16, 0)
	c := float64(v) / 255
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// Text colors and every background they can actually appear behind in
// pageShell/layout — see the comment above the :root block in render.go.
// A new background a text color gets placed on must be added here, not
// just visually checked, or a future edit can silently drop back below
// AAA the same way the original --muted value did.
const (
	mutedLight, mutedDark = "#4b4e53", "#b4b9bd"
	accentLight           = "#2b5d53" // also --ok
	accentDark            = "#6fbfa8" // also --ok
)

func TestPaletteMeetsWCAGAAAForNormalText(t *testing.T) {
	const aaa = 7.0 // WCAG 2.2 Level AAA, normal-size text (1.4.6)
	cases := []struct {
		name, fg, bg string
	}{
		{"muted/bg light", mutedLight, "#fbfaf7"},
		{"muted/surface light", mutedLight, "#ffffff"},
		{"muted/surface-2 light", mutedLight, "#f3f1ea"},
		{"muted/ok-bg light", mutedLight, "#e2eeea"},
		{"muted/warn-bg light", mutedLight, "#fbf1dc"},
		{"muted/crit-bg light", mutedLight, "#fbe9e3"},
		{"muted/bg dark", mutedDark, "#14171a"},
		{"muted/surface dark", mutedDark, "#1b1f23"},
		{"muted/surface-2 dark", mutedDark, "#1f242a"},
		{"muted/ok-bg dark", mutedDark, "#1b2b26"},
		{"muted/warn-bg dark", mutedDark, "#362b18"},
		{"muted/crit-bg dark", mutedDark, "#3a241f"},
		// accent/bg is also .btn:hover's background (render.go) — chosen
		// specifically because it was already verified here, unlike
		// --ok-bg, which a first attempt at .btn:hover used and which
		// actually fails AAA for accent-colored text (6.32:1/6.83:1,
		// caught by this test before it shipped).
		{"accent/bg light", accentLight, "#fbfaf7"},
		{"accent/surface light", accentLight, "#ffffff"},
		{"accent/bg dark", accentDark, "#14171a"},
		{"accent/surface dark", accentDark, "#1b1f23"},
	}
	for _, c := range cases {
		if got := wcagContrast(c.fg, c.bg); got < aaa {
			t.Errorf("%s: contrast %.2f:1, want >= %.0f:1 (fg %s on bg %s)", c.name, got, aaa, c.fg, c.bg)
		}
	}
}

func TestWCAGContrastKnownValues(t *testing.T) {
	// Pin the helper itself against WCAG's own published examples so a
	// bug in the math doesn't make TestPaletteMeetsWCAGAAAForNormalText
	// falsely pass.
	if got := wcagContrast("#000000", "#ffffff"); math.Abs(got-21.0) > 0.01 {
		t.Errorf("black/white contrast = %.4f, want 21.0", got)
	}
	if got := wcagContrast("#777777", "#777777"); math.Abs(got-1.0) > 0.01 {
		t.Errorf("identical colors contrast = %.4f, want 1.0", got)
	}
}

func TestLayoutHighlightsActiveNavItem(t *testing.T) {
	out := layout("Overview", "cpu", "1.2.3", []Module{
		{Slug: "", NavLabel: "Overview"},
		{Slug: "cpu", NavLabel: "CPU"},
	}, "<p>body</p>")

	if !strings.Contains(out, `<a href="/cpu" aria-current="page">CPU</a>`) {
		t.Errorf("active nav item not marked with aria-current, got:\n%s", out)
	}
	if !strings.Contains(out, `<a href="/">Overview</a>`) {
		t.Errorf("inactive nav item should have no attributes, got:\n%s", out)
	}
	if !strings.Contains(out, "<p>body</p>") {
		t.Error("body content missing from rendered page")
	}
}

func TestLayoutEscapesNavLabels(t *testing.T) {
	out := layout("t", "x", "1.0", []Module{{Slug: "x", NavLabel: "<script>alert(1)</script>"}}, "")
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("nav label was not escaped: %s", out)
	}
}

func TestLayoutShowsVersionInTheFooterAndFallsBackToDev(t *testing.T) {
	if out := layout("t", "", "1.2.3", nil, ""); !strings.Contains(out, "vitals 1.2.3") {
		t.Errorf("footer missing the version, got:\n%s", out)
	}
	if out := layout("t", "", "", nil, ""); !strings.Contains(out, "vitals dev") {
		t.Errorf("empty version should fall back to \"dev\", got:\n%s", out)
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

func TestVerdictBannerEscapesHeadlineAndSummary(t *testing.T) {
	out := verdictBanner("<script>alert(1)</script>", "<b>x</b>", diag.OK)
	if strings.Contains(out, "<script>alert(1)</script>") || strings.Contains(out, "<b>x</b>") {
		t.Errorf("verdictBanner did not escape headline/summary: %s", out)
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

func TestReportHeadlineUsesHealthyTextWhenNoFindings(t *testing.T) {
	if got := reportHeadline(diag.Report{}, "All good"); got != "All good" {
		t.Errorf("reportHeadline(empty) = %q, want the healthy text", got)
	}
}

func TestReportHeadlineUsesTheWorstFindingsTitle(t *testing.T) {
	var report diag.Report
	report.Add(diag.Finding{Severity: diag.Warn, Title: "RAM elevated"})
	report.Add(diag.Finding{Severity: diag.Critical, Title: "Swap thrashing"})
	if got := reportHeadline(report, "All good"); got != "Swap thrashing" {
		t.Errorf("reportHeadline = %q, want the worst finding's title", got)
	}
}

func TestUnavailablePageNamesTheModuleAndReason(t *testing.T) {
	out := unavailablePage("Advice", "no local or cloud LLM is reachable")
	if !strings.Contains(out, "Advice") || !strings.Contains(out, "no local or cloud LLM is reachable") {
		t.Errorf("unavailablePage = %s", out)
	}
}

func TestUnavailablePageEscapesItsArguments(t *testing.T) {
	out := unavailablePage("<script>alert(1)</script>", "<b>reason</b>")
	if strings.Contains(out, "<script>alert(1)</script>") || strings.Contains(out, "<b>reason</b>") {
		t.Errorf("unavailablePage did not escape its arguments: %s", out)
	}
}
