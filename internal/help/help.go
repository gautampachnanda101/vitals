// Package help is the single source of truth for vitals' command documentation:
// per-command synopsis, long description, flags and examples, plus the top-level
// listing, contextual help, and shell-completion generation. main wires each
// subcommand's flag.Usage to RenderCommand so `-h` and `vitals help <cmd>` agree.
package help

import (
	"fmt"
	"io"
	"slices"
	"strings"
)

// Flag documents one command flag.
type Flag struct {
	Name string // without dashes, e.g. "json"
	Arg  string // metavar, "" for a bool
	Help string
}

// Command documents one vitals subcommand.
type Command struct {
	Name     string
	Synopsis string
	Long     string
	Flags    []Flag
	Examples []string
}

var commands = []Command{
	{
		Name:     "doctor",
		Synopsis: "correlate every resource into one ranked verdict + fix",
		Long: "Samples CPU, memory, swap, disk, thermal and any local LLM runtime, then\n" +
			"correlates them: high CPU that is really I/O wait, swap thrashing vs\n" +
			"reclaimable cache, thermal throttling, a model running on CPU, a disk about\n" +
			"to fill. Prints the findings most-severe first with a concrete fix for each.\n" +
			"Exit code: 0 healthy, 1 warning, 2 critical — usable in scripts and CI.",
		Flags: []Flag{
			{"ollama-url", "URL", "base URL of the Ollama server (default http://localhost:11434)"},
			{"json", "", "emit the findings and the underlying snapshot as JSON"},
			{"no-color", "", "disable ANSI colour (also honours NO_COLOR)"},
		},
		Examples: []string{
			"vitals doctor",
			"vitals doctor --json | jq .verdict",
			"vitals doctor >/dev/null || echo 'machine unhealthy'",
		},
	},
	{
		Name:     "top",
		Synopsis: "Activity-Monitor-style snapshot of the whole system",
		Long: "System CPU / RAM / load, per-second disk and network I/O rates, and the top\n" +
			"processes by CPU or memory. --watch redraws it as a live dashboard.",
		Flags: []Flag{
			{"top", "N", "number of processes to list (default 15)"},
			{"sort", "cpu|mem", "sort processes by CPU or memory (default cpu)"},
			{"watch", "", "refresh continuously until interrupted"},
			{"interval", "DUR", "refresh interval when --watch is set (default 2s)"},
			{"json", "", "emit a machine-readable snapshot instead of a report"},
			{"no-color", "", "disable ANSI colour"},
		},
		Examples: []string{
			"vitals top --sort mem",
			"vitals top --watch --interval 1s",
			"vitals top --json | jq '.processes[0]'",
		},
	},
	{
		Name:     "clean",
		Synopsis: "cross-platform disk cleanup: caches, logs, temp, trash",
		Long: "Removes developer and OS caches, logs, temp directories and trash in pure Go,\n" +
			"and orchestrates brew / docker / npm / pip prune when those tools are present.\n" +
			"Always dry-run first.",
		Flags: []Flag{
			{"dry-run", "", "report what would be removed without deleting anything"},
			{"yes", "", "skip the confirmation prompt"},
			{"no-color", "", "disable ANSI colour"},
		},
		Examples: []string{
			"vitals clean --dry-run",
			"vitals clean --yes",
		},
	},
	{
		Name:     "memhogs",
		Synopsis: "rank app families & processes by memory, with an action each",
		Long: "Groups processes into application families (Chrome, VS Code, Docker, JetBrains,\n" +
			"Ollama...), ranks them and the heaviest individual processes by resident\n" +
			"memory, and prints an OS-correct stop command for each.",
		Flags: []Flag{
			{"top", "N", "number of individual processes to list (default 15)"},
			{"no-color", "", "disable ANSI colour"},
		},
		Examples: []string{"vitals memhogs", "vitals memhogs --top 30"},
	},
	{
		Name:     "memcheck",
		Synopsis: "RAM / swap / pressure overview with a health verdict",
		Long: "A detailed physical-memory and swap breakdown followed by a ranked verdict\n" +
			"with concrete remedies.",
		Flags:    []Flag{{"no-color", "", "disable ANSI colour"}},
		Examples: []string{"vitals memcheck"},
	},
	{
		Name:     "gpu",
		Synopsis: "GPU telemetry via nvidia-smi / rocm-smi / ioreg",
		Long: "Per-GPU VRAM, utilisation, temperature, power and clocks, plus the\n" +
			"processes holding VRAM (NVIDIA). Reads the vendor CLI that is already\n" +
			"installed; reports nothing gracefully when none is present. For a live\n" +
			"per-process view, use nvtop.",
		Flags:    []Flag{{"json", "", "emit GPU telemetry as JSON"}, {"no-color", "", "disable ANSI colour"}},
		Examples: []string{"vitals gpu", "vitals gpu --json"},
	},
	{
		Name:     "llm",
		Synopsis: "inspect every local and cloud LLM endpoint",
		Long: "Local runtimes (Ollama, LM Studio, llama.cpp, vLLM): host CPU/RAM of the\n" +
			"runtime process and, for Ollama, per-model VRAM footprint with the exact GPU\n" +
			"offload percentage. Cloud providers (OpenAI, Anthropic, Groq, Mistral,\n" +
			"Together, OpenRouter, DeepSeek, xAI, Fireworks, Gemini, Ollama Cloud):\n" +
			"reachability and latency, probed over the open OpenAI-compatible API only\n" +
			"when the provider's API-key environment variable is set. API keys are never\n" +
			"printed.",
		Flags: []Flag{
			{"ollama-url", "URL", "base URL of the Ollama server"},
			{"watch", "", "refresh continuously until interrupted"},
			{"interval", "DUR", "refresh interval when --watch is set"},
			{"json", "", "emit a machine-readable snapshot instead of a report"},
			{"no-color", "", "disable ANSI colour"},
		},
		Examples: []string{
			"vitals llm",
			"OPENAI_API_KEY=sk-... vitals llm",
			"vitals llm --json | jq '.models[] | {name, gpu_offload_percent}'",
		},
	},
	{
		Name:     "guide",
		Synopsis: "print the full embedded user guide",
		Long:     "Writes the complete user guide (bundled into the binary) to stdout.",
		Examples: []string{"vitals guide", "vitals guide | less"},
	},
	{
		Name:     "completion",
		Synopsis: "output a shell completion script (bash | zsh | fish)",
		Long: "Prints a completion script for the named shell. Install it with, e.g.:\n" +
			"  vitals completion bash | sudo tee /etc/bash_completion.d/vitals\n" +
			"  vitals completion zsh  > \"${fpath[1]}/_vitals\"\n" +
			"  vitals completion fish > ~/.config/fish/completions/vitals.fish",
		Examples: []string{"vitals completion bash", "vitals completion zsh"},
	},
	{
		Name:     "version",
		Synopsis: "print version information",
		Long:     "Prints the vitals version the binary was built from.",
		Examples: []string{"vitals version"},
	},
}

