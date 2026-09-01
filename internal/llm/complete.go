package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// CompleteOptions configures a single advice request. Every local-runtime
// URL defaults the same way `vitals llm` does — none of them is ever
// assumed to be installed or running; each is only used after it actually
// answers.
type CompleteOptions struct {
	OllamaURL   string
	LMStudioURL string
	LlamaCppURL string
	VLLMURL     string
	Provider    string // force this provider name (case-insensitive); empty = auto-detect
	Model       string // override the provider's default model
}

func (o CompleteOptions) asOptions() Options {
	return Options{OllamaURL: o.OllamaURL, LMStudioURL: o.LMStudioURL, LlamaCppURL: o.LlamaCppURL, VLLMURL: o.VLLMURL}.withDefaults()
}

// completeTimeout bounds how long an advice request waits — generation can
// legitimately take a while on a large local model, but the command should
// never hang forever.
const completeTimeout = 60 * time.Second

// defaultModelFor returns a sensible default model for a well-known
// provider, or "" when there isn't one — callers must then require --model
// explicitly rather than guess at a name that might not exist.
func defaultModelFor(provider string) string {
	switch provider {
	case "OpenAI":
		return "gpt-4o-mini"
	case "Anthropic":
		return "claude-sonnet-5"
	case "Groq":
		return "llama-3.3-70b-versatile"
	case "Mistral":
		return "mistral-large-latest"
	case "DeepSeek":
		return "deepseek-chat"
	case "xAI":
		return "grok-4"
	default:
		return ""
	}
}

// Complete sends prompt to an LLM and returns its reply as plain text. Every
// local runtime `vitals llm` already knows about (Ollama, LM Studio,
// llama.cpp, vLLM) is tried in that order — none is ever assumed installed
// or running; each is used only once it actually answers with at least one
// model available. If none of them do, the first cloud provider with a
// configured API key is used. Provider, when set, forces that one choice
// instead of auto-detecting — an unreachable or unrecognised forced
// provider is an error, never a silent fallback to something else.
func Complete(prompt string, opts CompleteOptions) (string, error) {
	getenv := os.Getenv

	if opts.Provider != "" {
		return completeNamed(prompt, opts, getenv)
	}

	if reply, tried, err := completeLocal(prompt, opts); tried {
		return reply, err
	}

	cloud := cloudTargets(getenv)
	if len(cloud) == 0 {
		return "", fmt.Errorf("advice: no local LLM runtime reachable (checked Ollama, LM Studio, llama.cpp, vLLM) and no cloud API key set (OPENAI_API_KEY, ANTHROPIC_API_KEY, GROQ_API_KEY, ...)")
	}
	return completeCloud(cloud[0], modelOrDefault(opts.Model, cloud[0].name), prompt, getenv)
}

// completeLocal tries every local runtime target in registry order and uses
// the first one that is actually reachable and reports at least one
// available model. tried is false only when none of them answered at all,
// so the caller knows to move on to a cloud provider instead of treating a
// real error from a runtime that DID respond as "nothing to try".
func completeLocal(prompt string, opts CompleteOptions) (reply string, tried bool, err error) {
	local := localTargets(opts.asOptions())
	ollamaOpts := opts.asOptions()

	for _, t := range local {
		if t.kind == "ollama" {
			if !ollamaReachable(ollamaOpts.OllamaURL) {
				continue
			}
			model, mErr := ollamaModelChoice(ollamaOpts.OllamaURL, opts.Model, ollamaModels(ollamaOpts.OllamaURL))
			if mErr != nil {
				return "", true, mErr
			}
			reply, err = completeOllama(ollamaOpts.OllamaURL, model, prompt)
			return reply, true, err
		}

		models := probeModels(t)
		if len(models) == 0 {
			continue // unreachable, or reachable with nothing loaded — try the next one
		}
		model := opts.Model
		if model == "" {
			model = models[0]
		}
		reply, err = completeOpenAICompat(chatEndpoint(t), model, prompt, nil)
		return reply, true, err
	}
	return "", false, nil
}

// probeModels lists the models a local runtime target currently reports,
// or nil if it's unreachable — never an error, since "not installed" is the
// expected, common case for most of these targets on most machines.
func probeModels(t target) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return parseModels(buf.Bytes(), t.kind)
}

// completeNamed forces a specific provider by name, local or cloud.
func completeNamed(prompt string, opts CompleteOptions, getenv func(string) string) (string, error) {
	if strings.EqualFold(opts.Provider, "ollama") {
		ollamaURL := opts.asOptions().OllamaURL
		model, err := ollamaModelChoice(ollamaURL, opts.Model, ollamaModels(ollamaURL))
		if err != nil {
			return "", err
		}
		return completeOllama(ollamaURL, model, prompt)
	}
	for _, t := range localTargets(opts.asOptions()) {
		if t.kind != "ollama" && strings.EqualFold(shortLocalName(t.name), opts.Provider) {
			models := probeModels(t)
			if len(models) == 0 {
				return "", fmt.Errorf("advice: %s is not reachable at %s", t.name, t.url)
			}
			model := opts.Model
			if model == "" {
				model = models[0]
			}
			return completeOpenAICompat(chatEndpoint(t), model, prompt, nil)
		}
	}
	for _, t := range cloudRegistry {
		if strings.EqualFold(t.name, opts.Provider) {
			if strings.TrimSpace(getenv(t.keyEnv)) == "" {
				return "", fmt.Errorf("advice: %s requires %s to be set", t.name, t.keyEnv)
			}
			return completeCloud(t, modelOrDefault(opts.Model, t.name), prompt, getenv)
		}
	}
	return "", fmt.Errorf("advice: unknown provider %q", opts.Provider)
}

