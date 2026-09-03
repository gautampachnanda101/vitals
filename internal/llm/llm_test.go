package llm

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestClassify(t *testing.T) {
	cases := []struct{ name, cmd, want string }{
		{"ollama", "", "ollama"},
		{"", "/usr/local/bin/ollama serve", "ollama"},
		{"LM Studio", "", "lmstudio"},
		{"", "llama-server --model x.gguf", "llamacpp"},
		{"vllm", "", "vllm"},
		{"local-ai", "", "localai"},
		{"firefox", "firefox --new-window", ""},
	}
	for _, c := range cases {
		if got := classify(c.name, c.cmd); got != c.want {
			t.Errorf("classify(%q, %q) = %q, want %q", c.name, c.cmd, got, c.want)
		}
	}
}

func TestCapitalize(t *testing.T) {
	if got := capitalize(""); got != "" {
		t.Errorf("capitalize(\"\") = %q, want empty", got)
	}
	if got := capitalize("ollama"); got != "Ollama" {
		t.Errorf("capitalize(\"ollama\") = %q, want Ollama", got)
	}
}

func TestPluralLLM(t *testing.T) {
	if got := plural(1); got != "" {
		t.Errorf("plural(1) = %q, want empty", got)
	}
	if got := plural(2); got != "s" {
		t.Errorf("plural(2) = %q, want s", got)
	}
}

func TestNzLLM(t *testing.T) {
	if got := nz(""); got != "?" {
		t.Errorf("nz(\"\") = %q, want ?", got)
	}
	if got := nz("qwen"); got != "qwen" {
		t.Errorf("nz(\"qwen\") = %q, want it unchanged", got)
	}
}

