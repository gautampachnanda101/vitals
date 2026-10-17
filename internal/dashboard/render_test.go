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

// parseTokens reads the "--name:#rrggbb" pairs out of one of render.go's
// token blocks, so these tests check the colors the page actually ships
// rather than a hand-copied list that can drift from it. rgba() tokens
// (decorative lines, shadows) are skipped — none of them carry meaning.
func parseTokens(t *testing.T, block string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, decl := range strings.Split(block, ";") {
		name, val, ok := strings.Cut(strings.TrimSpace(decl), ":")
		if ok && strings.HasPrefix(name, "--") && strings.HasPrefix(val, "#") && len(val) == 7 {
			out[name] = val
		}
	}
	if len(out) < 30 {
		t.Fatalf("parsed only %d color tokens — has the token block's format changed?", len(out))
	}
	return out
}

// Text colors and every background they can actually be painted on in
// the dashboard. A new background a text color gets placed on must be
// added here, not just visually checked, or a future edit can silently
// drop back below AAA the same way an earlier --muted value did.
var aaaTextPairs = []struct{ fg, bg string }{
	{"--ink", "--bg"}, {"--ink", "--surface"}, {"--ink", "--surface-2"},
	{"--ink", "--ok-bg"}, {"--ink", "--warn-bg"}, {"--ink", "--crit-bg"},
	// --muted renders almost everywhere: captions, finding details, table
	// headers, the verdict summary line, inside pills.
	{"--muted", "--bg"}, {"--muted", "--surface"}, {"--muted", "--surface-2"},
	{"--muted", "--ok-bg"}, {"--muted", "--warn-bg"}, {"--muted", "--crit-bg"}, {"--muted", "--k8s-bg"},
	// --accent: links, content headings, the selected runtime pill/chip.
	{"--accent", "--bg"}, {"--accent", "--surface"}, {"--accent", "--surface-2"}, {"--accent", "--ok-bg"},
	// Status text (pills, warn/crit headline values) on their tints and cards.
	{"--ok-ink", "--ok-bg"}, {"--ok-ink", "--surface"},
	{"--warn-ink", "--warn-bg"}, {"--warn-ink", "--surface"}, {"--warn-ink", "--bg"},
	{"--crit-ink", "--crit-bg"}, {"--crit-ink", "--surface"}, {"--crit-ink", "--bg"},
	{"--k8s-ink", "--k8s-bg"}, {"--k8s-ink", "--surface"}, {"--k8s-ink", "--bg"},
	// The ink-navy sidebar, resting and hovered/current.
	{"--side-ink", "--side"}, {"--side-ink", "--side-hover"},
	{"--side-muted", "--side"}, {"--side-muted", "--side-hover"},
	{"--side-gold", "--side"}, {"--side-gold", "--side-hover"},
	// Primary (gold) buttons and text selection, resting and hovered.
	{"--btn-ink", "--btn"}, {"--btn-ink", "--btn-hover"},
}

// Non-text marks (WCAG 1.4.11, 3:1): status bars/dots/borders, control
// outlines, the gold accent rule and every chart slice, against each
// surface they're drawn on.
var nonTextMarks = []string{"--ok", "--warn", "--crit", "--k8s", "--gold", "--ctl", "--accent",
	"--c1", "--c2", "--c3", "--c4", "--c5", "--c6", "--c-other"}

func TestPaletteMeetsWCAGAAAForNormalText(t *testing.T) {
	const aaa = 7.0 // WCAG 2.2 Level AAA, normal-size text (1.4.6)
	for theme, block := range map[string]string{"light": lightTokens, "dark": darkTokens} {
		tok := parseTokens(t, block)
		for _, p := range aaaTextPairs {
			fg, bg := tok[p.fg], tok[p.bg]
			if fg == "" || bg == "" {
				t.Errorf("%s: token %s or %s missing", theme, p.fg, p.bg)
				continue
			}
			if got := wcagContrast(fg, bg); got < aaa {
				t.Errorf("%s %s on %s: contrast %.2f:1, want >= %.0f:1 (%s on %s)", theme, p.fg, p.bg, got, aaa, fg, bg)
			}
		}
	}
}

