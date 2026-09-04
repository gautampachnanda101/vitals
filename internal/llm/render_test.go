package llm

import (
	"strings"
	"testing"
)

func TestRenderShowsHostProcessesTable(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Processes: []ProcSnapshot{
			{PID: 4242, Name: "ollama", Runtime: "ollama", CPUPct: 12.5, RSSBytes: 1 << 30},
		}})
	})
	if !strings.Contains(out, "4242") || !strings.Contains(out, "ollama") {
		t.Errorf("render should list a running LLM-runtime process, got:\n%s", out)
	}
}

func TestRenderNoProcessesWarns(t *testing.T) {
	out := captureStdout(t, func() { render(Report{}) })
	if !strings.Contains(out, "no local LLM runtime process found") {
		t.Errorf("render with no processes should say so, got:\n%s", out)
	}
}

func TestRenderProviderWithBlankLocationDefaultsToLocalGroup(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Providers: []Provider{
			{Name: "Ollama", Endpoint: "http://localhost:11434", Location: "", Reachable: true, Models: []string{"llama3.1:8b"}},
		}})
	})
	if !strings.Contains(out, "Local LLM endpoints") {
		t.Errorf("a blank Location should default to the local group, got:\n%s", out)
	}
}

func TestRenderShowsLatencyForAReachableProvider(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Providers: []Provider{
			{Name: "Ollama", Endpoint: "http://localhost:11434", Location: "local", Reachable: true, LatencyMS: 42},
		}})
	})
	if !strings.Contains(out, "42ms") {
		t.Errorf("render should show a reachable provider's latency, got:\n%s", out)
	}
}

func TestRenderShowsErrorForAnUnreachableProvider(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Providers: []Provider{
			{Name: "vLLM", Endpoint: "http://localhost:8000", Location: "local", Reachable: false, Err: "connection refused"}},
		})
	})
	if !strings.Contains(out, "connection refused") {
		t.Errorf("render should show an unreachable provider's error, got:\n%s", out)
	}
}

func TestRenderNoModelsSaysSo(t *testing.T) {
	out := captureStdout(t, func() { render(Report{}) })
	if !strings.Contains(out, "no models currently resident") {
		t.Errorf("render with no models should say so, got:\n%s", out)
	}
}

func TestRenderModelWithNoMemoryDetailShowsServedByRuntimeLine(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Models: []ModelState{{Provider: "LM Studio", Name: "qwen3", TotalBytes: 0}}})
	})
	if !strings.Contains(out, "no per-model memory detail exposed") {
		t.Errorf("a model with TotalBytes<=0 should show the no-detail line, got:\n%s", out)
	}
}

func TestRenderModelFullyOnGPUShowsOptimalInsight(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Models: []ModelState{{
			Provider: "Ollama", Name: "llama3.1:8b", TotalBytes: 8 << 30, VRAMBytes: 8 << 30, GPUOffload: 100,
		}}})
	})
	if !strings.Contains(out, "fully on GPU VRAM") {
		t.Errorf("a 100%% offloaded model should show the optimal insight, got:\n%s", out)
	}
}

func TestRenderModelPartiallyOffloadedWarns(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Models: []ModelState{{
			Provider: "Ollama", Name: "llama3.1:70b", TotalBytes: 40 << 30, VRAMBytes: 20 << 30, GPUOffload: 50, ExpiresAt: "5m",
		}}})
	})
	if !strings.Contains(out, "PARTIAL OFFLOAD") {
		t.Errorf("a partially offloaded model should warn, got:\n%s", out)
	}
	if !strings.Contains(out, "unload at") || !strings.Contains(out, "5m") {
		t.Errorf("a model with ExpiresAt set should show the unload-at line, got:\n%s", out)
	}
}

func TestRenderModelCPUOnlyWarnsAndOmitsUnloadAtWhenBlank(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Models: []ModelState{{
			Provider: "Ollama", Name: "llama3.1:70b", TotalBytes: 40 << 30, VRAMBytes: 0, GPUOffload: 0,
		}}})
	})
	if !strings.Contains(out, "CPU-ONLY") {
		t.Errorf("a 0%% offloaded model should warn CPU-ONLY, got:\n%s", out)
	}
	if strings.Contains(out, "unload at") {
		t.Errorf("a model with no ExpiresAt should not show an unload-at line, got:\n%s", out)
	}
}

func TestRenderCPUOnlyModelShowsGPUDriverMessageWhenChecked(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{
			Models:    []ModelState{{Provider: "Ollama", Name: "llama3.1:70b", TotalBytes: 40 << 30, GPUOffload: 0}},
			GPUDriver: GPUDriverStatus{Checked: true, Name: "nvidia-smi", Reachable: false, Err: "exit status 1"},
		})
	})
	if !strings.Contains(out, "gpu driver") || !strings.Contains(out, "nvidia-smi") {
		t.Errorf("a CPU-only model with a checked GPU driver status should show the driver message, got:\n%s", out)
	}
}

func TestRenderCPUOnlyModelOmitsGPUDriverLineWhenNotChecked(t *testing.T) {
	out := captureStdout(t, func() {
		render(Report{Models: []ModelState{{Provider: "Ollama", Name: "llama3.1:70b", TotalBytes: 40 << 30, GPUOffload: 0}}})
	})
	if strings.Contains(out, "gpu driver") {
		t.Errorf("an unchecked GPU driver status should print no driver line, got:\n%s", out)
	}
}
