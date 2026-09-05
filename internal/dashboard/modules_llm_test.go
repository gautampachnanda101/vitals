package dashboard

import (
	"strings"
	"testing"

	"vitals/internal/doctor"
	"vitals/internal/llm"
)

func TestLLMInsightModuleIsRegistered(t *testing.T) {
	m, exists, available := findModule("llm", PageContext{})
	if !exists {
		t.Fatal("llm module should be registered")
	}
	if !available {
		t.Error("llm module should always be available")
	}
	if m.NavLabel != "LLM Insight" || m.Group != "Intelligence" {
		t.Errorf("NavLabel/Group = %q/%q, want \"LLM Insight\"/\"Intelligence\"", m.NavLabel, m.Group)
	}
}

func TestRenderLLMInsightGroupsProvidersByLocation(t *testing.T) {
	out := renderLLMInsight(PageContext{Providers: []llm.Provider{
		{Name: "Ollama", Endpoint: "http://localhost:11434", Location: "local", Reachable: true, LatencyMS: 12, Models: []string{"llama3.1:8b"}},
		{Name: "OpenAI", Endpoint: "https://api.openai.com/v1/models", Location: "cloud", Reachable: true},
		{Name: "LM Studio", Endpoint: "http://localhost:1234", Location: "local", Reachable: false, Err: "connection refused"},
	}})
	for _, want := range []string{"Local endpoints", "Cloud endpoints", "Ollama", "OpenAI", "LM Studio", "llama3.1:8b", "12ms", "connection refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderLLMInsight missing %q, got: %s", want, out)
		}
	}
}

func TestRenderLLMInsightBlankLocationCountsAsLocal(t *testing.T) {
	out := renderLLMInsight(PageContext{Providers: []llm.Provider{
		{Name: "Ollama", Endpoint: "http://localhost:11434", Location: "", Reachable: true},
	}})
	if !strings.Contains(out, "Local endpoints") || strings.Contains(out, "Cloud endpoints") {
		t.Errorf("a blank Location should be grouped as local, got: %s", out)
	}
}

func TestRenderLLMInsightNoProvidersSaysSo(t *testing.T) {
	out := renderLLMInsight(PageContext{})
	if !strings.Contains(out, "No local LLM runtime reachable") {
		t.Errorf("renderLLMInsight with no providers should say so, got: %s", out)
	}
	if strings.Contains(out, "Cloud endpoints") {
		t.Errorf("no cloud providers should mean no Cloud endpoints section, got: %s", out)
	}
}

func TestRenderLLMInsightNoModelsSaysSo(t *testing.T) {
	out := renderLLMInsight(PageContext{})
	if !strings.Contains(out, "No models currently resident") {
		t.Errorf("renderLLMInsight with no models should say so, got: %s", out)
	}
}

func TestLLMModelCardReflectsOffloadTiers(t *testing.T) {
	full := llmModelCard(doctor.LLMModel{Name: "a", OffloadPct: 100})
	if !strings.Contains(full, "fully on GPU") {
		t.Errorf("100%% offload should say fully on GPU, got: %s", full)
	}
	partial := llmModelCard(doctor.LLMModel{Name: "b", OffloadPct: 40})
	if !strings.Contains(partial, "PARTIAL OFFLOAD") {
		t.Errorf("40%% offload should say PARTIAL OFFLOAD, got: %s", partial)
	}
	cpuOnly := llmModelCard(doctor.LLMModel{Name: "c", OffloadPct: 0, HostCPUPct: 88})
	if !strings.Contains(cpuOnly, "CPU-ONLY") || !strings.Contains(cpuOnly, "88%") {
		t.Errorf("0%% offload should say CPU-ONLY with the host CPU%%, got: %s", cpuOnly)
	}
}

func TestRenderLLMInsightEscapesUntrustedFields(t *testing.T) {
	out := renderLLMInsight(PageContext{
		Providers: []llm.Provider{{Name: "<script>alert(1)</script>", Endpoint: "x", Location: "local", Reachable: false, Err: "<img src=x>"}},
		Snapshot:  doctor.Snapshot{LLM: []doctor.LLMModel{{Name: "<b>bold</b>"}}},
	})
	for _, bad := range []string{"<script>alert(1)</script>", "<img src=x>", "<b>bold</b>"} {
		if strings.Contains(out, bad) {
			t.Errorf("renderLLMInsight did not escape %q, got: %s", bad, out)
		}
	}
}
