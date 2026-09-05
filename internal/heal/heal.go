// Package heal applies a diag.Finding's machine-executable Remedy —
// the third step after `vitals doctor` diagnoses and `vitals advice`
// explains. It is the only vitals command whose purpose is running
// remediation, so it is deliberately narrow (roadmap item 008, design
// §12 review):
//
//   - interactive TTY only. No batch/non-interactive mode in v1: a
//     no-TTY run refuses with a pointer to run it interactively.
//   - a tiny, hand-written v1 remedy set: `sudo purge` (macOS,
//     reversible) and a `vitals clean` delegate (gated by clean's own
//     preview + confirm + audit trail). No SIGTERM-to-a-process remedy
//     in v1 — the review cut it as the one irreversible, racy one.
//   - a compile-time exec allowlist checked at apply time, on top of
//     "only in-process builders construct a Remedy".
//   - it re-assesses the machine itself immediately before acting; it
//     never trusts a stale report piped in from an earlier run.
package heal

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/ui"
)

// Options configures a heal run.
type Options struct {
	OllamaURL string
	DryRun    bool   // print what each remedy would do; touch nothing
	Only      string // apply only the remedy for the finding with this ID
	Yes       bool   // pre-answer the confirmation prompt (still requires a TTY)
}

// runner is the injected OS surface, so the apply loop is unit-tested
// without a real sudo/clean/terminal. defaultRunner wires the real
// calls.
type runner struct {
	assess  func(doctor.RunOptions) (doctor.Snapshot, diag.Report)
	exec    func(argv []string) error
	confirm func(prompt string) bool
	isTTY   func() bool
	goos    string
	out     io.Writer
}

var defaultRunner = runner{
	assess:  doctor.QuickAssess,
	exec:    realExec,
	confirm: promptYesNo,
	isTTY:   ui.ColorEnabled, // ColorEnabled already means "stdout is an interactive terminal"
	goos:    runtime.GOOS,
	// out left nil: run() resolves it to the current os.Stdout at call
	// time, so a test swapping os.Stdout (captureStdout) is honoured.
}

// Run applies remedies for the current findings. Returns a process exit
// code: 0 on success (including "nothing to do"), non-zero on a refusal
// or a remedy failure.
func Run(opts Options) int { return run(defaultRunner, opts) }

