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
//	clean      Cross-platform disk cleanup (caches, logs, temp, trash)
//	memhogs    Rank application families & processes by memory use, with actions
//	memcheck   Advanced memory / swap / pressure overview and verdict
//	llm        Deep insight for local LLM runtimes (Ollama VRAM offload, etc.)
//	version    Print version information
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"time"

	"vitals/internal/clean"
	"vitals/internal/doctor"
	"vitals/internal/gpu"
	"vitals/internal/help"
	"vitals/internal/llm"
	"vitals/internal/memcheck"
	"vitals/internal/memhogs"
	"vitals/internal/monitor"
	"vitals/internal/ui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

//go:embed USERGUIDE.md
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
	argv := applyGlobalFlags(os.Args[1:])
	if len(argv) < 1 {
		help.RenderList(os.Stderr, version)
		os.Exit(2)
	}

	cmd := argv[0]
	args := argv[1:]

	switch cmd {
	case "doctor":
		fs := newFlagSet("doctor")
		url := fs.String("ollama-url", "http://localhost:11434", "base URL of the Ollama server")
		asJSON := fs.Bool("json", false, "emit the findings and snapshot as JSON")
		_ = fs.Parse(args)
		os.Exit(doctor.Run(doctor.RunOptions{OllamaURL: *url, JSON: *asJSON}))

	case "clean":
		fs := newFlagSet("clean")
		dry := fs.Bool("dry-run", false, "report what would be removed without deleting anything")
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		_ = fs.Parse(args)
		must(clean.Run(clean.Options{DryRun: *dry, Assume: *yes}))

	case "memhogs":
		fs := newFlagSet("memhogs")
		top := fs.Int("top", 15, "number of individual processes to list")
		_ = fs.Parse(args)
		must(memhogs.Run(*top))

	case "memcheck":
		fs := newFlagSet("memcheck")
		_ = fs.Parse(args)
		must(memcheck.Run())

	case "gpu":
		fs := newFlagSet("gpu")
		asJSON := fs.Bool("json", false, "emit GPU telemetry as JSON")
		_ = fs.Parse(args)
		must(gpu.Run(*asJSON))

	case "cpu", "mem", "memory", "disk", "net", "network", "power", "battery":
		fs := newFlagSet(cmd)
		url := fs.String("ollama-url", "http://localhost:11434", "base URL of the Ollama server")
		asJSON := fs.Bool("json", false, "emit the resource detail and findings as JSON")
		_ = fs.Parse(args)
		os.Exit(doctor.RunFocus(cmd, doctor.RunOptions{OllamaURL: *url, JSON: *asJSON}))

	case "top", "monitor":
		fs := newFlagSet("top")
		top := fs.Int("top", 15, "number of processes to list")
		sortBy := fs.String("sort", "cpu", "sort processes by \"cpu\" or \"mem\"")
		watch := fs.Bool("watch", false, "refresh continuously until interrupted")
		interval := fs.Duration("interval", 2*time.Second, "refresh interval when --watch is set")
		asJSON := fs.Bool("json", false, "emit a machine-readable snapshot instead of a report")
		_ = fs.Parse(args)
		must(monitor.Run(monitor.Options{
			Top:      *top,
			SortBy:   *sortBy,
			Watch:    *watch,
			Interval: *interval,
			JSON:     *asJSON,
		}))

	case "llm":
		if len(args) >= 1 && args[0] == "fit" {
			model := ""
			if len(args) >= 2 {
				model = args[1]
			}
			must(llm.RunFit(model))
			return
		}
		fs := newFlagSet("llm")
		url := fs.String("ollama-url", "http://localhost:11434", "base URL of the Ollama server")
		watch := fs.Bool("watch", false, "refresh continuously until interrupted")
		interval := fs.Duration("interval", 2*time.Second, "refresh interval when --watch is set")
		asJSON := fs.Bool("json", false, "emit a machine-readable snapshot instead of a report")
		_ = fs.Parse(args)
		must(llm.Run(llm.Options{
			OllamaURL: *url,
			Watch:     *watch,
			Interval:  *interval,
			JSON:      *asJSON,
		}))

	case "version", "--version", "-v":
		fmt.Printf("vitals %s\n", version)

	case "guide":
		fmt.Print(userGuide)

	case "completion":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: vitals completion bash|zsh|fish")
			os.Exit(2)
		}
		script, err := help.CompletionScript(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(2)
		}
		fmt.Print(script)

	case "help", "-h", "--help":
		if len(args) >= 1 {
			if err := help.RenderCommand(os.Stdout, args[0]); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
				help.RenderList(os.Stderr, version)
				os.Exit(2)
			}
			return
		}
		help.RenderList(os.Stdout, version)

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		help.RenderList(os.Stderr, version)
		os.Exit(2)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
