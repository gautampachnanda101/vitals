// Package llm gives deep, real-time diagnostics for locally hosted LLM runtimes.
//
// It combines each runtime's own API state (Ollama's /api/ps allocation engine,
// OpenAI-compatible /v1/models for LM Studio / llama.cpp / vLLM) with native OS
// process tracking via gopsutil: exact VRAM footprint, host CPU/RAM of the
// server process, and the precise percentage of each model offloaded to GPU.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"vitals/internal/ui"
)

// Base URLs for the local runtimes vitals probes. Overridable via Options,
// mainly so tests can point them at stub servers.
const (
	defaultOllamaURL   = "http://localhost:11434"
	defaultLMStudioURL = "http://localhost:1234"
	defaultLlamaCppURL = "http://localhost:8080"
	defaultVLLMURL     = "http://localhost:8000"
)

// Options configures a diagnostic run.
type Options struct {
	OllamaURL   string        // base URL of the Ollama server
	LMStudioURL string        // base URL of the LM Studio server
	LlamaCppURL string        // base URL of the llama.cpp / OpenAI server
	VLLMURL     string        // base URL of the vLLM server
	Watch       bool          // repeat until interrupted
	Interval    time.Duration // refresh period when Watch is set
	JSON        bool          // emit a machine-readable snapshot instead of a report
}

func (o Options) withDefaults() Options {
	if o.OllamaURL == "" {
		o.OllamaURL = defaultOllamaURL
	}
	if o.LMStudioURL == "" {
		o.LMStudioURL = defaultLMStudioURL
	}
	if o.LlamaCppURL == "" {
		o.LlamaCppURL = defaultLlamaCppURL
	}
	if o.VLLMURL == "" {
		o.VLLMURL = defaultVLLMURL
	}
	return o
}

// Report is the machine-readable form emitted with --json.
type Report struct {
	Timestamp time.Time       `json:"timestamp"`
	Processes []ProcSnapshot  `json:"processes"`
	Providers []Provider      `json:"providers"`
	Models    []ModelState    `json:"models"`
	GPUDriver GPUDriverStatus `json:"gpu_driver,omitempty"`
}

type ProcSnapshot struct {
	PID      int32   `json:"pid"`
	Name     string  `json:"name"`
	CPUPct   float64 `json:"cpu_percent"`
	RSSBytes uint64  `json:"rss_bytes"`
	Runtime  string  `json:"runtime"` // ollama | lmstudio | llamacpp | other
}

type Provider struct {
	Name      string   `json:"name"`
	Endpoint  string   `json:"endpoint"`
	Location  string   `json:"location,omitempty"` // local | cloud
	Reachable bool     `json:"reachable"`
	LatencyMS int64    `json:"latency_ms,omitempty"`
	Models    []string `json:"models,omitempty"`
	Err       string   `json:"error,omitempty"`
}

type ModelState struct {
	Provider   string  `json:"provider"`
	Name       string  `json:"name"`
	Resident   bool    `json:"resident"` // currently loaded in RAM/VRAM
	Family     string  `json:"family"`
	Quant      string  `json:"quantization"`
	ParamSize  string  `json:"parameter_size"`
	TotalBytes int64   `json:"total_bytes"`
	VRAMBytes  int64   `json:"vram_bytes"`
	GPUOffload float64 `json:"gpu_offload_percent"`
	ExpiresAt  string  `json:"expires_at,omitempty"`
}

// --- Ollama /api/ps schema ---------------------------------------------------

type ollamaPS struct {
	Models []struct {
		Name      string `json:"name"`
		Model     string `json:"model"`
		Size      int64  `json:"size"`
		SizeVRAM  int64  `json:"size_vram"`
		ExpiresAt string `json:"expires_at"`
		Details   struct {
			Family            string `json:"family"`
			ParameterSize     string `json:"parameter_size"`
			QuantizationLevel string `json:"quantization_level"`
		} `json:"details"`
	} `json:"models"`
}