// shortLocalName maps a local target's display name to the --provider word
// a user would actually type for it.
func shortLocalName(name string) string {
	switch name {
	case "LM Studio":
		return "lmstudio"
	case "llama.cpp":
		return "llamacpp"
	case "vLLM":
		return "vllm"
	default:
		return name
	}
}

func modelOrDefault(model, provider string) string {
	if model != "" {
		return model
	}
	return defaultModelFor(provider)
}

// ollamaModelChoice picks the model to use for an advice request: an
// explicit override wins, then a currently-resident model (no load delay),
// then the first pulled-but-idle model from /api/tags (Ollama loads it on
// first use). Only errors when none of those exist.
func ollamaModelChoice(ollamaURL, override string, resident []ModelState) (string, error) {
	if override != "" {
		return override, nil
	}
	if len(resident) > 0 {
		return resident[0].Name, nil
	}
	if avail := ollamaAvailableModels(ollamaURL); len(avail) > 0 {
		return avail[0], nil
	}
	return "", fmt.Errorf("advice: Ollama is running but has no models pulled — `ollama pull <model>` first")
}

// ollamaAvailableModels lists every locally pulled model (not just the
// currently-loaded ones ollamaModels/api/ps reports) via /api/tags.
func ollamaAvailableModels(ollamaURL string) []string {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(ollamaURL, "/") + "/api/tags")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.NewDecoder(resp.Body).Decode(&tags) != nil {
		return nil
	}
	out := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		out = append(out, m.Name)
	}
	return out
}

func ollamaReachable(ollamaURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(ollamaURL, "/")+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// --- Ollama /api/chat --------------------------------------------------

func buildOllamaChatBody(model, prompt string) []byte {
	b, _ := json.Marshal(struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}{
		Model: model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{Role: "user", Content: prompt}},
		Stream: false,
	})
	return b
}

func parseOllamaChatResponse(body []byte) (string, error) {
	var r struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parse Ollama response: %w", err)
	}
	if r.Error != "" {
		return "", fmt.Errorf("ollama: %s", r.Error)
	}
	if r.Message.Content == "" {
		return "", fmt.Errorf("ollama: empty response")
	}
	return r.Message.Content, nil
}

func completeOllama(ollamaURL, model, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), completeTimeout)
	defer cancel()
	url := strings.TrimRight(ollamaURL, "/") + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buildOllamaChatBody(model, prompt)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama unreachable: %w", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned %s: %s", resp.Status, buf.String())
	}
	return parseOllamaChatResponse(buf.Bytes())
}

// --- OpenAI-compatible /v1/chat/completions -----------------------------

func buildOpenAIChatBody(model, prompt string) []byte {
	b, _ := json.Marshal(struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model: model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{Role: "user", Content: prompt}},
	})
	return b
}

func parseOpenAIChatResponse(body []byte) (string, error) {
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if r.Error.Message != "" {
		return "", fmt.Errorf("%s", r.Error.Message)
	}
	if len(r.Choices) == 0 {
		return "", fmt.Errorf("empty response (no choices)")
	}
	return r.Choices[0].Message.Content, nil
}

// --- Anthropic /v1/messages ----------------------------------------------

func buildAnthropicMessagesBody(model, prompt string) []byte {
	b, _ := json.Marshal(struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model:     model,
		MaxTokens: 1024,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{{Role: "user", Content: prompt}},
	})
	return b
}

func parseAnthropicMessagesResponse(body []byte) (string, error) {
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if r.Error.Message != "" {
		return "", fmt.Errorf("%s", r.Error.Message)
	}
	for _, c := range r.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("empty response (no text content)")
}

// --- cloud dispatch -------------------------------------------------------

// chatEndpoint derives a completion endpoint from a target's models-listing
// URL — Anthropic's real generation endpoint is /v1/messages (it is not
// OpenAI-compatible for completions, only for the models list this package
// probes elsewhere); every other "openai" kind uses /v1/chat/completions.
func chatEndpoint(t target) string {
	base := strings.TrimSuffix(t.url, "/models")
	if t.name == "Anthropic" {
		return base + "/messages"
	}
	return base + "/chat/completions"
}

func completeCloud(t target, model, prompt string, getenv func(string) string) (string, error) {
	headers := authHeaders(t, getenv)
	if t.name == "Anthropic" {
		return doComplete(t.name, chatEndpoint(t), buildAnthropicMessagesBody(model, prompt), headers, parseAnthropicMessagesResponse)
	}
	return doComplete(t.name, chatEndpoint(t), buildOpenAIChatBody(model, prompt), headers, parseOpenAIChatResponse)
}

// completeOpenAICompat posts to any OpenAI-compatible /v1/chat/completions
// endpoint — used for local runtimes (LM Studio, llama.cpp, vLLM), which
// typically need no auth headers at all.
func completeOpenAICompat(endpoint, model, prompt string, headers map[string]string) (string, error) {
	return doComplete(endpoint, endpoint, buildOpenAIChatBody(model, prompt), headers, parseOpenAIChatResponse)
}

// doComplete POSTs body to endpoint, adding headers, and hands the response
// to parse. name is used only to label errors (a provider's display name
// for cloud calls, or the endpoint itself for local ones, where there's no
// separate friendly name to show).
func doComplete(name, endpoint string, body []byte, headers map[string]string, parse func([]byte) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), completeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s unreachable: %w", name, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s returned %s: %s", name, resp.Status, buf.String())
	}
	return parse(buf.Bytes())
}
