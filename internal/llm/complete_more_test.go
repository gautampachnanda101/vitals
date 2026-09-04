package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- defaultModelFor: the remaining known providers ------------------------

func TestDefaultModelForRemainingKnownProviders(t *testing.T) {
	cases := map[string]string{"Mistral": "mistral-large-latest", "DeepSeek": "deepseek-chat", "xAI": "grok-4"}
	for name, want := range cases {
		if got := defaultModelFor(name); got != want {
			t.Errorf("defaultModelFor(%q) = %q, want %q", name, got, want)
		}
	}
}

// --- completeLocal: Ollama reachable but genuinely has nothing to offer ----

func TestCompleteLocalErrorsWhenOllamaReachableButHasNoModelsAtAll(t *testing.T) {
	// /api/tags (reachability + ollamaAvailableModels) and /api/ps
	// (ollamaModels, resident) both report zero models, and no --model
	// override — ollamaModelChoice has nothing left to fall back to.
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[]}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()

	_, err := Complete("x", CompleteOptions{OllamaURL: ollama.URL})
	if err == nil {
		t.Error("expected an error when Ollama is reachable but has no models pulled or loaded")
	}
}

// --- probeModels: a request that can't even be constructed -----------------

func TestProbeModelsBadURLReturnsNilNotPanic(t *testing.T) {
	if got := probeModels(target{url: "://not a url", kind: "openai"}); got != nil {
		t.Errorf("probeModels(bad url) = %v, want nil", got)
	}
}

// --- completeNamed: every branch, not just the "found and reachable" one --

func TestCompleteNamedOllamaForcedSuccess(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
		case "/api/chat":
			w.Write([]byte(`{"message":{"content":"forced ollama reply"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()

	got, err := completeNamed("x", CompleteOptions{Provider: "ollama", OllamaURL: ollama.URL}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got != "forced ollama reply" {
		t.Errorf("got %q, want %q", got, "forced ollama reply")
	}
}

func TestCompleteNamedOllamaForcedButNoModelsErrors(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[]}`))
	}))
	defer ollama.Close()

	_, err := completeNamed("x", CompleteOptions{Provider: "ollama", OllamaURL: ollama.URL}, func(string) string { return "" })
	if err == nil {
		t.Error("expected an error when the forced Ollama target has no models")
	}
}

func TestCompleteNamedLocalNonOllamaNotReachableErrors(t *testing.T) {
	unreachable := "http://127.0.0.1:1"
	_, err := completeNamed("x", CompleteOptions{Provider: "lmstudio", LMStudioURL: unreachable}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("completeNamed(forced, unreachable) error = %v, want a \"not reachable\" error", err)
	}
}

func TestCompleteNamedLocalNonOllamaUsesFirstModelWhenNoOverride(t *testing.T) {
	llamacpp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"first-model"},{"id":"second-model"}]}`))
		case "/v1/chat/completions":
			w.Write([]byte(`{"choices":[{"message":{"content":"forced llama.cpp reply"}}]}`))
		}
	}))
	defer llamacpp.Close()

	got, err := completeNamed("x", CompleteOptions{Provider: "llamacpp", LlamaCppURL: llamacpp.URL}, func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if got != "forced llama.cpp reply" {
		t.Errorf("got %q, want %q", got, "forced llama.cpp reply")
	}
}

func TestCompleteNamedCloudProviderMissingAPIKeyErrors(t *testing.T) {
	orig := cloudRegistry
	cloudRegistry = []target{{name: "TestCloud", url: "http://unused/v1/models", kind: "openai", auth: "bearer", keyEnv: "TEST_CLOUD_KEY_UNSET", location: "cloud"}}
	defer func() { cloudRegistry = orig }()

	_, err := completeNamed("x", CompleteOptions{Provider: "TestCloud"}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "TEST_CLOUD_KEY_UNSET") {
		t.Errorf("error = %v, want it to name the missing env var", err)
	}
}

func TestCompleteNamedCloudProviderForcedSuccess(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"forced cloud reply"}}]}`))
	}))
	defer cloud.Close()

	orig := cloudRegistry
	cloudRegistry = []target{{name: "TestCloud", url: cloud.URL + "/v1/models", kind: "openai", auth: "bearer", keyEnv: "TEST_CLOUD_KEY", location: "cloud"}}
	defer func() { cloudRegistry = orig }()

	got, err := completeNamed("x", CompleteOptions{Provider: "TestCloud", Model: "test-model"}, func(k string) string {
		if k == "TEST_CLOUD_KEY" {
			return "sekrit"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "forced cloud reply" {
		t.Errorf("got %q, want %q", got, "forced cloud reply")
	}
}

func TestCompleteNamedUnknownProviderErrors(t *testing.T) {
	_, err := completeNamed("x", CompleteOptions{Provider: "not-a-real-provider"}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("error = %v, want an \"unknown provider\" error", err)
	}
}

// --- ollamaAvailableModels: non-200 and unparseable-body branches ----------

func TestOllamaAvailableModelsNon200ReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if got := ollamaAvailableModels(srv.URL); got != nil {
		t.Errorf("ollamaAvailableModels(500) = %v, want nil", got)
	}
}

func TestOllamaAvailableModelsUnparseableBodyReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()
	if got := ollamaAvailableModels(srv.URL); got != nil {
		t.Errorf("ollamaAvailableModels(bad json) = %v, want nil", got)
	}
}