type openAIModels struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Run executes the diagnostic, optionally looping.
func Run(opts Options) error {
	if opts.OllamaURL == "" {
		opts.OllamaURL = "http://localhost:11434"
	}
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}

	if !opts.Watch {
		return once(opts)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()

	for {
		if !opts.JSON {
			fmt.Print("\033[H\033[2J") // clear screen
		}
		if err := once(opts); err != nil {
			ui.Errf("%v", err)
		}
		select {
		case <-ctx.Done():
			fmt.Println()
			return nil
		case <-ticker.C:
		}
	}
}

func once(opts Options) error {
	opts = opts.withDefaults()
	rep := Report{Timestamp: time.Now()}
	rep.Processes = scanProcesses()
	rep.Providers = probeProviders(opts, os.Getenv)
	rep.Models = collectResidentModels(opts, rep.Providers)
	if needsGPUPreflightCheck(rep.Models) {
		rep.GPUDriver = checkGPUDriver()
	}

	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	render(rep)
	return nil
}

// --- process side ----------------------------------------------------------

var runtimeMatchers = []struct {
	tag     string
	needles []string
}{
	{"ollama", []string{"ollama"}},
	{"lmstudio", []string{"lm studio", "lmstudio", "lms-", "lms "}},
	{"llamacpp", []string{"llama-server", "llama.cpp", "llamacpp", "server --model"}},
	{"vllm", []string{"vllm"}},
	{"localai", []string{"local-ai", "localai"}},
}

func classify(name, cmd string) string {
	hay := strings.ToLower(name + " " + cmd)
	for _, m := range runtimeMatchers {
		for _, n := range m.needles {
			if strings.Contains(hay, n) {
				return m.tag
			}
		}
	}
	return ""
}

