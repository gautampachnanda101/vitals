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
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"vitals/internal/ui"
)

// Options configures a diagnostic run.
type Options struct {
	OllamaURL string        // base URL of the Ollama server
	Watch     bool          // repeat until interrupted
	Interval  time.Duration // refresh period when Watch is set
	JSON      bool          // emit a machine-readable snapshot instead of a report
}

// Report is the machine-readable form emitted with --json.
type Report struct {
	Timestamp time.Time      `json:"timestamp"`
	Processes []ProcSnapshot `json:"processes"`
	Providers []Provider     `json:"providers"`
	Models    []ModelState   `json:"models"`
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
	Reachable bool     `json:"reachable"`
	Models    []string `json:"models,omitempty"`
	Err       string   `json:"error,omitempty"`
}

type ModelState struct {
	Provider   string  `json:"provider"`
	Name       string  `json:"name"`
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
	rep := Report{Timestamp: time.Now()}
	rep.Processes = scanProcesses()
	rep.Providers = probeProviders(opts.OllamaURL)
	rep.Models = ollamaModels(opts.OllamaURL)

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
	var out []ProcSnapshot
	for _, p := range ps {
		name, _ := p.Name()
		cmd, _ := p.Cmdline()
		tag := classify(name, cmd)
		if tag == "" {
			continue
		}
		cpu, _ := p.CPUPercent()
		var rss uint64
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			rss = mi.RSS
		}
		out = append(out, ProcSnapshot{
			PID: p.Pid, Name: name, CPUPct: cpu, RSSBytes: rss, Runtime: tag,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RSSBytes > out[j].RSSBytes })
	return out
}

// --- provider probing ----------------------------------------------------------

func probeProviders(ollamaURL string) []Provider {
	targets := []struct{ name, url, kind string }{
		{"Ollama", strings.TrimRight(ollamaURL, "/") + "/api/tags", "ollama"},
		{"LM Studio", "http://localhost:1234/v1/models", "openai"},
		{"llama.cpp / OpenAI :8080", "http://localhost:8080/v1/models", "openai"},
		{"vLLM :8000", "http://localhost:8000/v1/models", "openai"},
	}
	client := &http.Client{Timeout: 2 * time.Second}
	var out []Provider
	for _, t := range targets {
		p := Provider{Name: t.name, Endpoint: t.url}
		resp, err := client.Get(t.url)
		if err != nil {
			p.Err = "unreachable"
			out = append(out, p)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				p.Err = resp.Status
				return
			}
			p.Reachable = true
			p.Models = parseModelNames(resp, t.kind)
		}()
		out = append(out, p)
	}
	return out
}

func parseModelNames(resp *http.Response, kind string) []string {
	switch kind {
	case "ollama":
		var body struct {
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) != nil {
			return nil
		}
		names := make([]string, 0, len(body.Models))
		for _, m := range body.Models {
			names = append(names, m.Name)
		}
		return names
	default: // openai
		var body openAIModels
		if json.NewDecoder(resp.Body).Decode(&body) != nil {
			return nil
		}
		names := make([]string, 0, len(body.Data))
		for _, m := range body.Data {
			names = append(names, m.ID)
		}
		return names
	}
}

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
	ui.Header("LOCAL LLM DEEP INSIGHT")
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

	fmt.Printf("\n%sProvider endpoints%s\n", ui.Bold, ui.Reset)
	for _, pr := range rep.Providers {
		if pr.Reachable {
			ui.Okf("%-26s %s  (%d model%s)", pr.Name, pr.Endpoint, len(pr.Models), plural(len(pr.Models)))
		} else {
			fmt.Printf("  %s%-26s%s %s  (%s)\n", ui.Dim, pr.Name, ui.Reset, pr.Endpoint, pr.Err)
		}
	}

	fmt.Printf("\n%sOllama model memory allocation%s\n", ui.Bold, ui.Reset)
	if len(rep.Models) == 0 {
		fmt.Println("  no models currently resident in RAM/VRAM")
		return
	}
	for _, m := range rep.Models {
		fmt.Printf("  ├─ %s%s%s\n", ui.Bold, m.Name, ui.Reset)
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
		}
	}
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