// --- ollamaReachable: a request that can't even be constructed -------------

func TestOllamaReachableBadURLReturnsFalse(t *testing.T) {
	if ollamaReachable("://not a url") {
		t.Error("ollamaReachable(bad url) = true, want false")
	}
}

// --- parseOllamaChatResponse: the two remaining error branches -------------

func TestParseOllamaChatResponseUnparseableBodyErrors(t *testing.T) {
	if _, err := parseOllamaChatResponse([]byte("not json")); err == nil {
		t.Error("expected a parse error for an unparseable body")
	}
}

func TestParseOllamaChatResponseEmptyContentErrors(t *testing.T) {
	if _, err := parseOllamaChatResponse([]byte(`{"message":{"content":""}}`)); err == nil {
		t.Error("expected an error for an empty (but well-formed) response")
	}
}

// --- completeOllama: bad URL, unreachable, and non-200 branches ------------

func TestCompleteOllamaBadURLErrors(t *testing.T) {
	if _, err := completeOllama("://not a url", "model", "x"); err == nil {
		t.Error("expected an error constructing the request for a malformed URL")
	}
}

func TestCompleteOllamaUnreachableErrors(t *testing.T) {
	_, err := completeOllama("http://127.0.0.1:1", "model", "x")
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %v, want an \"unreachable\" error", err)
	}
}

func TestCompleteOllamaNon200Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	_, err := completeOllama(srv.URL, "model", "x")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want the server's body surfaced", err)
	}
}

// --- parseOpenAIChatResponse / parseAnthropicMessagesResponse: parse errors

func TestParseOpenAIChatResponseUnparseableBodyErrors(t *testing.T) {
	if _, err := parseOpenAIChatResponse([]byte("not json")); err == nil {
		t.Error("expected a parse error for an unparseable body")
	}
}

func TestParseAnthropicMessagesResponseUnparseableBodyErrors(t *testing.T) {
	if _, err := parseAnthropicMessagesResponse([]byte("not json")); err == nil {
		t.Error("expected a parse error for an unparseable body")
	}
}

func TestParseAnthropicMessagesResponseNoTextContentErrors(t *testing.T) {
	_, err := parseAnthropicMessagesResponse([]byte(`{"content":[{"type":"image","text":""}]}`))
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error = %v, want an \"empty response\" error when no text block is present", err)
	}
}

// --- completeCloud: the Anthropic-specific branch ---------------------------

func TestCompleteCloudAnthropicUsesTheMessagesParser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"content":[{"type":"text","text":"anthropic-shaped reply"}]}`))
	}))
	defer srv.Close()

	tgt := target{name: "Anthropic", url: srv.URL + "/v1/models", kind: "openai", auth: "x-api-key", keyEnv: "ANTHROPIC_API_KEY"}
	got, err := completeCloud(tgt, "claude-sonnet-5", "x", func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "sekrit"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "anthropic-shaped reply" {
		t.Errorf("got %q, want %q", got, "anthropic-shaped reply")
	}
}

// --- doComplete: bad URL, unreachable, and non-200 branches -----------------

func TestDoCompleteBadURLErrors(t *testing.T) {
	if _, err := doComplete("Test", "://not a url", nil, nil, parseOpenAIChatResponse); err == nil {
		t.Error("expected an error constructing the request for a malformed URL")
	}
}

func TestDoCompleteUnreachableErrors(t *testing.T) {
	_, err := doComplete("Test", "http://127.0.0.1:1", nil, nil, parseOpenAIChatResponse)
	if err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("error = %v, want an \"unreachable\" error naming the target", err)
	}
}

func TestDoCompleteNon200Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream exploded"))
	}))
	defer srv.Close()

	_, err := doComplete("Test", srv.URL, nil, nil, parseOpenAIChatResponse)
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("error = %v, want the server's body surfaced", err)
	}
}