func scanProcesses() []ProcSnapshot {
	ps, err := process.Processes()
	if err != nil {
		return nil
	}
	// Prime CPU counters on the matching processes, let a short window elapse,
	// then read — otherwise gopsutil reports each process's lifetime-average
	// CPU, not what it is doing right now.
	type cand struct {
		p    *process.Process
		name string
		tag  string
	}
	var cands []cand
	for _, p := range ps {
		name, _ := p.Name()
		cmd, _ := p.Cmdline()
		tag := classify(name, cmd)
		if tag == "" {
			continue
		}
		_, _ = p.Percent(0)
		cands = append(cands, cand{p, name, tag})
	}
	time.Sleep(300 * time.Millisecond)

	var out []ProcSnapshot
	for _, c := range cands {
		cpuPct, _ := c.p.Percent(0)
		var rss uint64
		if mi, err := c.p.MemoryInfo(); err == nil && mi != nil {
			rss = mi.RSS
		}
		out = append(out, ProcSnapshot{
			PID: c.p.Pid, Name: c.name, CPUPct: cpuPct, RSSBytes: rss, Runtime: c.tag,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RSSBytes > out[j].RSSBytes })
	return out
}

// --- provider probing ----------------------------------------------------------

// target is one endpoint to probe: a local runtime or a cloud provider, always
// reached over an open API (OpenAI-compatible /v1/models, or Ollama's /api/tags).
type target struct {
	name     string
	url      string
	kind     string // "ollama" | "openai"
	auth     string // "" | "bearer" | "x-api-key"
	keyEnv   string
	extra    map[string]string // static extra headers (e.g. anthropic-version)
	location string            // "local" | "cloud"
}

// cloudRegistry lists well-known providers reachable over the open
// OpenAI-compatible API (Anthropic via its documented /v1/models). Each is
// probed only when its API-key environment variable is present, so corporate
// keys are picked up automatically and nothing is contacted otherwise.
var cloudRegistry = []target{
	{name: "Ollama Cloud", url: "https://ollama.com/api/tags", kind: "ollama", auth: "bearer", keyEnv: "OLLAMA_API_KEY", location: "cloud"},
	{name: "OpenAI", url: "https://api.openai.com/v1/models", kind: "openai", auth: "bearer", keyEnv: "OPENAI_API_KEY", location: "cloud"},
	{name: "Anthropic", url: "https://api.anthropic.com/v1/models", kind: "openai", auth: "x-api-key", keyEnv: "ANTHROPIC_API_KEY", extra: map[string]string{"anthropic-version": "2023-06-01"}, location: "cloud"},
	{name: "Groq", url: "https://api.groq.com/openai/v1/models", kind: "openai", auth: "bearer", keyEnv: "GROQ_API_KEY", location: "cloud"},
	{name: "Mistral", url: "https://api.mistral.ai/v1/models", kind: "openai", auth: "bearer", keyEnv: "MISTRAL_API_KEY", location: "cloud"},
	{name: "Together", url: "https://api.together.xyz/v1/models", kind: "openai", auth: "bearer", keyEnv: "TOGETHER_API_KEY", location: "cloud"},
	{name: "OpenRouter", url: "https://openrouter.ai/api/v1/models", kind: "openai", auth: "bearer", keyEnv: "OPENROUTER_API_KEY", location: "cloud"},
	{name: "DeepSeek", url: "https://api.deepseek.com/v1/models", kind: "openai", auth: "bearer", keyEnv: "DEEPSEEK_API_KEY", location: "cloud"},
	{name: "xAI", url: "https://api.x.ai/v1/models", kind: "openai", auth: "bearer", keyEnv: "XAI_API_KEY", location: "cloud"},
	{name: "Fireworks", url: "https://api.fireworks.ai/inference/v1/models", kind: "openai", auth: "bearer", keyEnv: "FIREWORKS_API_KEY", location: "cloud"},
	{name: "Gemini", url: "https://generativelanguage.googleapis.com/v1beta/openai/models", kind: "openai", auth: "bearer", keyEnv: "GEMINI_API_KEY", location: "cloud"},
}

func localTargets(opts Options) []target {
	opts = opts.withDefaults()
	trim := func(s string) string { return strings.TrimRight(s, "/") }
	return []target{
		{name: "Ollama", url: trim(opts.OllamaURL) + "/api/tags", kind: "ollama", location: "local"},
		{name: "LM Studio", url: trim(opts.LMStudioURL) + "/v1/models", kind: "openai", location: "local"},
		{name: "llama.cpp", url: trim(opts.LlamaCppURL) + "/v1/models", kind: "openai", location: "local"},
		{name: "vLLM", url: trim(opts.VLLMURL) + "/v1/models", kind: "openai", location: "local"},
	}
}

// cloudTargets returns the cloud providers whose API key is set in the given
// environment.
func cloudTargets(getenv func(string) string) []target {
	var out []target
	for _, t := range cloudRegistry {
		if strings.TrimSpace(getenv(t.keyEnv)) != "" {
			out = append(out, t)
		}
	}
	return out
}

// authHeaders renders the request headers for a target: any static extras, plus
// the credential in the provider's expected form when the key env var is set.
func authHeaders(t target, getenv func(string) string) map[string]string {
	h := make(map[string]string, len(t.extra)+1)
	for k, v := range t.extra {
		h[k] = v
	}
	key := strings.TrimSpace(getenv(t.keyEnv))
	if key == "" {
		return h
	}
	switch t.auth {
	case "bearer":
		h["Authorization"] = "Bearer " + key
	case "x-api-key":
		h["x-api-key"] = key
	}
	return h
}

// probeOne issues a single GET against a target and reports what came back. An
// auth failure or non-200 is recorded, never fatal.
func probeOne(client *http.Client, t target, getenv func(string) string) Provider {
	p := Provider{Name: t.name, Endpoint: t.url, Location: t.location}
	req, err := http.NewRequest(http.MethodGet, t.url, nil)
	if err != nil {
		p.Err = "bad endpoint"
		return p
	}
	for k, v := range authHeaders(t, getenv) {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		p.Err = "unreachable"
		return p
	}
	defer resp.Body.Close()
	p.LatencyMS = time.Since(start).Milliseconds()
	if resp.StatusCode != http.StatusOK {
		p.Err = resp.Status
		return p
	}
	p.Reachable = true
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	p.Models = parseModels(body, t.kind)
	return p
}

// probeProviders probes every target concurrently — each is an
// independent network round-trip (a local runtime or a cloud API), so
// there's no reason to pay N sequential timeouts when checking N targets;
// a review of `vitals dashboard`'s design flagged this exact loop as the
// worst-case tens-of-seconds cost of building its nav bar. Order in the
// result matches targets' order (not completion order), so callers that
// care about a stable/deterministic ordering (existing ones look up by
// Name, not position, but no need to make that a requirement) don't need
// to re-sort.
func probeProviders(opts Options, getenv func(string) string) []Provider {
	targets := append(localTargets(opts), cloudTargets(getenv)...)
	client := &http.Client{Timeout: 4 * time.Second}
	out := make([]Provider, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t target) {
			defer wg.Done()
			out[i] = probeOne(client, t, getenv)
		}(i, t)
	}
	wg.Wait()
	return out
}

