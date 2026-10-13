package advice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vitals/internal/diag"
	"vitals/internal/diskusage"
	"vitals/internal/doctor"
	"vitals/internal/llm"
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

func TestGenerateBuildsPromptCallsLLMAndStripsFabricatedSources(t *testing.T) {
	// Generate is the shared "report -> prompt -> reply, cleaned up" path
	// both `vitals advice` and the dashboard's advice page use — one
	// place for the strip-fabricated-sources backstop, not two.
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
		case "/api/chat":
			w.Write([]byte(`{"message":{"content":"Restart the app.\n\n## Sources\n(source: https://example.com/fake)\n"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()

	reportJSON := []byte(`{"verdict":"warning","findings":[{"title":"disk nearly full"}]}`)
	reply, err := Generate(reportJSON, llm.CompleteOptions{OllamaURL: ollama.URL})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(reply, "Restart the app.") {
		t.Errorf("Generate reply = %q, missing the real answer", reply)
	}
	if strings.Contains(strings.ToLower(reply), "source") {
		t.Errorf("Generate reply = %q, fabricated sources were not stripped", reply)
	}
}

func TestGenerateReturnsTheErrorWhenNoProviderIsReachable(t *testing.T) {
	_, err := Generate([]byte(`{"verdict":"ok"}`), llm.CompleteOptions{OllamaURL: "http://127.0.0.1:1"})
	if err == nil {
		t.Error("Generate should return an error when no provider answers, not swallow it")
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

func TestBuildPromptAsksToSynthesizeNotJustRestateFindings(t *testing.T) {
	// A prompt that only asks for "prioritized advice" on a report the user
	// already has in front of them (each finding already carries its own
	// detail and fix) gives a capable model nothing to add beyond
	// paraphrasing — the exact "why would anyone use this" complaint. Push
	// it to find what a per-finding list can't show: shared root causes.
	prompt := BuildPrompt([]byte(`{"verdict":"warning","findings":[]}`))
	lower := strings.ToLower(prompt)
	for _, want := range []string{"restate", "root cause", "same process"} {
		if !strings.Contains(lower, want) {
			t.Errorf("prompt should push the model to add value beyond restating findings, missing %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPromptAsksNotToInventProblemsWhenHealthy(t *testing.T) {
	prompt := BuildPrompt([]byte(`{"verdict":"ok","findings":[]}`))
	if !strings.Contains(strings.ToLower(prompt), "healthy") {
		t.Errorf("prompt should tell the model not to invent problems on a healthy report:\n%s", prompt)
	}
}

func TestHeuristicRendersEveryFindingWithItsFixes(t *testing.T) {
	report := diag.Report{Findings: []diag.Finding{
		{Severity: diag.Critical, Title: "Swap heavily used", Detail: "swap 91% full", Fixes: []string{"restart Chrome", "reboot"}},
		{Severity: diag.Warn, Title: "Disk nearly full", Fixes: []string{"run vitals clean"}},
	}}
	out := Heuristic(report)
	for _, want := range []string{"Swap heavily used", "swap 91% full", "restart Chrome", "reboot", "Disk nearly full", "run vitals clean"} {
		if !strings.Contains(out, want) {
			t.Errorf("Heuristic missing %q, got:\n%s", want, out)
		}
	}
}

func TestHeuristicOnAHealthyReportSaysSo(t *testing.T) {
	out := Heuristic(diag.Report{})
	if !strings.Contains(strings.ToLower(out), "healthy") {
		t.Errorf("Heuristic on an empty report should say healthy, got:\n%s", out)
	}
}

func TestHeuristicOrdersMostSevereFirst(t *testing.T) {
	report := diag.Report{Findings: []diag.Finding{
		{Severity: diag.Warn, Title: "warn finding"},
		{Severity: diag.Critical, Title: "critical finding"},
	}}
	out := Heuristic(report)
	if strings.Index(out, "critical finding") > strings.Index(out, "warn finding") {
		t.Errorf("Heuristic should list the most severe finding first, got:\n%s", out)
	}
}

func TestNoLLMMessageOnAHealthyReportPointsAtTheNumbersNotFindings(t *testing.T) {
	// The actual bug this fixes: reusing the findings-list wording on a
	// healthy report left a dangling reference to a "rule-based findings
	// above" section that was never shown (just a bare "looks healthy"
	// line), which read as empty/unhelpful rather than as a real answer.
	msg := noLLMMessage(diag.Report{}, errors.New("connection refused"))
	if strings.Contains(msg, "findings above") {
		t.Errorf("noLLMMessage on a healthy report should not reference a findings list that was never shown, got: %q", msg)
	}
	if !strings.Contains(msg, "healthy") {
		t.Errorf("noLLMMessage on a healthy report should say so, got: %q", msg)
	}
}

func TestNoLLMMessageWithFindingsPointsBackAtThem(t *testing.T) {
	msg := noLLMMessage(diag.Report{Findings: []diag.Finding{{Title: "disk nearly full"}}}, errors.New("connection refused"))
	if !strings.Contains(msg, "findings above") {
		t.Errorf("noLLMMessage with real findings should point back at them, got: %q", msg)
	}
}

func TestBuildPromptInstructsOnMachineRelativeMemoryAndKVAllocation(t *testing.T) {
	prompt := BuildPrompt([]byte(`{"verdict":"ok"}`))
	lower := strings.ToLower(prompt)
	for _, want := range []string{"key-value", "kv", "allocator", "pressure", "kill"} {
		if !strings.Contains(lower, want) {
			t.Errorf("prompt missing memory guidance %q:\n%s", want, prompt)
		}
	}
}

func TestBuildPromptIncludesDiskUsage(t *testing.T) {
	payload := []byte(`{"report":{"verdict":"ok"},"disk_usage":{"root":"/Users/user","top_folders":[{"path":"/Users/user/.ollama","bytes":7381975040}]}}`)
	prompt := BuildPrompt(payload)
	if !strings.Contains(prompt, "/Users/user/.ollama") {
		t.Errorf("prompt should embed disk_usage data, got:\n%s", prompt)
	}
	if !strings.Contains(strings.ToLower(prompt), "disk usage") {
		t.Errorf("prompt should mention disk usage guidance, got:\n%s", prompt)
	}
}

func TestRunWithTextModeHealthyWithLLM(t *testing.T) {
	var buf strings.Builder
	deps := runDeps{
		assess: func(opts doctor.RunOptions) (doctor.Snapshot, diag.Report) {
			return doctor.Snapshot{}, diag.Report{}
		},
		generate: func(reportJSON []byte, opts llm.CompleteOptions) (string, error) {
			return "Keep system updated.", nil
		},
		out: &buf,
	}

	err := runWith(deps, Options{})
	if err != nil {
		t.Fatalf("runWith unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Keep system updated.") {
		t.Errorf("expected commentary body, got: %s", out)
	}
}

func TestRunWithTextModeWithFindingsAndNoLLM(t *testing.T) {
	var buf strings.Builder
	deps := runDeps{
		assess: func(opts doctor.RunOptions) (doctor.Snapshot, diag.Report) {
			return doctor.Snapshot{}, diag.Report{
				Findings: []diag.Finding{
					{Severity: diag.Critical, Title: "High Swap", Detail: "swap 90%", Fixes: []string{"reboot"}},
				},
			}
		},
		generate: func(reportJSON []byte, opts llm.CompleteOptions) (string, error) {
			return "", errors.New("no LLM running")
		},
		out: &buf,
	}

	err := runWith(deps, Options{})
	if err != nil {
		t.Fatalf("runWith unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Keep system updated.") {
		t.Errorf("expected no AI commentary body when LLM fails, got: %s", out)
	}
}

func TestRunWithScanAndJSON(t *testing.T) {
	var buf strings.Builder
	scanCalled := false
	deps := runDeps{
		assess: func(opts doctor.RunOptions) (doctor.Snapshot, diag.Report) {
			return doctor.Snapshot{}, diag.Report{}
		},
		scan: func(ctx context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
			scanCalled = true
			return diskusage.Result{
				Root:          root,
				Entries:       42,
				ObservedBytes: 1024 * 1024 * 50,
			}, nil
		},
		generate: func(reportJSON []byte, opts llm.CompleteOptions) (string, error) {
			if !strings.Contains(string(reportJSON), `"disk_usage"`) {
				t.Errorf("expected disk_usage in reportJSON, got: %s", string(reportJSON))
			}
			return "Disk usage looks reasonable.", nil
		},
		home: func() (string, error) {
			return "/Users/testuser", nil
		},
		out: &buf,
	}

	opts := Options{
		JSON:       true,
		Scan:       true,
		ScanBudget: "5s",
		Top:        5,
	}
	err := runWith(deps, opts)
	if err != nil {
		t.Fatalf("runWith: %v", err)
	}
	if !scanCalled {
		t.Errorf("expected disk scan to be called")
	}
	out := buf.String()
	if !strings.Contains(out, `"source": "heuristic+llm"`) {
		t.Errorf("expected source heuristic+llm in JSON output, got: %s", out)
	}
	if !strings.Contains(out, `"llm_advice": "Disk usage looks reasonable."`) {
		t.Errorf("expected llm_advice in JSON output, got: %s", out)
	}
}

func TestRunWithJSONErrorFallback(t *testing.T) {
	var buf strings.Builder
	deps := runDeps{
		assess: func(opts doctor.RunOptions) (doctor.Snapshot, diag.Report) {
			return doctor.Snapshot{}, diag.Report{}
		},
		generate: func(reportJSON []byte, opts llm.CompleteOptions) (string, error) {
			return "", errors.New("timeout connecting to LLM")
		},
		out: &buf,
	}

	err := runWith(deps, Options{JSON: true})
	if err != nil {
		t.Fatalf("runWith: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"source": "heuristic"`) {
		t.Errorf("expected source heuristic, got: %s", out)
	}
	if !strings.Contains(out, `"llm_error": "timeout connecting to LLM"`) {
		t.Errorf("expected llm_error in json, got: %s", out)
	}
}

func TestRunWithScanTextOutput(t *testing.T) {
	var buf strings.Builder
	deps := runDeps{
		assess: func(opts doctor.RunOptions) (doctor.Snapshot, diag.Report) {
			return doctor.Snapshot{}, diag.Report{}
		},
		scan: func(ctx context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
			return diskusage.Result{
				Root:          root,
				Entries:       1234,
				ObservedBytes: 1024 * 1024 * 100,
			}, nil
		},
		generate: func(reportJSON []byte, opts llm.CompleteOptions) (string, error) {
			return "Clean up temporary caches.", nil
		},
		home: func() (string, error) {
			return "/Users/testuser", nil
		},
		out: &buf,
	}

	err := runWith(deps, Options{Scan: true, Root: "/Users/testuser"})
	if err != nil {
		t.Fatalf("runWith: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "disk scan: 100.00 MB observed in 1234 entries") {
		t.Errorf("expected disk scan text output, got: %s", out)
	}
}

func TestRunDefaultDelegation(t *testing.T) {
	// Exercise Run with unreachable local endpoints in JSON mode.
	// It will run doctor.Assess, skip scan, fail generate, and output JSON.
	opts := Options{
		JSON:        true,
		OllamaURL:   "http://127.0.0.1:1",
		LMStudioURL: "http://127.0.0.1:1",
		LlamaCppURL: "http://127.0.0.1:1",
		VLLMURL:     "http://127.0.0.1:1",
	}
	err := Run(opts)
	if err != nil {
		t.Fatalf("Run default delegation error: %v", err)
	}
}
