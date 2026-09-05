package guide

import (
	"strings"
	"testing"

	"vitals/internal/ui"
)

const sample = `# vitals — user guide

Some intro text with **bold** and ` + "`inline code`" + ` and a
[link](#anchor) in it.

## vitals doctor

- first bullet with **bold**
- second bullet with ` + "`code`" + `

### Trend detection

More text.

` + "```bash" + `
vitals doctor --json
` + "```" + `

Last paragraph.
`

func TestRenderTerminalStripsFenceMarkersAndKeepsCommandText(t *testing.T) {
	out := RenderTerminal(sample)
	if strings.Contains(out, "```") {
		t.Errorf("fence markers should not appear in terminal output:\n%s", out)
	}
	if !strings.Contains(out, "vitals doctor --json") {
		t.Errorf("fenced command text should be preserved:\n%s", out)
	}
}

func TestRenderTerminalAppliesBoldAndCodeStyling(t *testing.T) {
	out := RenderTerminal(sample)
	if !strings.Contains(out, ui.Bold+"bold"+ui.Reset) {
		t.Errorf("expected **bold** to render with ui.Bold, got:\n%s", out)
	}
	if !strings.Contains(out, ui.Dim+"inline code"+ui.Reset) {
		t.Errorf("expected `inline code` to render with ui.Dim, got:\n%s", out)
	}
}

func TestRenderTerminalDropsLinkURLKeepingText(t *testing.T) {
	out := RenderTerminal(sample)
	if strings.Contains(out, "(#anchor)") {
		t.Errorf("a terminal has nowhere to send an anchor link, so the URL should be dropped:\n%s", out)
	}
	if !strings.Contains(out, "link") {
		t.Errorf("the link's visible text should still appear:\n%s", out)
	}
}

func TestRenderTerminalRendersBulletsWithAMarker(t *testing.T) {
	out := RenderTerminal(sample)
	if !strings.Contains(out, "first bullet") || !strings.Contains(out, "second bullet") {
		t.Errorf("both bullets should be present:\n%s", out)
	}
}

func TestRenderTerminalHeadersAreDistinguishable(t *testing.T) {
	out := RenderTerminal(sample)
	for _, want := range []string{"vitals doctor", "Trend detection"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing header text %q:\n%s", want, out)
		}
	}
}

// TestRenderTerminalEveryHeaderLevelGetsColor guards a real regression: ###
// (h3) headers used to render with ui.Bold only, no color, so an LLM reply
// structured with a top-level "## Prioritized Advice" section and "###"
// sub-headings inside it (a common shape) showed its outer heading in cyan
// but every sub-heading in plain white — indistinguishable from body text.
// All three levels must carry ui.Cyan; h1 additionally gets an underline
// rule, h2/h3 don't, but color itself must never be h1/h2-only.
func TestRenderTerminalEveryHeaderLevelGetsColor(t *testing.T) {
	out := RenderTerminal(sample)
	for _, want := range []string{"vitals — user guide", "vitals doctor", "Trend detection"} {
		if !strings.Contains(out, ui.Bold+ui.Cyan+want+ui.Reset) {
			t.Errorf("header %q should render with ui.Bold+ui.Cyan, got:\n%s", want, out)
		}
	}
}

func TestRenderTerminalNeverPanicsOnEdgeCases(t *testing.T) {
	for _, in := range []string{"", "#", "```", "**", "`", "[]()", "- \n- \n"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderTerminal(%q) panicked: %v", in, r)
				}
			}()
			RenderTerminal(in)
		}()
	}
}

func TestRenderHTMLProducesWellFormedTagsAndEscapesText(t *testing.T) {
	out := RenderHTML("# Title\n\nA & B < C with **bold** and `code`.\n", "Title")
	for _, want := range []string{"<h1", "Title", "&amp;", "&lt;", "<strong>bold</strong>", "<code>code</code>"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "A & B < C") {
		t.Error("raw & and < should have been escaped, not passed through verbatim")
	}
}

func TestRenderHTMLIncludesATitleTag(t *testing.T) {
	out := RenderHTML("# x", "My Guide")
	if !strings.Contains(out, "<title>My Guide</title>") {
		t.Errorf("expected a <title> tag, got:\n%s", out)
	}
}

func TestRenderHTMLRendersLinksAsAnchors(t *testing.T) {
	out := RenderHTML("[Extending memhogs](#extending-memhogs)", "t")
	if !strings.Contains(out, `<a href="#extending-memhogs">Extending memhogs</a>`) {
		t.Errorf("link not rendered as a real anchor:\n%s", out)
	}
}