// parseModels extracts model names from an open-API listing response body.
func parseModels(data []byte, kind string) []string {
	switch kind {
	case "ollama":
		var body struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if json.Unmarshal(data, &body) != nil {
			return nil
		}
		names := make([]string, 0, len(body.Models))
		for _, m := range body.Models {
			names = append(names, m.Name)
		}
		return names
	default: // openai-compatible
		var body openAIModels
		if json.Unmarshal(data, &body) != nil {
			return nil
		}
		names := make([]string, 0, len(body.Data))
		for _, m := range body.Data {
			names = append(names, m.ID)
		}
		return names
	}
}

// OllamaModels returns the models Ollama currently holds resident, with their
// GPU-offload percentage. Exported for `vitals doctor`. Empty when Ollama is
// not running or has nothing loaded.
func OllamaModels(ollamaURL string) []ModelState { return ollamaModels(ollamaURL) }

// collectResidentModels builds the unified list of loaded models across every
// reachable local runtime — not just Ollama. Ollama entries carry full
// VRAM / GPU-offload detail from /api/ps; other runtimes (LM Studio, llama.cpp,
// vLLM) contribute the models they are serving, which are by definition loaded,
// with whatever the open /v1/models endpoint exposes (name only). Cloud
// providers are omitted here — their catalogue lives on the provider line.
func collectResidentModels(opts Options, providers []Provider) []ModelState {
	var out []ModelState
	seen := map[string]bool{}
	add := func(m ModelState) {
		key := m.Provider + "\x00" + m.Name
		if m.Name == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, m)
	}

	for _, m := range ollamaModels(opts.OllamaURL) {
		m.Provider = "Ollama"
		m.Resident = true
		add(m)
	}

	for _, p := range providers {
		if !p.Reachable || p.Location != "local" || p.Name == "Ollama" {
			continue
		}
		for _, name := range p.Models {
			add(ModelState{Provider: p.Name, Name: name, Resident: true})
		}
	}
	return out
}

// ScanProcesses returns the running local LLM-runtime processes with current
// CPU and RSS. Exported for `vitals doctor`.
func ScanProcesses() []ProcSnapshot { return scanProcesses() }

func ollamaModels(ollamaURL string) []ModelState {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(ollamaURL, "/") + "/api/ps")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var ps ollamaPS
	if json.NewDecoder(resp.Body).Decode(&ps) != nil {
		return nil
	}
	out := make([]ModelState, 0, len(ps.Models))
	for _, m := range ps.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		var offload float64
		if m.Size > 0 {
			offload = float64(m.SizeVRAM) / float64(m.Size) * 100
		}
		out = append(out, ModelState{
			Provider:   "ollama",
			Name:       name,
			Family:     m.Details.Family,
			Quant:      m.Details.QuantizationLevel,
			ParamSize:  m.Details.ParameterSize,
			TotalBytes: m.Size,
			VRAMBytes:  m.SizeVRAM,
			GPUOffload: offload,
			ExpiresAt:  m.ExpiresAt,
		})
	}
	return out
}

