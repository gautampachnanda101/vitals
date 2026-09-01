package llm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultModelForKnownProviders(t *testing.T) {
	cases := map[string]string{"OpenAI": "gpt-4o-mini", "Anthropic": "claude-sonnet-5", "Groq": "llama-3.3-70b-versatile"}
	for name, want := range cases {
		if got := defaultModelFor(name); got != want {
			t.Errorf("defaultModelFor(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestDefaultModelForUnknownProviderIsEmpty(t *testing.T) {
	if got := defaultModelFor("SomeNewProvider"); got != "" {
		t.Errorf("defaultModelFor(unknown) = %q, want empty (caller must require --model)", got)
	}
}

func TestBuildOllamaChatBody(t *testing.T) {
	body := buildOllamaChatBody("llama3.1", "hello")
	for _, want := range []string{`"model":"llama3.1"`, `"content":"hello"`, `"stream":false`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("ollama chat body missing %q: %s", want, body)
		}
	}
}

func TestParseOllamaChatResponse(t *testing.T) {
	body := []byte(`{"model":"llama3.1","message":{"role":"assistant","content":"free some RAM"},"done":true}`)
	got, err := parseOllamaChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "free some RAM" {
		t.Errorf("got %q, want %q", got, "free some RAM")
	}
}

func TestParseOllamaChatResponseError(t *testing.T) {
	if _, err := parseOllamaChatResponse([]byte(`{"error":"model not found"}`)); err == nil {
		t.Error("expected an error when Ollama reports one")
	}
}

func TestBuildOpenAIChatBody(t *testing.T) {
	body := buildOpenAIChatBody("gpt-4o-mini", "hello")
	for _, want := range []string{`"model":"gpt-4o-mini"`, `"content":"hello"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("openai chat body missing %q: %s", want, body)
		}
	}
}

func TestParseOpenAIChatResponse(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"check disk space"}}]}`)
	got, err := parseOpenAIChatResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "check disk space" {
		t.Errorf("got %q, want %q", got, "check disk space")
	}
}

func TestParseOpenAIChatResponseNoChoicesErrors(t *testing.T) {
	if _, err := parseOpenAIChatResponse([]byte(`{"choices":[]}`)); err == nil {
		t.Error("expected an error with zero choices")
	}
}

func TestParseOpenAIChatResponseAPIErrorSurfacesMessage(t *testing.T) {
	_, err := parseOpenAIChatResponse([]byte(`{"error":{"message":"invalid api key"}}`))
	if err == nil || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("expected the API's own error message surfaced, got %v", err)
	}
}

func TestBuildAnthropicMessagesBody(t *testing.T) {
	body := buildAnthropicMessagesBody("claude-sonnet-5", "hello")
	for _, want := range []string{`"model":"claude-sonnet-5"`, `"content":"hello"`, `"max_tokens"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("anthropic body missing %q: %s", want, body)
		}
	}
}

func TestParseAnthropicMessagesResponse(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"quit chrome"}]}`)
	got, err := parseAnthropicMessagesResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "quit chrome" {
		t.Errorf("got %q, want %q", got, "quit chrome")
	}
}

func TestParseAnthropicMessagesResponseAPIErrorSurfacesMessage(t *testing.T) {
	_, err := parseAnthropicMessagesResponse([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	if err == nil || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("expected the API's own error message surfaced, got %v", err)
	}
}

func TestChatEndpointDerivesFromTheModelsURL(t *testing.T) {
	cases := []struct {
		name, url, kind, want string
	}{
		{"OpenAI", "https://api.openai.com/v1/models", "openai", "https://api.openai.com/v1/chat/completions"},
		{"Anthropic", "https://api.anthropic.com/v1/models", "openai", "https://api.anthropic.com/v1/messages"},
	}
	for _, c := range cases {
		got := chatEndpoint(target{name: c.name, url: c.url, kind: c.kind})
		if got != c.want {
			t.Errorf("chatEndpoint(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCompleteUsesLocalOllamaWhenReachable(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
		case "/api/chat":
			w.Write([]byte(`{"message":{"content":"advice from local model"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ollama.Close()

	got, err := Complete("what's wrong?", CompleteOptions{OllamaURL: ollama.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got != "advice from local model" {
		t.Errorf("got %q, want %q", got, "advice from local model")
	}
}

func TestCompleteFallsBackToLMStudioWhenOllamaIsNotInstalled(t *testing.T) {
	// Port 1 refuses connections immediately on every OS — stands in for
	// "Ollama isn't installed/running", never assumed reachable.
	unreachableOllama := "http://127.0.0.1:1"

	lmstudio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
		case "/v1/chat/completions":
			w.Write([]byte(`{"choices":[{"message":{"content":"advice from LM Studio"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer lmstudio.Close()

	got, err := Complete("what's wrong?", CompleteOptions{OllamaURL: unreachableOllama, LMStudioURL: lmstudio.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got != "advice from LM Studio" {
		t.Errorf("got %q, want %q", got, "advice from LM Studio")
	}
}

func TestCompleteProviderLMStudioForcesThatTarget(t *testing.T) {
	lmstudio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"local-model"}]}`))
		case "/v1/chat/completions":
			w.Write([]byte(`{"choices":[{"message":{"content":"forced LM Studio reply"}}]}`))
		}
	}))
	defer lmstudio.Close()

	got, err := Complete("x", CompleteOptions{Provider: "lmstudio", LMStudioURL: lmstudio.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got != "forced LM Studio reply" {
		t.Errorf("got %q, want %q", got, "forced LM Studio reply")
	}
}

func TestCompleteSkipsALocalRuntimeThatIsUpButHasNoModelLoaded(t *testing.T) {
	unreachableOllama := "http://127.0.0.1:1"

	// Reachable, but reports zero models — e.g. llama.cpp's server running
	// with nothing loaded yet. Must not be mistaken for "nothing to try".
	emptyLlamaCpp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[]}`))
	}))
	defer emptyLlamaCpp.Close()

	vllm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"served-model"}]}`))
		case "/v1/chat/completions":
			w.Write([]byte(`{"choices":[{"message":{"content":"advice from vLLM"}}]}`))
		}
	}))
	defer vllm.Close()

	got, err := Complete("x", CompleteOptions{
		OllamaURL:   unreachableOllama,
		LMStudioURL: unreachableOllama, // also unreachable
		LlamaCppURL: emptyLlamaCpp.URL, // reachable, but no models
		VLLMURL:     vllm.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "advice from vLLM" {
		t.Errorf("got %q, want %q — should have skipped the model-less llama.cpp server", got, "advice from vLLM")
	}
}

func TestCompleteFallsBackToCloudWhenNoLocalRuntime(t *testing.T) {
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/wrong Authorization header: %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"advice from cloud model"}}]}`))
	}))
	defer cloud.Close()

	orig := cloudRegistry
	cloudRegistry = []target{{name: "TestCloud", url: cloud.URL + "/v1/models", kind: "openai", auth: "bearer", keyEnv: "TEST_CLOUD_KEY", location: "cloud"}}
	defer func() { cloudRegistry = orig }()
	t.Setenv("TEST_CLOUD_KEY", "test-key")

	got, err := Complete("what's wrong?", CompleteOptions{OllamaURL: "http://127.0.0.1:1", Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "advice from cloud model" {
		t.Errorf("got %q, want %q", got, "advice from cloud model")
	}
}

func TestCompleteErrorsWithNoProviderAvailable(t *testing.T) {
	orig := cloudRegistry
	cloudRegistry = nil
	defer func() { cloudRegistry = orig }()

	_, err := Complete("x", CompleteOptions{OllamaURL: "http://127.0.0.1:1"})
	if err == nil {
		t.Error("expected an error when neither local nor cloud is available")
	}
}
