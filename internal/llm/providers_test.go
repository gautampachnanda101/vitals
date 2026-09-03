package llm

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

// openAIModelsHandler serves a minimal /v1/models listing.
func openAIModelsHandler(ids ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[`))
		for i, id := range ids {
			if i > 0 {
				_, _ = w.Write([]byte(","))
			}
			_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
		}
		_, _ = w.Write([]byte(`]}`))
	}
}

// ollamaHandler serves /api/tags and /api/ps like a real Ollama server.
func ollamaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:3b"},{"name":"qwen2.5:7b"}]}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5:7b","size":5000000000,"size_vram":5000000000,
				"details":{"family":"qwen2","parameter_size":"7B","quantization_level":"Q4_K_M"}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestProbeProvidersRunsTargetsConcurrentlyNotSequentially(t *testing.T) {
	// Four slow-but-reachable targets at 150ms each: sequential probing
	// would take >=600ms, concurrent probing should finish close to
	// 150ms. This is exactly the "up to 20+ seconds on every dashboard
	// page load" cost the design review flagged — probing multiple
	// independent local runtimes has no reason to be sequential.
	slow := func() http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(150 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"m"}]}`))
		}
	}
	ollama := httptest.NewServer(func() http.HandlerFunc {
		h := slow()
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/tags" {
				h(w, r)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}
	}())
	defer ollama.Close()
	lmstudio := httptest.NewServer(slow())
	defer lmstudio.Close()
	llamacpp := httptest.NewServer(slow())
	defer llamacpp.Close()
	vllm := httptest.NewServer(slow())
	defer vllm.Close()

	opts := Options{OllamaURL: ollama.URL, LMStudioURL: lmstudio.URL, LlamaCppURL: llamacpp.URL, VLLMURL: vllm.URL}
	noEnv := func(string) string { return "" }

	start := time.Now()
	provs := probeProviders(opts, noEnv)
	elapsed := time.Since(start)

	if len(provs) < 4 {
		t.Fatalf("want at least 4 probed providers, got %d", len(provs))
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("probeProviders took %v for 4x150ms targets — looks sequential, want concurrent (~150ms)", elapsed)
	}
}

func TestProbeProvidersWithStubbedRuntimes(t *testing.T) {
	ollama := httptest.NewServer(ollamaHandler())
	defer ollama.Close()
	lmstudio := httptest.NewServer(openAIModelsHandler("lmstudio/model-a", "lmstudio/model-b"))
	defer lmstudio.Close()
	vllm := httptest.NewServer(openAIModelsHandler("meta-llama/Llama-3-8B"))
	defer vllm.Close()

	opts := Options{
		OllamaURL:   ollama.URL,
		LMStudioURL: lmstudio.URL,
		VLLMURL:     vllm.URL,
		LlamaCppURL: "http://127.0.0.1:1", // deliberately unreachable
	}
	noEnv := func(string) string { return "" } // no cloud keys

	provs := probeProviders(opts, noEnv)

	get := func(name string) (Provider, bool) {
		for _, p := range provs {
			if p.Name == name {
				return p, true
			}
		}
		return Provider{}, false
	}

	if p, ok := get("Ollama"); !ok || !p.Reachable || !slices.Equal(p.Models, []string{"llama3.2:3b", "qwen2.5:7b"}) {
		t.Errorf("Ollama probe: %+v", p)
	}
	if p, ok := get("LM Studio"); !ok || !p.Reachable || len(p.Models) != 2 {
		t.Errorf("LM Studio probe: %+v", p)
	}
	if p, ok := get("vLLM"); !ok || !p.Reachable || len(p.Models) != 1 {
		t.Errorf("vLLM probe: %+v", p)
	}
	if p, ok := get("llama.cpp"); !ok || p.Reachable || p.Err == "" {
		t.Errorf("llama.cpp should be unreachable: %+v", p)
	}
	for _, p := range provs {
		if p.Location == "cloud" {
			t.Errorf("no cloud provider should be probed without a key: %s", p.Name)
		}
	}

	// The unified resident-model list spans every reachable local runtime, with
	// Ollama's entry enriched from /api/ps and the others name-only.
	models := collectResidentModels(opts, provs)
	byKey := map[string]ModelState{}
	for _, m := range models {
		byKey[m.Provider+"/"+m.Name] = m
	}

	if q, ok := byKey["Ollama/qwen2.5:7b"]; !ok || q.TotalBytes == 0 || q.GPUOffload < 99 {
		t.Errorf("Ollama resident model not enriched from /api/ps: %+v", q)
	}
	if a, ok := byKey["LM Studio/lmstudio/model-a"]; !ok || !a.Resident || a.TotalBytes != 0 {
		t.Errorf("LM Studio model should be resident, name-only: %+v", a)
	}
	if _, ok := byKey["vLLM/meta-llama/Llama-3-8B"]; !ok {
		t.Errorf("vLLM served model missing from resident list")
	}
}
