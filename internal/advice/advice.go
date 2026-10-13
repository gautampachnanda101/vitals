// Package advice turns a full `vitals doctor` snapshot into a prompt for a
// local or cloud LLM and prints the reply — "gather everything vitals
// already knows about this machine, hand it to a model, get plain-English
// advice back" instead of the user copy-pasting `vitals doctor --json`
// output into a chat window by hand.
package advice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"vitals/internal/diag"
	"vitals/internal/diskusage"
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
	Scan        bool   // run deep disk usage scan to inform advice
	Root        string // root directory for disk usage scan
	ScanBudget  string // duration budget for disk usage scan
	Top         int    // number of top files/dirs to report
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

Guidelines for accurate, evidence-based systems advice:
- Machine-relative memory evaluation: Always evaluate memory usage, swap, and process sizes relative
  to total host capacity and memory pressure (available RAM, swap usage, swap thrashing). On large-memory
  machines with ample headroom, resident memory growth is often harmless caching.
- Caching vs leaks: Distinguish normal caching, buffer pools, and allocator behavior from true memory leaks.
  For example, local LLM runtimes (llama-server, ollama, vllm) allocate Key-Value (KV) cache slots and
  attention buffers across interactions and hold them resident; modern C/C++ allocators retain peak heap
  pages rather than returning them to the OS immediately. Databases and VMs behave similarly.
  Never recommend blind process termination (`+"`kill <pid>`"+`) as the first resort for caching runtimes;
  recommend tuning context length, batch size, slot allocation, or graceful application restarts.
- Disk usage synthesis: If "disk_usage" details (top directories, model blobs, or large single files) are
  present, synthesize them with any disk findings to recommend concrete, safe remediation:
  * For application/build caches: recommend running `+"`vitals clean`"+` or using the Clean dashboard page.
  * For container/VM disk images (e.g. Docker.raw, com.docker.docker, virtual machine disks): note that
    sparse files on disk can have an allocated physical footprint much smaller than their logical maximum capacity.
    Cross-reference with any container runtime data in the snapshot: check running containers, stopped containers,
    and advise using `+"`docker system df`"+` to inspect space allocation, `+"`docker system prune`"+` to clean unused
    build cache and images, and `+"`docker volume prune`"+` to reclaim orphaned volumes that standard prune leaves behind.
  * For local AI/LLM model weights (e.g. .ollama, models, huggingface): advise managing models via the runtime
    (e.g. `+"`ollama rm <model>`"+`) rather than deleting raw disk blobs directly.
  * For personal/document directories: advise manual inspection rather than destructive commands.

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
type runDeps struct {
	assess   func(doctor.RunOptions) (doctor.Snapshot, diag.Report)
	scan     func(context.Context, string, diskusage.Options) (diskusage.Result, error)
	generate func([]byte, llm.CompleteOptions) (string, error)
	home     func() (string, error)
	out      io.Writer
}

var defaultDeps = runDeps{
	assess:   doctor.Assess,
	scan:     diskusage.Scan,
	generate: Generate,
	home:     os.UserHomeDir,
	out:      os.Stdout,
}

func Run(opts Options) error {
	return runWith(defaultDeps, opts)
}

func runWith(deps runDeps, opts Options) error {
	if deps.assess == nil {
		deps.assess = doctor.Assess
	}
	if deps.scan == nil {
		deps.scan = diskusage.Scan
	}
	if deps.generate == nil {
		deps.generate = Generate
	}
	if deps.home == nil {
		deps.home = os.UserHomeDir
	}
	out := deps.out
	if out == nil {
		out = os.Stdout
	}

	snap, report := deps.assess(doctor.RunOptions{OllamaURL: opts.OllamaURL})
	envelope := doctor.JSONReport(snap, report)

	var du *diskusage.Result
	if opts.Scan {
		budget := 10 * time.Second
		if opts.ScanBudget != "" {
			if b, err := time.ParseDuration(opts.ScanBudget); err == nil && b > 0 {
				budget = b
			}
		}
		top := opts.Top
		if top <= 0 {
			top = 10
		}
		root := opts.Root
		if root == "" {
			if h, err := deps.home(); err == nil {
				root = h
			}
		}
		if root != "" {
			scanOpts := diskusage.Options{Budget: budget, Top: top}
			if err := diskusage.Validate(scanOpts); err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), budget)
				res, err := deps.scan(ctx, root, scanOpts)
				cancel()
				if err == nil || res.Entries > 0 {
					du = &res
					envelope.DiskUsage = du
				}
			}
		}
	}

	reportJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("build report: %w", err)
	}
	heuristic := Heuristic(report)

	if !opts.JSON {
		ui.Header("ADVICE")
		fmt.Fprintf(out, "  %s\n\n", doctor.SummaryLine(snap))
		if du != nil {
			fmt.Fprintf(out, "  disk scan: %s observed in %d entries under %s\n\n",
				ui.HumanBytes(du.ObservedBytes), du.Entries, diskusage.DisplayPath(du.Root))
		}
		if findings := report.SortedBySeverity(); len(findings) == 0 {
			ui.Okf("no findings — this machine looks healthy")
		} else {
			doctor.PrintFindings(findings, true)
		}
		fmt.Fprintln(out)
		ui.Infof("checking for a local or cloud LLM to add further commentary — this can take a while on a local model, especially the first response after it loads...")
	}

	reply, genErr := deps.generate(reportJSON, llm.CompleteOptions{
		OllamaURL:   opts.OllamaURL,
		LMStudioURL: opts.LMStudioURL,
		LlamaCppURL: opts.LlamaCppURL,
		VLLMURL:     opts.VLLMURL,
		Provider:    opts.Provider,
		Model:       opts.Model,
	})

	if opts.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		res := struct {
			HeuristicAdvice string `json:"heuristic_advice"`
			LLMAdvice       string `json:"llm_advice,omitempty"`
			LLMError        string `json:"llm_error,omitempty"`
			Source          string `json:"source"`
		}{HeuristicAdvice: heuristic, Source: "heuristic"}
		if genErr != nil {
			res.LLMError = genErr.Error()
		} else {
			res.LLMAdvice = reply
			res.Source = "heuristic+llm"
		}
		return enc.Encode(res)
	}

	if genErr != nil {
		ui.Warnf("%s", noLLMMessage(report, genErr))
		return nil
	}
	fmt.Fprintln(out)
	ui.Header("AI COMMENTARY")
	fmt.Fprintln(out, guide.RenderTerminal(reply))
	return nil
}

// noLLMMessage explains why no AI commentary was added, phrased
// differently for a healthy report than one with real findings — a
// healthy machine has no "rule-based findings above" to point back to
// (just a bare "looks healthy" line), so reusing that same wording there
// left a dangling reference to something that was never shown, which is
// exactly what made the whole reply read as empty rather than as an
// actual, if brief, answer.
func noLLMMessage(report diag.Report, genErr error) string {
	if len(report.Findings) == 0 {
		return fmt.Sprintf("no LLM reachable (%v) — this machine looks healthy either way, per the numbers above", genErr)
	}
	return fmt.Sprintf("no LLM reachable (%v) — showing the rule-based findings above only", genErr)
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
