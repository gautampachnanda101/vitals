package advice

import (
	"strings"
	"testing"
)

func TestBuildPromptIncludesTheReportAndAsksForPrioritizedAdvice(t *testing.T) {
	reportJSON := []byte(`{"verdict":"warning","findings":[{"title":"disk nearly full"}]}`)
	prompt := BuildPrompt(reportJSON)

	if !strings.Contains(prompt, "disk nearly full") {
		t.Errorf("prompt should embed the report JSON verbatim, got:\n%s", prompt)
	}
	for _, want := range []string{"vitals doctor", "prioritized", "concise"} {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(want)) {
			t.Errorf("prompt missing expected instruction %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPromptForbidsFabricatedSourcesOrCitations(t *testing.T) {
	// Small local models sometimes pattern-match onto a "Sources" section
	// from RAG-style training data and invent a placeholder URL, even
	// though this prompt supplies no source at all — say so explicitly.
	prompt := BuildPrompt([]byte(`{"verdict":"ok"}`))
	if !strings.Contains(strings.ToLower(prompt), "no external sources") && !strings.Contains(strings.ToLower(prompt), "cite") {
		t.Errorf("prompt should tell the model not to fabricate sources/citations, got:\n%s", prompt)
	}
}

func TestStripFabricatedSourcesRemovesATrailingHeading(t *testing.T) {
	cases := []string{
		"Restart the process.\n\n## Sources\n(source: https://example.com/fake)\n",
		"Restart the process.\n\nSources:\n- https://example.com/fake\n",
		"Restart the process.\n\n**Sources**\nhttps://example.com/fake\n",
		"Restart the process.\n\n### Sources\n1. https://example.com/fake\n",
	}
	for _, in := range cases {
		got := stripFabricatedSources(in)
		if strings.Contains(strings.ToLower(got), "source") {
			t.Errorf("stripFabricatedSources(%q) = %q, still contains a sources section", in, got)
		}
		if !strings.Contains(got, "Restart the process.") {
			t.Errorf("stripFabricatedSources(%q) = %q, removed the real answer too", in, got)
		}
	}
}

func TestStripFabricatedSourcesLeavesNormalTextAlone(t *testing.T) {
	in := "Restart the process to free swap. This resource discovers no other issues."
	if got := stripFabricatedSources(in); got != in {
		t.Errorf("stripFabricatedSources should not touch text with no sources section: got %q, want %q", got, in)
	}
}

func TestBuildPromptAsksNotToInventProblemsWhenHealthy(t *testing.T) {
	prompt := BuildPrompt([]byte(`{"verdict":"ok","findings":[]}`))
	if !strings.Contains(strings.ToLower(prompt), "healthy") {
		t.Errorf("prompt should tell the model not to invent problems on a healthy report:\n%s", prompt)
	}
}
