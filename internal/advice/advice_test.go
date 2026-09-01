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

func TestBuildPromptAsksNotToInventProblemsWhenHealthy(t *testing.T) {
	prompt := BuildPrompt([]byte(`{"verdict":"ok","findings":[]}`))
	if !strings.Contains(strings.ToLower(prompt), "healthy") {
		t.Errorf("prompt should tell the model not to invent problems on a healthy report:\n%s", prompt)
	}
}