func TestRenderHTMLRendersHTTPSAndMailtoLinks(t *testing.T) {
	out := RenderHTML("[repo](https://github.com/example/vitals) or [help](mailto:a@b.com)", "t")
	if !strings.Contains(out, `<a href="https://github.com/example/vitals">repo</a>`) {
		t.Errorf("https link should pass through unchanged:\n%s", out)
	}
	if !strings.Contains(out, `<a href="mailto:a@b.com">help</a>`) {
		t.Errorf("mailto link should pass through unchanged:\n%s", out)
	}
}

func TestRenderHTMLNeutralizesUnsafeLinkSchemes(t *testing.T) {
	// This is a real XSS path, not a hypothetical: RenderFragment (this
	// same renderInlineHTML) is used to render vitals dashboard's advice
	// page, which shows LLM-generated text — a model coaxed into echoing
	// a markdown link with a javascript: href must not produce a live,
	// clickable exploit. html.EscapeString alone (already applied first)
	// blocks tag/attribute breakout but does nothing about URL scheme.
	cases := []string{
		"[click me](javascript:alert(document.cookie))",
		"[x](data:text/html,<script>alert(1)</script>)",
		"[x](vbscript:msgbox(1))",
	}
	for _, md := range cases {
		out := RenderHTML(md, "t")
		if strings.Contains(out, `href="javascript:`) || strings.Contains(out, `href="data:`) || strings.Contains(out, `href="vbscript:`) {
			t.Errorf("unsafe scheme reached a live href for input %q:\n%s", md, out)
		}
	}
}

func TestRenderFragmentNeutralizesUnsafeLinkSchemesInLLMOutput(t *testing.T) {
	out := RenderFragment("Restart the app. [details](javascript:alert(1))")
	if strings.Contains(out, `href="javascript:`) {
		t.Errorf("RenderFragment let a javascript: URL through — this is the path LLM advice output renders through:\n%s", out)
	}
	if !strings.Contains(out, "Restart the app.") {
		t.Errorf("the real text should still render, got:\n%s", out)
	}
}

func TestRenderHTMLGroupsBulletsIntoAList(t *testing.T) {
	out := RenderHTML("- one\n- two\n", "t")
	if !strings.Contains(out, "<ul>") || !strings.Contains(out, "</ul>") {
		t.Errorf("consecutive bullets should be wrapped in one <ul>:\n%s", out)
	}
	if strings.Count(out, "<li>") != 2 {
		t.Errorf("expected 2 <li> items, got:\n%s", out)
	}
}

func TestRenderHTMLPreservesFencedCodeVerbatimAndEscaped(t *testing.T) {
	out := RenderHTML("```bash\necho \"<hi>\" && true\n```\n", "t")
	if !strings.Contains(out, "<pre>") {
		t.Errorf("expected a <pre> block for fenced code:\n%s", out)
	}
	if !strings.Contains(out, "&lt;hi&gt;") {
		t.Errorf("code block content should be HTML-escaped:\n%s", out)
	}
	if strings.Contains(out, "<hi>") {
		t.Error("unescaped <hi> leaked into the HTML output")
	}
}

func TestRenderHTMLHeadersGetAnchorIDsMatchingSlugifiedText(t *testing.T) {
	out := RenderHTML("## vitals doctor\n\n### Trend detection\n", "t")
	if !strings.Contains(out, `<h2 id="vitals-doctor">`) {
		t.Errorf("expected an id on the h2 matching its slug, got:\n%s", out)
	}
	if !strings.Contains(out, `<h3 id="trend-detection">`) {
		t.Errorf("expected an id on the h3 matching its slug, got:\n%s", out)
	}
}

// TestRenderHTMLColorsEveryHeadingLevel guards the HTML-side counterpart
// of the ### terminal regression above: h3 previously had no color rule
// at all in pageTemplate (only h1/h2 got a colored border-bottom), so an
// h3 in a real rendered page was visually indistinguishable from body
// text — the exact "sub headings don't have color" complaint, just in
// this medium instead of the terminal.
func TestRenderHTMLColorsEveryHeadingLevel(t *testing.T) {
	out := RenderHTML(sample, "t")
	for _, selector := range []string{"h1, h2, h3 { line-height: 1.25; color: #2b5d53; }"} {
		if !strings.Contains(out, selector) {
			t.Errorf("expected every heading level to share a color rule, got:\n%s", out)
		}
	}
}

