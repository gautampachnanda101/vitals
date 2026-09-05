// Command vitals is a single, cross-platform helper binary that consolidates a
// set of system-diagnostic and cleanup routines which previously lived as
// separate shell / PowerShell scripts.
//
// Philosophy: wherever an established open-source tool already does the job well
// (htop, btop, ncdu, nvtop, glances, docker, brew...), this binary complements
// rather than replaces it — it stitches together a consolidated, scriptable view
// and adds diagnostics those tools don't provide (e.g. per-model GPU offload for
// local LLM runtimes). All metrics come from gopsutil, so there are no external
// script dependencies and the same binary runs on macOS, Linux and Windows.
//
// Usage:
//
//	vitals <command> [flags]
//
// Commands:
//
//	dashboard  Serve vitals as a local, loopback-only web app
//	advice     Ask a local or cloud LLM for advice on the current doctor report
//	clean      Cross-platform disk cleanup (caches, logs, temp, trash)
//	dupes      Find byte-identical duplicate files (report only, never deletes)
//	tools      List/install the companion tools vitals defers to (ncdu, btop, ...)
//	explore    Launch the best installed disk explorer (gdu/ncdu/dust)
//	live       Launch the best installed live monitor (btop/htop)
//	memhogs    Rank application families & processes by memory use, with actions
//	memcheck   Advanced memory / swap / pressure overview and verdict
//	llm        Deep insight for local LLM runtimes (Ollama VRAM offload, etc.)
//	info       Binary, machine, and config file details
//	version    Print version information
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"vitals/internal/advice"
	"vitals/internal/clean"
	"vitals/internal/config"
	"vitals/internal/dashboard"
	"vitals/internal/doctor"
	"vitals/internal/dupes"
	"vitals/internal/gpu"
	"vitals/internal/guide"
	"vitals/internal/help"
	"vitals/internal/info"
	"vitals/internal/llm"
	"vitals/internal/mcp"
	"vitals/internal/memcheck"
	"vitals/internal/memhogs"
	"vitals/internal/metrics"
	"vitals/internal/monitor"
	"vitals/internal/tools"
	"vitals/internal/ui"
)

// cfg is the resolved config-file overrides (thresholds, default
// --ollama-url), loaded once at startup. A missing or unreadable config file
// leaves every field at config.Default(), so this is always safe to read.
var cfg = config.Load()

// defaultOllamaURL is the --ollama-url flag default: the config file's value
// when set, else the well-known local Ollama port.
func defaultOllamaURL() string {
	if cfg.OllamaURL != "" {
		return cfg.OllamaURL
	}
	return "http://localhost:11434"
}

// defaultLMStudioURL/defaultLlamaCppURL/defaultVLLMURL mirror
// defaultOllamaURL for the other three local runtimes --advice supports.
// Unlike defaultOllamaURL, these fall through to "" (not a hardcoded
// port) when unset — the flag's own current default is already "", and
// internal/llm's Options.apply() fills in that runtime's well-known port
// itself, so there's no reason to duplicate that constant here too.
func defaultLMStudioURL() string { return cfg.LMStudioURL }
func defaultLlamaCppURL() string { return cfg.LlamaCppURL }
func defaultVLLMURL() string     { return cfg.VLLMURL }

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

//go:embed docs/user-guide.md
var userGuide string

// newFlagSet builds a subcommand flag set whose -h / -help output is the
// contextual help from the help package, so `vitals X -h` and
// `vitals help X` always agree.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.Usage = func() { _ = help.RenderCommand(os.Stderr, name) }
	return fs
}

// applyGlobalFlags consumes options that are valid before any subcommand
// (currently just --no-color) and returns the remaining arguments untouched.
func applyGlobalFlags(in []string) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		switch a {
		case "--no-color", "-no-color":
			ui.DisableColor()
		default:
			out = append(out, a)
		}
	}
	return out
}

func main() {
	doctor.SetThresholds(cfg)
	os.Exit(run(applyGlobalFlags(os.Args[1:]), version))
}

