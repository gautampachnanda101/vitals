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
	"flag"
	"fmt"
	"os"
	"time"

	"vitals/internal/clean"
	"vitals/internal/doctor"
	"vitals/internal/llm"
	"vitals/internal/memcheck"
	"vitals/internal/memhogs"
	"vitals/internal/monitor"
	"vitals/internal/ui"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

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
		usage()
		os.Exit(2)
	}

	cmd := argv[0]
	args := argv[1:]

	switch cmd {
	case "doctor":
		fs := flag.NewFlagSet("doctor", flag.ExitOnError)
		url := fs.String("ollama-url", "http://localhost:11434", "base URL of the Ollama server")
		asJSON := fs.Bool("json", false, "emit the findings and snapshot as JSON")
		_ = fs.Parse(args)
		os.Exit(doctor.Run(doctor.RunOptions{OllamaURL: *url, JSON: *asJSON}))

	case "clean":
		fs := flag.NewFlagSet("clean", flag.ExitOnError)
		dry := fs.Bool("dry-run", false, "report what would be removed without deleting anything")
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		_ = fs.Parse(args)
		must(clean.Run(clean.Options{DryRun: *dry, Assume: *yes}))

	case "memhogs":
		fs := flag.NewFlagSet("memhogs", flag.ExitOnError)
		top := fs.Int("top", 15, "number of individual processes to list")
		_ = fs.Parse(args)
		must(memhogs.Run(*top))

	case "memcheck":
		fs := flag.NewFlagSet("memcheck", flag.ExitOnError)
		_ = fs.Parse(args)
		must(memcheck.Run())

	case "top", "monitor":
		fs := flag.NewFlagSet("top", flag.ExitOnError)
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
		fs := flag.NewFlagSet("llm", flag.ExitOnError)
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

	case "help", "-h", "--help":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `vitals — cross-platform system helper (`+version+`)

USAGE
  vitals [--no-color] <command> [flags]

GLOBAL FLAGS
  --no-color   disable ANSI colour (also honours the NO_COLOR env var)

COMMANDS
  doctor     Correlate CPU / memory / disk / thermal / LLM signals into a ranked
             verdict — what is constraining the machine right now and the exact
             fix. Exit code: 0 healthy, 1 warning, 2 critical.
             Flags: --ollama-url URL  --json
  top        Activity-Monitor-style snapshot: system CPU / RAM / load, per-second
             disk and network I/O, and the top processes by CPU or memory.
             Flags: --top N  --sort cpu|mem  --watch  --interval DUR  --json
  clean      Cross-platform disk cleanup: dev/OS caches, logs, temp, trash.
             Flags: --dry-run  --yes
  memhogs    Rank application families and processes by memory footprint and
             print an OS-correct suggested action (kill / prune) for each.
             Flags: --top N
  memcheck   Advanced RAM / swap / pressure overview with a health verdict.
  llm        Deep diagnostics for local and cloud LLM endpoints: host CPU/RAM of
             any local runtime, Ollama's per-model VRAM footprint and GPU offload
             percentage, plus reachability/latency of cloud providers whose API
             key is set in the environment.
             Flags: --ollama-url URL  --watch  --interval DUR  --json
  version    Print version information.

EXAMPLES
  vitals top --sort mem --watch
  vitals clean --dry-run
  vitals memhogs --top 20
  vitals memcheck
  vitals llm --watch --interval 1s
  vitals llm --json | jq '.models[] | {name, gpu_offload_percent}'

This binary complements standard tools (htop/btop, ncdu, nvtop, glances,
docker, brew); it does not replace them.
`)
}