func TestPaletteMarksMeetNonTextContrast(t *testing.T) {
	const min = 3.0 // WCAG 1.4.11 non-text contrast
	for theme, block := range map[string]string{"light": lightTokens, "dark": darkTokens} {
		tok := parseTokens(t, block)
		for _, m := range nonTextMarks {
			for _, bg := range []string{"--bg", "--surface", "--surface-2"} {
				if got := wcagContrast(tok[m], tok[bg]); got < min {
					t.Errorf("%s %s on %s: contrast %.2f:1, want >= %.0f:1", theme, m, bg, got, min)
				}
			}
		}
	}
}

// The light/dark token blocks must define the same names, or a token set
// only in light silently keeps its light value in dark mode.
func TestThemeTokenSetsMatch(t *testing.T) {
	light, dark := parseTokens(t, lightTokens), parseTokens(t, darkTokens)
	for k := range light {
		if _, ok := dark[k]; !ok {
			t.Errorf("token %s is defined for light but not dark", k)
		}
	}
	for k := range dark {
		if _, ok := light[k]; !ok {
			t.Errorf("token %s is defined for dark but not light", k)
		}
	}
}

func TestPageShellThemingAndAccessibilityHooks(t *testing.T) {
	out := layoutLive("CPU", "cpu", "1.2.3", nil, "<p>x</p>", true)
	for _, want := range []string{
		`@media (prefers-color-scheme: dark){:root:not([data-theme="light"])`, // follows the OS by default
		`:root[data-theme="dark"]{`,            // explicit pin from the toggle
		`id="vitals-theme-toggle"`,             // the toggle itself
		`localStorage.getItem('vitals.theme')`, // pinned theme applied before paint
		`<a class="skip" href="#vitals-main">`, // skip link (2.4.1)
		`data-live="1" tabindex="-1"`,          // skip target can take focus
		`min-height:44px`,                      // AAA target size (2.5.5)
		`prefers-reduced-motion:reduce`,
		`[hidden]{display:none!important}`, // keeps hidden destructive buttons hidden
		`aria-pressed="true"`,              // live toggle exposes its state
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page shell missing %q", want)
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

// TestNavGroupsUsesCanonicalOrderRegardlessOfRegistrationOrder guards the
// sidebar's whole point: modules registered in any order (Order only
// controls position *within* a group) still render grouped by
// navGroupOrder's fixed section sequence.
func TestNavGroupsUsesCanonicalOrderRegardlessOfRegistrationOrder(t *testing.T) {
	groups := navGroups([]Module{
		{Slug: "clean", NavLabel: "Clean", Group: "Tools"},
		{Slug: "", NavLabel: "Overview", Group: "Overview"},
		{Slug: "cpu", NavLabel: "CPU", Group: "Resources"},
	}, "")
	if len(groups) != 3 {
		t.Fatalf("groups = %+v, want 3", groups)
	}
	var titles []string
	for _, g := range groups {
		titles = append(titles, g.Title)
	}
	if titles[0] != "Overview" || titles[1] != "Resources" || titles[2] != "Tools" {
		t.Errorf("group order = %v, want Overview, Resources, Tools (navGroupOrder's sequence)", titles)
	}
}

// TestNavGroupsFallsBackToOtherForAnUnknownGroup guards Module.Group's
// own documented behavior: a typo or unset Group must not silently drop
// the module from the nav.
func TestNavGroupsFallsBackToOtherForAnUnknownGroup(t *testing.T) {
	groups := navGroups([]Module{{Slug: "x", NavLabel: "X", Group: "Nonexistent"}}, "")
	if len(groups) != 1 || groups[0].Title != "Other" {
		t.Errorf("groups = %+v, want a single \"Other\" section", groups)
	}
	if len(groups[0].Items) != 1 || groups[0].Items[0].NavLabel != "X" {
		t.Errorf("the module should still appear in the fallback section, got %+v", groups[0].Items)
	}
}

// TestNavGroupsOmitsSectionsWithNoAvailableModules guards against an
// empty <div class="navgroup"><h4>Title</h4></div> — sections come from
// navGroupOrder, a fixed list, but not every machine offers a module in
// every section (e.g. no GPU/battery modules on a headless server).
func TestNavGroupsOmitsSectionsWithNoAvailableModules(t *testing.T) {
	groups := navGroups([]Module{{Slug: "cpu", NavLabel: "CPU", Group: "Resources"}}, "")
	for _, g := range groups {
		if g.Title == "Intelligence" || g.Title == "Tools" || g.Title == "System" {
			t.Errorf("empty section %q should be omitted, got %+v", g.Title, groups)
		}
	}
}

func TestLayoutRendersGroupHeadingsAndIcons(t *testing.T) {
	out := layout("t", "cpu", "1.0", []Module{
		{Slug: "cpu", NavLabel: "CPU", Group: "Resources", Icon: iconCPU},
	}, "")
	if !strings.Contains(out, `<h4>Resources</h4>`) {
		t.Errorf("group heading missing, got:\n%s", out)
	}
	if !strings.Contains(out, `<svg viewBox="0 0 24 24">`) {
		t.Errorf("nav icon missing, got:\n%s", out)
	}
}

func TestLayoutOmitsIconMarkupWhenModuleHasNone(t *testing.T) {
	out := layout("t", "", "1.0", []Module{{Slug: "x", NavLabel: "X", Group: "Tools"}}, "")
	// Scope to the nav: the top bar's theme toggle carries its own icons.
	nav := out[strings.Index(out, `<nav aria-label="Primary">`):strings.Index(out, "</nav>")]
	if strings.Contains(nav, "<svg") {
		t.Errorf("no <svg> should render for a module with no Icon, got:\n%s", nav)
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

// TestPageShellColorsHeadingTags guards a real regression: the page shell
// used to carry no h1/h2/h3 CSS at all, so any Markdown-derived content
// embedded via guide.RenderFragment (the AI-commentary card, currently
// the only such content, and anything future) rendered its headings in
// plain body-text color — no visual hierarchy at all, not even the
// hardcoded "AI commentary" <h3> title got a real accent color. --accent
// is already proven AAA-contrast as text color against both --bg and
// --surface (TestPaletteMeetsWCAGAAAForNormalText), so reusing it here
// needs no new contrast check.
func TestPageShellColorsHeadingTags(t *testing.T) {
	out := layout("t", "", "", nil, "<h3>x</h3>")
	if !strings.Contains(out, "h1,h2,h3{color:var(--accent)") {
		t.Errorf("page shell should style every heading level with the accent color, got:\n%s", out)
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

// TestFindingsListEmptyIsBlank guards a real regression: every caller
// already leads with a verdictBanner whose own headline says "nothing's
// wrong" — a second "No findings — this looks healthy" card right below
// it just repeated the same fact the reader had already read. findingsList
// now returns "" so callers (via findingsCard) skip the redundant card
// entirely instead of rendering an empty or restating one.
func TestFindingsListEmptyIsBlank(t *testing.T) {
	if out := findingsList(nil); out != "" {
		t.Errorf("findingsList(nil) = %q, want empty", out)
	}
}

func TestFindingsCardSkipsTheWrapperWhenTheresNothingToShow(t *testing.T) {
	if out := findingsCard(nil); out != "" {
		t.Errorf("findingsCard(nil) = %q, want empty (no redundant card)", out)
	}
}

func TestFindingsCardWrapsRealFindingsInACard(t *testing.T) {
	out := findingsCard([]diag.Finding{{Severity: diag.Warn, Title: "RAM elevated"}})
	if !strings.Contains(out, `<div class="card">`) || !strings.Contains(out, "RAM elevated") {
		t.Errorf("findingsCard should wrap real findings in a .card, got: %s", out)
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