// --- rendering ---------------------------------------------------------------

func render(rep Report) {
	ui.Header("LLM DEEP INSIGHT")
	fmt.Printf("  %s\n", rep.Timestamp.Format("2006-01-02 15:04:05"))

	fmt.Printf("\n%sHost processes%s\n", ui.Bold, ui.Reset)
	if len(rep.Processes) == 0 {
		ui.Warnf("no local LLM runtime process found (ollama, lm studio, llama.cpp, vllm...)")
	} else {
		fmt.Printf("  %-8s %-10s %-12s %-12s %s\n", "PID", "RUNTIME", "CPU", "HOST RAM", "PROCESS")
		ui.Rule()
		for _, p := range rep.Processes {
			fmt.Printf("  %-8d %-10s %-12s %-12s %s\n",
				p.PID, p.Runtime, fmt.Sprintf("%.1f%%", p.CPUPct),
				ui.HumanBytes(int64(p.RSSBytes)), p.Name)
		}
	}

	for _, group := range []string{"local", "cloud"} {
		var rows []Provider
		for _, pr := range rep.Providers {
			loc := pr.Location
			if loc == "" {
				loc = "local"
			}
			if loc == group {
				rows = append(rows, pr)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Printf("\n%s%s LLM endpoints%s\n", ui.Bold, capitalize(group), ui.Reset)
		for _, pr := range rows {
			if pr.Reachable {
				lat := ""
				if pr.LatencyMS > 0 {
					lat = fmt.Sprintf("  %dms", pr.LatencyMS)
				}
				ui.Okf("%-26s %s  (%d model%s)%s", pr.Name, pr.Endpoint, len(pr.Models), plural(len(pr.Models)), lat)
			} else {
				fmt.Printf("  %s%-26s%s %s  (%s)\n", ui.Dim, pr.Name, ui.Reset, pr.Endpoint, pr.Err)
			}
		}
	}

	fmt.Printf("\n%sLoaded models%s\n", ui.Bold, ui.Reset)
	if len(rep.Models) == 0 {
		fmt.Println("  no models currently resident in any local runtime")
		return
	}
	for _, m := range rep.Models {
		fmt.Printf("  ├─ %s%s%s  %s(%s)%s\n", ui.Bold, m.Name, ui.Reset, ui.Dim, m.Provider, ui.Reset)
		if m.TotalBytes <= 0 {
			// Runtime serves this model but exposes no memory breakdown over
			// the open API.
			fmt.Printf("  └─ served by this runtime; no per-model memory detail exposed\n")
			continue
		}
		fmt.Printf("  │    family/quant : %s (%s, %s)\n", nz(m.Family), nz(m.Quant), nz(m.ParamSize))
		fmt.Printf("  │    total size   : %s\n", ui.HumanBytes(m.TotalBytes))
		fmt.Printf("  │    VRAM portion : %s\n", ui.HumanBytes(m.VRAMBytes))
		fmt.Printf("  │    GPU offload  : %.1f%%\n", m.GPUOffload)
		if m.ExpiresAt != "" {
			fmt.Printf("  │    unload at    : %s\n", m.ExpiresAt)
		}
		fmt.Print("  └─ insight       : ")
		switch {
		case m.GPUOffload >= 99.5:
			ui.Okf("fully on GPU VRAM — optimal token throughput")
		case m.GPUOffload > 0:
			ui.Warnf("PARTIAL OFFLOAD — CPU<->GPU context shifting will spike CPU and slow generation; try a smaller quant (Q8_0 -> Q4_K_M) or fewer layers")
		default:
			ui.Warnf("CPU-ONLY — generation is bottlenecked on system RAM bandwidth; free VRAM or pick a smaller model")
			if msg := gpuPreflightMessage(rep.GPUDriver); msg != "" {
				fmt.Printf("  │    gpu driver   : %s\n", msg)
			}
		}
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func nz(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