func run(r runner, opts Options) int {
	if r.out == nil {
		r.out = os.Stdout
	}
	if !opts.DryRun && !r.isTTY() {
		fmt.Fprintln(r.out, "vitals heal needs an interactive terminal — run it directly, not from a script or pipe.")
		fmt.Fprintln(r.out, "(`vitals heal --dry-run` works anywhere and changes nothing.)")
		return 2
	}

	_, report := r.assess(doctor.RunOptions{OllamaURL: opts.OllamaURL})
	findings := report.SortedBySeverity()

	if opts.Only != "" {
		f, ok := findByID(findings, opts.Only)
		if !ok {
			fmt.Fprintf(r.out, "no current finding %q — nothing to do.\n", opts.Only)
			return 0 // the machine is fine for that id now; not an error
		}
		findings = []diag.Finding{f}
	}

	actionable := 0
	failed := 0
	for _, f := range findings {
		if !hasEnabledRemedy(f, r.goos) {
			printManual(r.out, f)
			continue
		}
		actionable++
		rem := *f.Remedy
		argv := resolveArgv(r, rem)

		fmt.Fprintln(r.out)
		fmt.Fprintf(r.out, "%s  [%s]\n", ui.Emph(f.Title), f.ID)
		fmt.Fprintf(r.out, "  remedy: %s\n", rem.Label)
		fmt.Fprintf(r.out, "  runs:   %s\n", strings.Join(argv, " "))
		fmt.Fprintf(r.out, "  risk:   %s, %s\n", rem.Risk, reversibleWord(rem.Reversible))

		if opts.DryRun {
			fmt.Fprintln(r.out, "  (--dry-run: not executed)")
			continue
		}
		if !opts.Yes && !r.confirm("  apply this remedy? [y/N] ") {
			fmt.Fprintln(r.out, "  skipped.")
			continue
		}

		if err := applyRemedy(r, rem, argv); err != nil {
			fmt.Fprintf(r.out, "  %s %v\n", ui.Red+"failed:"+ui.Reset, err)
			failed++
			continue
		}
		fmt.Fprintln(r.out, "  done.")
	}

	if actionable == 0 && opts.Only == "" {
		fmt.Fprintln(r.out, "\nno findings have an automatable remedy right now.")
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// hasEnabledRemedy reports whether f carries a Remedy heal will actually
// run in v1 on this OS: a non-nil RemedyExec/RemedyDelegate (never
// RemedySignal — disabled in v1), and, for the `sudo purge` remedy, only
// on macOS.
func hasEnabledRemedy(f diag.Finding, goos string) bool {
	if f.Remedy == nil {
		return false
	}
	switch f.Remedy.Kind {
	case diag.RemedyExec, diag.RemedyDelegate:
	default:
		return false // RemedyManual, RemedySignal (v1-disabled)
	}
	if len(f.Remedy.Argv) > 0 && f.Remedy.Argv[0] == "sudo" && goos != "darwin" {
		return false // `sudo purge` is macOS-only
	}
	return execAllowed(f.Remedy.Argv)
}

// execAllowed is the compile-time allowlist (review must-fix): heal
// only ever runs `vitals <subcommand>` or `sudo purge`. Any other
// Argv[0] — even from a crafted Remedy that somehow reached here — is
// refused.
func execAllowed(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch argv[0] {
	case "vitals":
		return true
	case "sudo":
		return len(argv) == 2 && argv[1] == "purge"
	default:
		return false
	}
}

// resolveArgv turns a Remedy's Argv into the command actually run: a
// RemedyDelegate's leading "vitals" becomes this binary's own path.
func resolveArgv(r runner, rem diag.Remedy) []string {
	argv := append([]string(nil), rem.Argv...)
	if rem.Kind == diag.RemedyDelegate && len(argv) > 0 && argv[0] == "vitals" {
		if self, err := os.Executable(); err == nil {
			argv[0] = self
		}
	}
	return argv
}

// applyRemedy runs one remedy. A RemedyDelegate to `vitals clean` runs
// the preview (`--dry-run`) first, shows it, re-confirms, then the real
// clean — so clean's own confirmation and audit history still apply.
func applyRemedy(r runner, rem diag.Remedy, argv []string) error {
	if !execAllowed(rem.Argv) {
		return fmt.Errorf("refusing to run %q — not in heal's allowlist", strings.Join(rem.Argv, " "))
	}
	if rem.Kind == diag.RemedyDelegate {
		preview := append(append([]string(nil), argv...), "--dry-run")
		fmt.Fprintf(r.out, "  preview: %s\n", strings.Join(preview, " "))
		if err := r.exec(preview); err != nil {
			return fmt.Errorf("preview failed: %w", err)
		}
		if !r.confirm("  proceed with the real cleanup? [y/N] ") {
			return fmt.Errorf("aborted after preview")
		}
	}
	return r.exec(argv)
}

func findByID(findings []diag.Finding, id string) (diag.Finding, bool) {
	for _, f := range findings {
		if f.ID == id {
			return f, true
		}
	}
	return diag.Finding{}, false
}

func printManual(w io.Writer, f diag.Finding) {
	if len(f.Fixes) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s — no automatable remedy; do this by hand:\n", ui.Emph(f.Title))
	for _, fix := range f.Fixes {
		fmt.Fprintf(w, "  → %s\n", fix)
	}
}

func reversibleWord(b bool) string {
	if b {
		return "reversible"
	}
	return "not reversible"
}

// --- real OS wiring -------------------------------------------------------

func realExec(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("empty command")
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func promptYesNo(prompt string) bool {
	fmt.Print(ui.Yellow + prompt + ui.Reset)
	return readYes(os.Stdin)
}

// readYes returns true only for an explicit y / yes (case-insensitive),
// matching internal/clean's and internal/dupes' own confirm helpers.
func readYes(r io.Reader) bool {
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}
