package dashboard

import (
	"fmt"
	"html/template"

	"vitals/internal/doctor"
	"vitals/internal/llm"
)

func init() {
	Register(Module{Slug: "llm", NavLabel: "LLM Insight", Group: "Intelligence", Icon: iconLLM, Order: 75, Available: Always, Render: renderLLMInsight})
}

// renderLLMInsight shows local/cloud LLM provider reachability and each
// currently-loaded model's GPU-offload state — the same
// providers/host-CPU-correlated-offload signal `vitals llm` prints in a
// terminal, as a page. Both ctx.Providers and ctx.Snapshot.LLM are
// already part of every PageContext (the shared snapshotCache probes
// them for the Advice module's own nav-gating check), so this page adds
// no new live call of its own — a deliberately smaller LLMModel than
// `vitals llm`'s own richer per-model detail (family/quant/total size),
// which lives in llm.ModelState, not doctor.LLMModel; showing that would
// need its own new live call, not yet added.
func renderLLMInsight(ctx PageContext) string {
	body := `<div class="sectiontitle">Local endpoints</div>`
	local := providerRows(ctx.Providers, "local")
	if local == "" {
		body += `<p class="unavailable">No local LLM runtime reachable (Ollama, LM Studio, llama.cpp, vLLM).</p>`
	} else {
		body += local
	}

	if cloud := providerRows(ctx.Providers, "cloud"); cloud != "" {
		body += `<div class="sectiontitle">Cloud endpoints</div>` + cloud
	}

	body += `<div class="sectiontitle">Loaded models</div>`
	if len(ctx.Snapshot.LLM) == 0 {
		body += `<p class="unavailable">No models currently resident in any local runtime.</p>`
	} else {
		for _, m := range ctx.Snapshot.LLM {
			body += llmModelCard(m)
		}
	}
	return body
}

// providerRows renders every ctx.Providers entry whose Location matches
// location ("local" | "cloud"); empty Location counts as "local",
// matching render()'s own terminal grouping in internal/llm/llm.go.
func providerRows(providers []llm.Provider, location string) string {
	var out string
	for _, p := range providers {
		loc := p.Location
		if loc == "" {
			loc = "local"
		}
		if loc != location {
			continue
		}
		out += providerRow(p)
	}
	return out
}

var providerRowTmpl = template.Must(template.New("providerRow").Parse(
	`<div class="modelcard"><div><div class="mname">{{.Name}}</div><div class="msub">{{.Endpoint}}` +
		`{{if .Reachable}}{{if gt .LatencyMS 0}} — {{.LatencyMS}}ms{{end}}{{else}} — {{.Err}}{{end}}` +
		`{{if .Models}}</div><div class="msub mono">{{.ModelList}}{{end}}</div></div>` +
		`<span class="pill {{if .Reachable}}ok{{end}}">{{if .Reachable}}reachable{{else}}unreachable{{end}}</span></div>`))

func providerRow(p llm.Provider) string {
	models := ""
	for i, m := range p.Models {
		if i > 0 {
			models += ", "
		}
		models += m
	}
	return mustExecute(providerRowTmpl, struct {
		Name, Endpoint, Err, ModelList string
		Reachable                      bool
		LatencyMS                      int64
		Models                         []string
	}{p.Name, p.Endpoint, p.Err, models, p.Reachable, p.LatencyMS, p.Models})
}

var llmModelCardTmpl = template.Must(template.New("llmModelCard").Parse(
	`<div class="card"><div style="display:flex;justify-content:space-between;align-items:baseline;margin-bottom:.5rem">` +
		`<span style="font-weight:700;font-size:.95rem">{{.Name}}</span></div>` +
		`<div class="bar{{if .Warn}} warn{{end}}"><span style="width:{{.OffloadPct}}%"></span></div>` +
		`<div class="pill {{.PillClass}}" style="display:inline-block">{{.Insight}}</div></div>`))

func llmModelCard(m doctor.LLMModel) string {
	pillClass, insight, warn := "ok", fmt.Sprintf("fully on GPU VRAM — optimal token throughput (%.0f%%)", m.OffloadPct), false
	switch {
	case m.OffloadPct >= 99.5:
		// defaults above already cover this case
	case m.OffloadPct > 0:
		pillClass, warn = "warn", true
		insight = fmt.Sprintf("PARTIAL OFFLOAD (%.0f%%) — CPU↔GPU context shifting will spike CPU and slow generation", m.OffloadPct)
	default:
		pillClass, warn = "crit", true
		insight = fmt.Sprintf("CPU-ONLY — host CPU at %.0f%%; generation is bottlenecked on system RAM bandwidth", m.HostCPUPct)
	}
	return mustExecute(llmModelCardTmpl, struct {
		Name, PillClass, Insight string
		OffloadPct               float64
		Warn                     bool
	}{m.Name, pillClass, insight, m.OffloadPct, warn})
}
