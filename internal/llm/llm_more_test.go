package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeOneBadEndpointReturnsErrorNotPanic(t *testing.T) {
	tg := target{name: "Bad", url: "://not a url", kind: "openai"}
	p := probeOne(&http.Client{}, tg, env(nil))
	if p.Reachable || p.Err == "" {
		t.Errorf("probeOne(bad url) = %+v, want a non-reachable result with an error set", p)
	}
}

func TestParseModelsOllamaGarbageYieldsNil(t *testing.T) {
	if got := parseModels([]byte("<html>nope"), "ollama"); got != nil {
		t.Errorf("parseModels(garbage, ollama) = %v, want nil", got)
	}
}

func TestCollectResidentModelsDedupesByProviderAndName(t *testing.T) {
	// A local non-ollama provider reporting the same model name twice
	// (e.g. two probes of the same runtime) must only appear once.
	providers := []Provider{
		{Name: "LM Studio", Location: "local", Reachable: true, Models: []string{"qwen3", "qwen3"}},
	}
	got := collectResidentModels(Options{OllamaURL: "http://127.0.0.1:1"}, providers)
	count := 0
	for _, m := range got {
		if m.Provider == "LM Studio" && m.Name == "qwen3" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("collectResidentModels should dedupe by provider+name, got %d qwen3 entries in %+v", count, got)
	}
}

func TestCollectResidentModelsSkipsBlankNames(t *testing.T) {
	providers := []Provider{
		{Name: "LM Studio", Location: "local", Reachable: true, Models: []string{""}},
	}
	got := collectResidentModels(Options{OllamaURL: "http://127.0.0.1:1"}, providers)
	for _, m := range got {
		if m.Name == "" {
			t.Errorf("collectResidentModels should skip blank model names, got %+v", got)
		}
	}
}

func TestCollectResidentModelsSkipsOllamaAndUnreachableAndCloud(t *testing.T) {
	providers := []Provider{
		{Name: "Ollama", Location: "local", Reachable: true, Models: []string{"should-be-skipped"}},
		{Name: "vLLM", Location: "local", Reachable: false, Models: []string{"unreachable-skip"}},
		{Name: "OpenAI", Location: "cloud", Reachable: true, Models: []string{"cloud-skip"}},
	}
	got := collectResidentModels(Options{OllamaURL: "http://127.0.0.1:1"}, providers)
	for _, m := range got {
		if m.Name == "should-be-skipped" || m.Name == "unreachable-skip" || m.Name == "cloud-skip" {
			t.Errorf("collectResidentModels should skip Ollama (handled separately)/unreachable/cloud providers, got %+v", got)
		}
	}
}

func TestOllamaModelsUnparseableBodyReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()
	if got := ollamaModels(srv.URL); got != nil {
		t.Errorf("ollamaModels(bad json) = %v, want nil", got)
	}
}

func TestOllamaModelsFallsBackToModelFieldWhenNameIsBlank(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[{"model":"llama3.1:8b","size":2000,"size_vram":1000}]}`))
	}))
	defer srv.Close()
	got := ollamaModels(srv.URL)
	if len(got) != 1 || got[0].Name != "llama3.1:8b" {
		t.Errorf("ollamaModels should fall back to the model field when name is blank, got %+v", got)
	}
}

func TestOnceRunsThePreflightCheckWhenAResidentModelLooksCPUBound(t *testing.T) {
	// A real end-to-end call through once() (not a fake), driving a real
	// Ollama-shaped resident model with near-zero GPU offload so
	// once()'s needsGPUPreflightCheck branch actually fires — same
	// "exercise the real integration once" style as this package's other
	// end-to-end tests (TestOnceGoesThroughTheRealDefaultsEndToEnd).
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[{"name":"llama3.1:70b","size":40000000000,"size_vram":0}]}`))
	}))
	defer ollama.Close()

	out := captureStdout(t, func() {
		if err := once(Options{OllamaURL: ollama.URL, LMStudioURL: "http://127.0.0.1:1", LlamaCppURL: "http://127.0.0.1:1", VLLMURL: "http://127.0.0.1:1"}); err != nil {
			t.Fatalf("once: %v", err)
		}
	})
	if !strings.Contains(out, "CPU-ONLY") {
		t.Errorf("once() with a CPU-bound resident model should reach the GPU-preflight-checked report, got:\n%s", out)
	}
}