func TestShortLocalName(t *testing.T) {
	cases := map[string]string{
		"LM Studio": "lmstudio",
		"llama.cpp": "llamacpp",
		"vLLM":      "vllm",
		"Ollama":    "Ollama", // unchanged — already short
	}
	for in, want := range cases {
		if got := shortLocalName(in); got != want {
			t.Errorf("shortLocalName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModelOrDefault(t *testing.T) {
	if got := modelOrDefault("custom-model", "OpenAI"); got != "custom-model" {
		t.Errorf("modelOrDefault(explicit) = %q, want the explicit model", got)
	}
	if got := modelOrDefault("", "OpenAI"); got != defaultModelFor("OpenAI") {
		t.Errorf("modelOrDefault(empty) = %q, want the provider default", got)
	}
}

func TestOllamaModelChoicePrefersOverrideThenResident(t *testing.T) {
	got, err := ollamaModelChoice("http://unreachable", "explicit-model", []ModelState{{Name: "resident-model"}})
	if err != nil || got != "explicit-model" {
		t.Errorf("ollamaModelChoice(override set) = %q, %v, want the override with no error", got, err)
	}
	got, err = ollamaModelChoice("http://unreachable", "", []ModelState{{Name: "resident-model"}})
	if err != nil || got != "resident-model" {
		t.Errorf("ollamaModelChoice(no override, resident present) = %q, %v, want the resident model", got, err)
	}
}

func TestOllamaModelChoiceErrorsWithNothingAvailable(t *testing.T) {
	// Port 1 refuses connections immediately — stands in for "no models
	// pulled and Ollama unreachable for the /api/tags fallback either".
	_, err := ollamaModelChoice("http://127.0.0.1:1", "", nil)
	if err == nil {
		t.Error("ollamaModelChoice with nothing available should error, not silently return empty")
	}
}

func TestParseModels(t *testing.T) {
	t.Run("openai-compatible /v1/models", func(t *testing.T) {
		body := []byte(`{"object":"list","data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`)
		got := parseModels(body, "openai")
		if !slices.Equal(got, []string{"gpt-4o", "gpt-4o-mini"}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("ollama /api/tags", func(t *testing.T) {
		body := []byte(`{"models":[{"name":"llama3.2:latest"},{"name":"qwen2.5:7b"}]}`)
		got := parseModels(body, "ollama")
		if !slices.Equal(got, []string{"llama3.2:latest", "qwen2.5:7b"}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("garbage yields nil", func(t *testing.T) {
		if parseModels([]byte("<html>nope"), "openai") != nil {
			t.Error("want nil for non-JSON")
		}
	})
	t.Run("empty list yields empty slice", func(t *testing.T) {
		if len(parseModels([]byte(`{"data":[]}`), "openai")) != 0 {
			t.Error("want no models")
		}
	})
}

func TestCloudTargets(t *testing.T) {
	t.Run("no env means no cloud targets", func(t *testing.T) {
		if got := cloudTargets(env(nil)); len(got) != 0 {
			t.Errorf("want none, got %d", len(got))
		}
	})
	t.Run("only providers with a key set are included", func(t *testing.T) {
		got := cloudTargets(env(map[string]string{
			"OPENAI_API_KEY":    "sk-a",
			"ANTHROPIC_API_KEY": "sk-b",
			"OLLAMA_API_KEY":    "sk-c",
		}))
		if len(got) != 3 {
			t.Fatalf("want 3 targets, got %d: %+v", len(got), got)
		}
		names := make([]string, len(got))
		for i, tg := range got {
			names[i] = tg.name
		}
		slices.Sort(names)
		if !slices.Equal(names, []string{"Anthropic", "Ollama Cloud", "OpenAI"}) {
			t.Errorf("unexpected providers: %v", names)
		}
		for _, tg := range got {
			if tg.location != "cloud" {
				t.Errorf("%s: location = %q, want cloud", tg.name, tg.location)
			}
			if tg.url == "" || tg.keyEnv == "" {
				t.Errorf("%s: incomplete target %+v", tg.name, tg)
			}
		}
	})
}

func TestAuthHeaders(t *testing.T) {
	t.Run("bearer", func(t *testing.T) {
		h := authHeaders(target{auth: "bearer", keyEnv: "OPENAI_API_KEY"},
			env(map[string]string{"OPENAI_API_KEY": "sk-abc"}))
		if h["Authorization"] != "Bearer sk-abc" {
			t.Errorf("got %v", h)
		}
	})
	t.Run("x-api-key with extra headers", func(t *testing.T) {
		tg := target{auth: "x-api-key", keyEnv: "ANTHROPIC_API_KEY",
			extra: map[string]string{"anthropic-version": "2023-06-01"}}
		h := authHeaders(tg, env(map[string]string{"ANTHROPIC_API_KEY": "sk-ant"}))
		if h["x-api-key"] != "sk-ant" || h["anthropic-version"] != "2023-06-01" {
			t.Errorf("got %v", h)
		}
	})
	t.Run("missing key sets no auth header", func(t *testing.T) {
		h := authHeaders(target{auth: "bearer", keyEnv: "OPENAI_API_KEY"}, env(nil))
		if _, ok := h["Authorization"]; ok {
			t.Errorf("should not set Authorization without a key: %v", h)
		}
	})
}

func TestProbeOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	}))
	defer srv.Close()

	tg := target{name: "Test", url: srv.URL, kind: "openai", auth: "bearer", keyEnv: "K"}

	t.Run("authorized", func(t *testing.T) {
		p := probeOne(srv.Client(), tg, env(map[string]string{"K": "sk-test"}))
		if !p.Reachable || !slices.Equal(p.Models, []string{"m1", "m2"}) {
			t.Errorf("got %+v", p)
		}
	})
	t.Run("auth failure is reported, not fatal", func(t *testing.T) {
		p := probeOne(srv.Client(), tg, env(nil))
		if p.Reachable || !strings.Contains(p.Err, "401") {
			t.Errorf("got %+v", p)
		}
	})
	t.Run("unreachable endpoint", func(t *testing.T) {
		dead := target{name: "Dead", url: "http://127.0.0.1:1/v1/models", kind: "openai"}
		p := probeOne(&http.Client{}, dead, env(nil))
		if p.Reachable || p.Err == "" {
			t.Errorf("got %+v", p)
		}
	})
}
