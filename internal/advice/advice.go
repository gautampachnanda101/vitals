// Package advice turns a full `vitals doctor` snapshot into a prompt for a
// local or cloud LLM and prints the reply — "gather everything vitals
// already knows about this machine, hand it to a model, get plain-English
// advice back" instead of the user copy-pasting `vitals doctor --json`
// output into a chat window by hand.
package advice

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/guide"
	"vitals/internal/llm"
	"vitals/internal/ui"
)

// Options configures an advice request. Every local-runtime URL defaults
// the same way `vitals llm` does — none of Ollama, LM Studio, llama.cpp or
// vLLM is ever assumed to be installed or running; each is only used once
// it actually answers.
type Options struct {
	OllamaURL   string
	LMStudioURL string
	LlamaCppURL string
	VLLMURL     string
	Provider    string // force a provider ("ollama", "lmstudio", "openai", "anthropic", ...); empty = auto-detect
	Model       string // override the provider's default model
	JSON        bool   // emit {"advice": "..."} instead of plain text
}

// BuildPrompt turns a `vitals doctor --json` envelope into an LLM prompt
// asking for practical, prioritized advice on it.
func BuildPrompt(reportJSON []byte) string {
	return fmt.Sprintf(`You are a systems diagnostics assistant. Below is the JSON output of `+"`vitals doctor`"+`,
a cross-platform system-health tool covering CPU, memory, disk, network, power, GPU and any local
LLM runtime, plus a ranked list of findings with severities and suggested fixes.

The user has already seen each finding's own detail and fix — do not just restate them one by one.
Give prioritized advice that adds what a per-finding list cannot: if two or more findings trace back
to the same process or root cause, say so and give the one fix that addresses both; rank what matters
most when there is more than one issue. If a finding truly has nothing more to add beyond its own fix,
say so in one line instead of padding it out. A few concise sentences, not an essay.

If the report is healthy (verdict "ok", no findings), say so briefly and do not invent problems that
aren't there. There are no external sources for this — everything you need is in the JSON below. Do
not cite, reference, or invent a source, URL, or "Sources" section; base the answer only on this data.

%s`, reportJSON)
}

// sourcesHeadingRE matches a trailing "Sources" heading in any of the forms
// small models tend to produce (Markdown heading, bold, or a plain
// "Sources:" line), case-insensitively, anchored to the rest of the line so
// it doesn't fire on the word appearing mid-sentence.
var sourcesHeadingRE = regexp.MustCompile(`(?im)^\s*(#{1,6}\s*sources\s*|\*\*sources\*\*|sources:)\s*$`)

// stripFabricatedSources removes a trailing "Sources" section from an LLM
// reply. This prompt never supplies or asks for a source, but small local
// models sometimes fabricate one anyway — a citation-shaped pattern picked
// up from unrelated training data — regardless of an explicit instruction
// not to (they're unreliable at following negative instructions). Since
// vitals controls the whole pipeline and knows a priori there is never a
// real source to show, stripping it deterministically here is more
// reliable than hoping every model obeys the prompt.
func stripFabricatedSources(reply string) string {
	loc := sourcesHeadingRE.FindStringIndex(reply)
	if loc == nil {
		return reply
	}
	return strings.TrimRight(reply[:loc[0]], "\n ")
}

// Generate turns a `vitals doctor --json` envelope into a prompt, asks a
// local or cloud LLM for advice on it, and strips any fabricated
// "Sources" section from the reply. Shared by the CLI (Run, below) and
// `vitals dashboard`'s advice page — one place for the prompt and the
// strip-fabricated-sources backstop, not two copies that could drift.
func Generate(reportJSON []byte, opts llm.CompleteOptions) (string, error) {
	reply, err := llm.Complete(BuildPrompt(reportJSON), opts)
	if err != nil {
		return "", err
	}
	return stripFabricatedSources(reply), nil
}

// Run gathers the current doctor report and prints its rule-based
// findings and fixes immediately — doctor.Analyze's correlation engine,
// already computed above, the same "well-established troubleshooting
// technique" vitals doctor itself prints, needing no network call and no
// LLM to be useful. An LLM is then asked, if one is reachable, to add
// synthesis on top: shared root causes across findings, what matters
// most when there's more than one issue — a complement to the heuristic
// baseline, never a replacement for it, since the heuristic answer is
// what still works with no LLM configured at all. A local model can
// genuinely take a while to load and generate on a memory-constrained
// machine, so a status line prints before that (potentially slow) call,
// not just silence until it returns or times out at completeTimeout.
func Run(opts Options) error {
	snap, report := doctor.Assess(doctor.RunOptions{OllamaURL: opts.OllamaURL})
	reportJSON, err := json.Marshal(doctor.JSONReport(snap, report))
	if err != nil {
		return fmt.Errorf("build report: %w", err)
	}
	heuristic := Heuristic(report)

	if !opts.JSON {
		ui.Header("ADVICE")
		fmt.Println()
		fmt.Print(heuristic)
		ui.Infof("checking for a local or cloud LLM to add further commentary — this can take a while on a local model, especially the first response after it loads...")
	}

	reply, genErr := Generate(reportJSON, llm.CompleteOptions{
		OllamaURL:   opts.OllamaURL,
		LMStudioURL: opts.LMStudioURL,
		LlamaCppURL: opts.LlamaCppURL,
		VLLMURL:     opts.VLLMURL,
		Provider:    opts.Provider,
		Model:       opts.Model,
	})

	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		out := struct {
			HeuristicAdvice string `json:"heuristic_advice"`
			LLMAdvice       string `json:"llm_advice,omitempty"`
			LLMError        string `json:"llm_error,omitempty"`
			Source          string `json:"source"`
		}{HeuristicAdvice: heuristic, Source: "heuristic"}
		if genErr != nil {
			out.LLMError = genErr.Error()
		} else {
			out.LLMAdvice = reply
			out.Source = "heuristic+llm"
		}
		return enc.Encode(out)
	}

	if genErr != nil {
		ui.Warnf("no LLM reachable (%v) — showing the rule-based findings above only", genErr)
		return nil
	}
	fmt.Println()
	ui.Header("AI COMMENTARY")
	fmt.Println(guide.RenderTerminal(reply))
	return nil
}

// Heuristic renders report's findings and fixes as plain text — the same
// rule-based correlation `vitals doctor` already prints, the immediate,
// always-available half of `vitals advice`'s two-part answer (see Run)
// that needs no LLM at all. Most severe first, matching every other
// findings list in this codebase.
func Heuristic(report diag.Report) string {
	findings := report.SortedBySeverity()
	if len(findings) == 0 {
		return "Healthy — nothing needs attention.\n"
	}
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "- %s\n", f.Title)
		if f.Detail != "" {
			fmt.Fprintf(&b, "    %s\n", f.Detail)
		}
		for _, fix := range f.Fixes {
			fmt.Fprintf(&b, "    -> %s\n", fix)
		}
	}
	return b.String()
}