// Names returns every command name, sorted.
func Names() []string {
	out := make([]string, len(commands))
	for i, c := range commands {
		out[i] = c.Name
	}
	slices.Sort(out)
	return out
}

// Lookup returns the named command's documentation.
func Lookup(name string) (Command, bool) {
	for _, c := range commands {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// RenderCommand writes contextual help for one command.
func RenderCommand(w io.Writer, name string) error {
	c, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("unknown command %q", name)
	}
	fmt.Fprintf(w, "vitals %s — %s\n\n", c.Name, c.Synopsis)
	if c.Long != "" {
		fmt.Fprintf(w, "%s\n\n", c.Long)
	}
	fmt.Fprintf(w, "USAGE\n  vitals %s", c.Name)
	if len(c.Flags) > 0 {
		fmt.Fprint(w, " [flags]")
	}
	fmt.Fprint(w, "\n")
	if len(c.Flags) > 0 {
		fmt.Fprint(w, "\nFLAGS\n")
		for _, f := range c.Flags {
			left := "--" + f.Name
			if f.Arg != "" {
				left += " " + f.Arg
			}
			fmt.Fprintf(w, "  %-22s %s\n", left, f.Help)
		}
	}
	if len(c.Examples) > 0 {
		fmt.Fprint(w, "\nEXAMPLES\n")
		for _, e := range c.Examples {
			fmt.Fprintf(w, "  %s\n", e)
		}
	}
	return nil
}

// RenderList writes the top-level command listing.
func RenderList(w io.Writer, version string) {
	fmt.Fprintf(w, "vitals — cross-platform system diagnostics (%s)\n\n", version)
	fmt.Fprint(w, "USAGE\n  vitals [--no-color] <command> [flags]\n\n")
	fmt.Fprint(w, "COMMANDS\n")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-11s %s\n", c.Name, c.Synopsis)
	}
	fmt.Fprint(w, "\nRun 'vitals help <command>' for details on a command.\n")
	fmt.Fprint(w, "This binary complements htop/btop, ncdu, nvtop, glances; it does not replace them.\n")
}

// CompletionScript returns a completion script for bash, zsh or fish.
func CompletionScript(shell string) (string, error) {
	names := strings.Join(Names(), " ")
	switch shell {
	case "bash":
		return "" +
			"# bash completion for vitals\n" +
			"_vitals() {\n" +
			"  local cur=\"${COMP_WORDS[COMP_CWORD]}\"\n" +
			"  if [ \"$COMP_CWORD\" -eq 1 ]; then\n" +
			"    COMPREPLY=( $(compgen -W \"" + names + " help\" -- \"$cur\") )\n" +
			"    return\n" +
			"  fi\n" +
			"  case \"${COMP_WORDS[1]}\" in\n" +
			"    help)       COMPREPLY=( $(compgen -W \"" + names + "\" -- \"$cur\") );;\n" +
			"    completion) COMPREPLY=( $(compgen -W \"bash zsh fish\" -- \"$cur\") );;\n" +
			"    *)          COMPREPLY=( $(compgen -W \"--json --no-color --watch --interval --top --sort --dry-run --yes --ollama-url\" -- \"$cur\") );;\n" +
			"  esac\n" +
			"}\n" +
			"complete -F _vitals vitals\n", nil
	case "zsh":
		return "" +
			"#compdef vitals\n" +
			"_vitals() {\n" +
			"  local -a cmds; cmds=(" + names + " help)\n" +
			"  if (( CURRENT == 2 )); then\n" +
			"    compadd -- $cmds\n" +
			"    return\n" +
			"  fi\n" +
			"  case $words[2] in\n" +
			"    help) compadd -- " + names + ";;\n" +
			"    completion) compadd -- bash zsh fish;;\n" +
			"    *) compadd -- --json --no-color --watch --interval --top --sort --dry-run --yes --ollama-url;;\n" +
			"  esac\n" +
			"}\n" +
			"compdef _vitals vitals\n", nil
	case "fish":
		var b strings.Builder
		b.WriteString("# fish completion for vitals\n")
		b.WriteString("complete -c vitals -f\n")
		for _, c := range commands {
			fmt.Fprintf(&b, "complete -c vitals -n '__fish_use_subcommand' -a %s -d %q\n", c.Name, c.Synopsis)
		}
		b.WriteString("complete -c vitals -n '__fish_use_subcommand' -a help -d 'help for a command'\n")
		b.WriteString("complete -c vitals -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'\n")
		return b.String(), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want bash, zsh or fish)", shell)
	}
}
