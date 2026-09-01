//go:build integration

// Integration tests hit real network endpoints and are excluded from the
// default build. Run them with:  go test -tags=integration ./internal/llm/
package llm

import (
	"net/http"
	"os"
	"testing"
	"time"
)

// TestIntegrationOllamaCloud proves the open-API probe path works end to end
// against a real hosted endpoint. Skips unless OLLAMA_API_KEY is set (CI maps
// it from the OLLAMA_CLOUD_KEY repository secret).
func TestIntegrationOllamaCloud(t *testing.T) {
	if os.Getenv("OLLAMA_API_KEY") == "" {
		t.Skip("OLLAMA_API_KEY not set")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	tg := target{
		name:     "Ollama Cloud",
		url:      "https://ollama.com/api/tags",
		kind:     "ollama",
		auth:     "bearer",
		keyEnv:   "OLLAMA_API_KEY",
		location: "cloud",
	}

	p := probeOne(client, tg, os.Getenv)
	if !p.Reachable {
		t.Fatalf("Ollama Cloud not reachable with the provided key: %q", p.Err)
	}
	if p.LatencyMS <= 0 {
		t.Errorf("expected a positive latency measurement, got %d", p.LatencyMS)
	}
	if p.Location != "cloud" {
		t.Errorf("location = %q, want cloud", p.Location)
	}
	t.Logf("Ollama Cloud OK: %d models, %d ms", len(p.Models), p.LatencyMS)
}

// TestIntegrationUnreachableHost confirms a bad host degrades gracefully rather
// than erroring the whole run.
func TestIntegrationUnreachableHost(t *testing.T) {
	client := &http.Client{Timeout: 3 * time.Second}
	tg := target{name: "nope", url: "https://vitals-no-such-host.invalid/v1/models", kind: "openai"}
	p := probeOne(client, tg, os.Getenv)
	if p.Reachable || p.Err == "" {
		t.Errorf("expected unreachable with an error, got %+v", p)
	}
}
