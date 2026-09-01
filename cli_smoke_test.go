package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCLISmoke execs the real, compiled binary for every read-only command —
// closing a real gap in CI: `go test ./...` exercises each package's pure
// logic thoroughly, but nothing previously invoked `vitals <command>` itself
// end to end, so a wiring mistake (a bad flag default, a panic on startup, a
// command that silently exits the wrong code) could reach a release
// unnoticed. This runs on every `go test ./...`, so it runs in CI on all
// three OSes the same as everything else.
//
// Deliberately excluded: anything destructive (`clean` without --dry-run,
// `dupes --hardlink`, `tools --install`), anything that blocks forever by
// design (`serve`, `mcp`, `--watch`), and `advice` (needs a real LLM
// endpoint — network-flaky, not appropriate for a fast deterministic test).
func TestCLISmoke(t *testing.T) {
	bin := buildCLIOnce(t)
	scratch := t.TempDir()

	cases := []struct {
		name string
		args []string
	}{
		{"version", []string{"version"}},
		{"help", []string{"help"}},
		{"help-doctor", []string{"help", "doctor"}},
		{"doctor-h", []string{"doctor", "-h"}},
		{"guide", []string{"guide"}},
		{"guide-raw", []string{"guide", "--raw"}},
		{"completion-bash", []string{"completion", "bash"}},
		{"completion-zsh", []string{"completion", "zsh"}},
		{"completion-fish", []string{"completion", "fish"}},
		{"doctor-json", []string{"doctor", "--json"}},
		{"doctor-ci", []string{"doctor", "--ci"}},
		{"doctor-quiet", []string{"doctor", "-q"}},
		{"doctor-schema", []string{"doctor", "--schema"}},
		{"cpu-json", []string{"cpu", "--json"}},
		{"mem-json", []string{"mem", "--json"}},
		{"disk-json", []string{"disk", "--json"}},
		{"net-json", []string{"net", "--json"}},
		{"power-json", []string{"power", "--json"}},
		{"top-json", []string{"top", "--json"}},
		{"gpu-json", []string{"gpu", "--json"}},
		{"llm-json", []string{"llm", "--json"}},
		{"memhogs", []string{"memhogs"}},
		{"memcheck", []string{"memcheck"}},
		{"tools", []string{"tools"}},
		{"dupes-json", []string{"dupes", "--root", scratch, "--json"}},
		{"clean-dry-run", []string{"clean", "--dry-run"}},
		{"clean-history", []string{"clean", "--history"}},
		{"export", []string{"export"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, bin, c.args...)
			// Isolate every OS's config-dir lookup into scratch: HOME covers
			// macOS/Linux, APPDATA covers Windows, and XDG_CONFIG_HOME is
			// force-cleared so an inherited value from the host can't win.
			cmd.Env = append(os.Environ(), "HOME="+scratch, "APPDATA="+scratch, "XDG_CONFIG_HOME=", "NO_COLOR=1")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()

			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("vitals %s hung past 20s (should be read-only and fast):\n%s", strings.Join(c.args, " "), out.String())
			}
			if strings.Contains(out.String(), "panic:") {
				t.Fatalf("vitals %s panicked:\n%s", strings.Join(c.args, " "), out.String())
			}

			exitCode := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !isExitError(err, &exitErr) {
					t.Fatalf("vitals %s: %v\n%s", strings.Join(c.args, " "), err, out.String())
				}
				exitCode = exitErr.ExitCode()
			}
			// doctor / cpu / mem / disk / net / power exit 0/1/2 by design
			// (healthy/warning/critical); everything else should exit 0 on
			// a well-formed, read-only invocation.
			allowed := []int{0}
			if strings.HasPrefix(c.name, "doctor") || strings.HasSuffix(c.name, "-json") && isResourceFocus(c.args[0]) {
				allowed = []int{0, 1, 2}
			}
			if !slices.Contains(allowed, exitCode) {
				t.Errorf("vitals %s exited %d, want one of %v:\n%s", strings.Join(c.args, " "), exitCode, allowed, out.String())
			}
		})
	}
}

func isResourceFocus(cmd string) bool {
	switch cmd {
	case "cpu", "mem", "disk", "net", "power":
		return true
	default:
		return false
	}
}

// isExitError is a small indirection so the type assertion reads clearly at
// the call site above.
func isExitError(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

var (
	cliOnce   sync.Once
	cliPath   string
	cliBuildE error
)

// buildCLIOnce compiles the binary once per test run and reuses it across
// every subtest, so the smoke test suite doesn't pay a full build per case.
func buildCLIOnce(t *testing.T) string {
	t.Helper()
	cliOnce.Do(func() {
		dir := t.TempDir()
		name := "vitals-smoke-test"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cliPath = filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", cliPath, ".")
		out, err := cmd.CombinedOutput()
		if err != nil {
			cliBuildE = err
			t.Logf("build output:\n%s", out)
		}
	})
	if cliBuildE != nil {
		t.Fatalf("building the CLI for smoke testing: %v", cliBuildE)
	}
	return cliPath
}