// run dispatches a single CLI invocation and returns the process exit code.
// main just wraps this call in os.Exit — kept separate so the dispatch and
// flag-validation logic (unknown commands, missing args, --schema/--compare
// validation, and so on) can be unit tested directly instead of only via the
// exec-the-real-binary smoke test. Most subcommands still shell out to their
// package's own Run/RunFocus, which touches real OS/network state — that
// part remains exercised by cli_smoke_test.go, not here.
func run(argv []string, version string) int {
	if len(argv) < 1 {
		help.RenderList(os.Stderr, version)
		return 2
	}

	cmd := argv[0]
	args := argv[1:]

	switch cmd {
	case "doctor":
		fs := newFlagSet("doctor")
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		asJSON := fs.Bool("json", false, "emit the findings and snapshot as JSON")
		output := fs.String("output", "", "also write the JSON envelope to this file")
		ci := fs.Bool("ci", false, "print one grep-friendly line instead of the full report")
		quiet := fs.Bool("quiet", false, "print nothing; only the exit code carries the verdict")
		fs.BoolVar(quiet, "q", false, "shorthand for --quiet")
		webhook := fs.String("webhook", "", "POST the JSON envelope here when the verdict needs attention")
		webhookAllowInsecure := fs.Bool("webhook-allow-insecure", false, "allow plain http and loopback/private/link-local --webhook targets (refused by default)")
		compare := fs.Bool("compare", false, "compare two --output-saved reports: vitals doctor --compare old.json new.json")
		schema := fs.Bool("schema", false, "print the JSON Schema for the --json payload and exit")
		_ = fs.Parse(args)
		if *schema {
			os.Stdout.Write(doctor.Schema())
			return 0
		}
		if *compare {
			if fs.NArg() != 2 {
				fmt.Fprintln(os.Stderr, "usage: vitals doctor --compare <old.json> <new.json>")
				return 2
			}
			oldEnv, err := doctor.LoadJSONEnvelope(fs.Arg(0))
			if err != nil {
				return must(err)
			}
			newEnv, err := doctor.LoadJSONEnvelope(fs.Arg(1))
			if err != nil {
				return must(err)
			}
			fmt.Print(doctor.RenderCompare(oldEnv, newEnv))
			return 0
		}
		return doctor.Run(doctor.RunOptions{OllamaURL: *url, JSON: *asJSON, Output: *output, CI: *ci, Quiet: *quiet, Webhook: *webhook, WebhookAllowInsecure: *webhookAllowInsecure})

	case "clean":
		fs := newFlagSet("clean")
		dry := fs.Bool("dry-run", false, "report what would be removed without deleting anything")
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		history := fs.Bool("history", false, "print past clean runs (date, freed) instead of cleaning")
		_ = fs.Parse(args)
		return must(clean.Run(clean.Options{DryRun: *dry, Assume: *yes, ShowHistory: *history}))

	case "dupes":
		fs := newFlagSet("dupes")
		root := fs.String("root", "", "directory to scan (default: home directory)")
		minMB := fs.Int64("min-size-mb", 1, "ignore files smaller than this many MB")
		top := fs.Int("top", 20, "number of duplicate groups to print")
		asJSON := fs.Bool("json", false, "emit the full result as JSON")
		output := fs.String("output", "", "also write the full JSON result to this file")
		hardlink := fs.Bool("hardlink", false, "replace duplicates with hardlinks to reclaim space (destroys no data)")
		yes := fs.Bool("yes", false, "skip the confirmation prompt before applying --hardlink")
		fast := fs.Bool("fast", false, "use the jdupes backend if it's installed (faster; reports no scanned-file total)")
		_ = fs.Parse(args)
		return must(dupes.Run(dupes.Options{
			Root:     *root,
			MinSize:  *minMB << 20,
			Top:      *top,
			JSON:     *asJSON,
			Output:   *output,
			Hardlink: *hardlink,
			Yes:      *yes,
			Fast:     *fast,
		}))

	case "tools":
		fs := newFlagSet("tools")
		install := fs.String("install", "", "install this companion tool via the system package manager")
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		_ = fs.Parse(args)
		return must(tools.Run(tools.Options{Install: *install, Yes: *yes}))

	case "explore":
		fs := newFlagSet("explore")
		_ = fs.Parse(args)
		path := "."
		if fs.NArg() > 0 {
			path = fs.Arg(0)
		}
		return must(tools.Launch("disk explorer", []string{path}))

	case "live":
		fs := newFlagSet("live")
		_ = fs.Parse(args)
		return must(tools.Launch("live monitor", nil))

	case "memhogs":
		fs := newFlagSet("memhogs")
		top := fs.Int("top", 15, "number of individual processes to list")
		watch := fs.Bool("watch", false, "refresh continuously until interrupted")
		interval := fs.Duration("interval", 2*time.Second, "refresh interval when --watch is set")
		_ = fs.Parse(args)
		return must(memhogs.Run(memhogs.Options{Top: *top, Watch: *watch, Interval: *interval}))

	case "memcheck":
		fs := newFlagSet("memcheck")
		_ = fs.Parse(args)
		return must(memcheck.Run())

	case "gpu":
		fs := newFlagSet("gpu")
		asJSON := fs.Bool("json", false, "emit GPU telemetry as JSON")
		live := fs.Bool("live", false, "hand off to a live per-process GPU monitor (nvtop) if installed — see `vitals tools install nvtop`")
		_ = fs.Parse(args)
		if *live {
			return must(tools.Launch("GPU monitor", nil))
		}
		return must(gpu.Run(*asJSON))

	case "cpu", "mem", "memory", "disk", "net", "network", "power", "battery":
		fs := newFlagSet(cmd)
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		asJSON := fs.Bool("json", false, "emit the resource detail and findings as JSON")
		output := fs.String("output", "", "also write the JSON envelope to this file")
		ci := fs.Bool("ci", false, "print one grep-friendly line instead of the full report")
		quiet := fs.Bool("quiet", false, "print nothing; only the exit code carries the verdict")
		fs.BoolVar(quiet, "q", false, "shorthand for --quiet")
		verbose := fs.Bool("verbose", false, "show more than the default view has room for (every core, the full reclaimable list, more net peers)")
		fs.BoolVar(verbose, "v", false, "shorthand for --verbose")
		_ = fs.Parse(args)
		return doctor.RunFocus(cmd, doctor.RunOptions{OllamaURL: *url, JSON: *asJSON, Output: *output, CI: *ci, Quiet: *quiet, Verbose: *verbose})

	case "top", "monitor":
		fs := newFlagSet("top")
		top := fs.Int("top", 15, "number of processes to list")
		sortBy := fs.String("sort", "cpu", "sort processes by \"cpu\" or \"mem\"")
		watch := fs.Bool("watch", false, "refresh continuously until interrupted")
		interval := fs.Duration("interval", 2*time.Second, "refresh interval when --watch is set")
		asJSON := fs.Bool("json", false, "emit a machine-readable snapshot instead of a report")
		_ = fs.Parse(args)
		return must(monitor.Run(monitor.Options{
			Top:      *top,
			SortBy:   *sortBy,
			Watch:    *watch,
			Interval: *interval,
			JSON:     *asJSON,
		}))

	case "advice":
		fs := newFlagSet("advice")
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		lmstudioURL := fs.String("lmstudio-url", defaultLMStudioURL(), "base URL of the LM Studio server")
		llamacppURL := fs.String("llamacpp-url", defaultLlamaCppURL(), "base URL of the llama.cpp / OpenAI-compatible server")
		vllmURL := fs.String("vllm-url", defaultVLLMURL(), "base URL of the vLLM server")
		provider := fs.String("provider", "", "force a provider (ollama, lmstudio, llamacpp, vllm, openai, anthropic, groq, ...); default: auto-detect")
		model := fs.String("model", "", "override the provider's default model")
		asJSON := fs.Bool("json", false, "emit {\"advice\": \"...\"} as JSON instead of plain text")
		_ = fs.Parse(args)
		return must(advice.Run(advice.Options{
			OllamaURL:   *url,
			LMStudioURL: *lmstudioURL,
			LlamaCppURL: *llamacppURL,
			VLLMURL:     *vllmURL,
			Provider:    *provider,
			Model:       *model,
			JSON:        *asJSON,
		}))

	case "llm":
		if len(args) >= 1 && args[0] == "fit" {
			model := ""
			if len(args) >= 2 {
				model = args[1]
			}
			return must(llm.RunFit(model))
		}
		fs := newFlagSet("llm")
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		watch := fs.Bool("watch", false, "refresh continuously until interrupted")
		interval := fs.Duration("interval", 2*time.Second, "refresh interval when --watch is set")
		asJSON := fs.Bool("json", false, "emit a machine-readable snapshot instead of a report")
		_ = fs.Parse(args)
		return must(llm.Run(llm.Options{
			OllamaURL: *url,
			Watch:     *watch,
			Interval:  *interval,
			JSON:      *asJSON,
		}))

	case "serve":
		fs := newFlagSet("serve")
		fs.Bool("prometheus", true, "expose Prometheus metrics (the only format today)")
		addr := fs.String("addr", "127.0.0.1:9100", "listen address for /metrics — a bare \":PORT\" binds every interface")
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		_ = fs.Parse(args)
		return must(metrics.Serve(metrics.Options{OllamaURL: *url, Addr: *addr}))

	case "export":
		fs := newFlagSet("export")
		fs.Bool("prometheus", true, "Prometheus text-exposition format (the only format today)")
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		_ = fs.Parse(args)
		return must(metrics.RunOnce(metrics.Options{OllamaURL: *url}))

	case "mcp":
		fs := newFlagSet("mcp")
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		_ = fs.Parse(args)
		return must(mcp.Serve(os.Stdin, os.Stdout, mcp.Options{OllamaURL: *url}))

	case "version", "--version", "-v":
		fmt.Printf("vitals %s\n", version)
		return 0

	case "info":
		fs := newFlagSet("info")
		asJSON := fs.Bool("json", false, "emit machine-readable JSON instead of the terminal view")
		_ = fs.Parse(args)
		r := info.Collect(version)
		if *asJSON {
			return must(json.NewEncoder(os.Stdout).Encode(r))
		}
		ui.Header("INFO")
		fmt.Println(info.Render(r))
		return 0

	case "dashboard":
		fs := newFlagSet("dashboard")
		addr := fs.String("addr", "", "loopback host:port to serve on (only the port is used — vitals dashboard never binds beyond 127.0.0.1); empty picks a random port")
		noOpen := fs.Bool("no-open", false, "don't open a browser automatically")
		url := fs.String("ollama-url", defaultOllamaURL(), "base URL of the Ollama server")
		_ = fs.Parse(args)
		return must(dashboard.Serve(dashboard.Options{Addr: *addr, NoOpen: *noOpen, OllamaURL: *url, Version: version}))

	case "guide":
		fs := newFlagSet("guide")
		web := fs.Bool("web", false, "serve the guide as HTML in your browser instead of printing it")
		raw := fs.Bool("raw", false, "print the literal Markdown source instead of the pretty-printed terminal version")
		_ = fs.Parse(args)
		switch {
		case *web:
			return must(guide.Serve(userGuide, "vitals user guide"))
		case *raw:
			fmt.Print(userGuide)
			return 0
		default:
			fmt.Print(guide.RenderTerminal(userGuide))
			return 0
		}

	case "completion":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: vitals completion bash|zsh|fish")
			return 2
		}
		script, err := help.CompletionScript(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 2
		}
		fmt.Print(script)
		return 0

	case "help", "-h", "--help":
		if len(args) >= 1 {
			if err := help.RenderCommand(os.Stdout, args[0]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
				help.RenderList(os.Stderr, version)
				return 2
			}
			return 0
		}
		help.RenderList(os.Stdout, version)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		help.RenderList(os.Stderr, version)
		return 2
	}
}

// must prints err (if any) and returns the exit code run should return for
// it: 1 on error, 0 on success.
func must(err error) int {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