func TestRenderHTMLExistingAnchorLinkResolvesToARealID(t *testing.T) {
	// docs/user-guide.md itself contains this exact link; it must not be dead.
	out := RenderHTML("[Extending memhogs](#extending-memhogs)\n\n### Extending memhogs\n", "t")
	if !strings.Contains(out, `href="#extending-memhogs"`) {
		t.Fatalf("link href missing:\n%s", out)
	}
	if !strings.Contains(out, `id="extending-memhogs"`) {
		t.Errorf("no element carries the id the link points at — the link is dead:\n%s", out)
	}
}

func TestRenderHTMLIncludesATableOfContentsListingH2AndH3(t *testing.T) {
	out := RenderHTML(sample, "t")
	toc := extractBetween(t, out, `<nav class="toc">`, `</nav>`)
	if !strings.Contains(toc, `href="#vitals-doctor"`) {
		t.Errorf("TOC missing the H2 section link:\n%s", toc)
	}
	if !strings.Contains(toc, `href="#trend-detection"`) {
		t.Errorf("TOC missing the H3 subsection link:\n%s", toc)
	}
}

func TestRenderHTMLTOCExcludesTheDocumentTitleItself(t *testing.T) {
	out := RenderHTML(sample, "t")
	toc := extractBetween(t, out, `<nav class="toc">`, `</nav>`)
	if strings.Contains(toc, "user guide") {
		t.Errorf("the H1 document title should not appear as its own TOC entry:\n%s", toc)
	}
}

func TestRenderHTMLTOCComesBeforeTheRestOfTheBody(t *testing.T) {
	out := RenderHTML(sample, "t")
	tocIdx := strings.Index(out, `<nav class="toc">`)
	sectionIdx := strings.Index(out, `id="vitals-doctor"`)
	if tocIdx == -1 || sectionIdx == -1 || tocIdx > sectionIdx {
		t.Errorf("expected the TOC before the first section heading, got toc@%d section@%d", tocIdx, sectionIdx)
	}
}

func extractBetween(t *testing.T, s, start, end string) string {
	t.Helper()
	i := strings.Index(s, start)
	if i == -1 {
		t.Fatalf("marker %q not found in:\n%s", start, s)
	}
	j := strings.Index(s[i:], end)
	if j == -1 {
		t.Fatalf("marker %q not found after %q in:\n%s", end, start, s)
	}
	return s[i : i+j]
}

func TestRenderHTMLDisambiguatesDuplicateHeadingSlugs(t *testing.T) {
	// Two "## Example" headings must not collide on the same #example
	// anchor — GitHub's own convention (and this renderer's) is to
	// suffix the second one -1, the third -2, and so on.
	out := RenderHTML("## Example\n\n## Example\n\n## Example\n", "t")
	for _, want := range []string{`id="example"`, `id="example-1"`, `id="example-2"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected a disambiguated anchor %q, got:\n%s", want, out)
		}
	}
}

func TestBuildTOCClosesSubListWhenAnH2FollowsH3Children(t *testing.T) {
	out := RenderHTML("## One\n### Sub\n## Two\n", "t")
	if !strings.Contains(out, "</ul></li>") && !strings.Contains(out, "</ul>\n") {
		t.Errorf("expected the H3 sub-list to close before the second H2, got:\n%s", out)
	}
	// Both top-level entries must still be present and in order.
	iOne := strings.Index(out, `id="one"`)
	iTwo := strings.Index(out, `id="two"`)
	if iOne == -1 || iTwo == -1 || iOne > iTwo {
		t.Errorf("both H2 anchors should appear, One before Two, got:\n%s", out)
	}
}

func TestSlugifyMatchesGitHubStyleAnchors(t *testing.T) {
	cases := map[string]string{
		"vitals doctor":     "vitals-doctor",
		"Extending memhogs": "extending-memhogs",
		"vitals clean, dupes, tools, explore, live":       "vitals-clean-dupes-tools-explore-live",
		"Resource deep dives: cpu, mem, disk, net, power": "resource-deep-dives-cpu-mem-disk-net-power",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderHTMLNeverPanicsOnEdgeCases(t *testing.T) {
	for _, in := range []string{"", "#", "```", "**", "`", "[]()", "- \n- \n"} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("RenderHTML(%q) panicked: %v", in, r)
				}
			}()
			RenderHTML(in, "t")
		}()
	}
}
