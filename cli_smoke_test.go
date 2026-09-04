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

	"vitals/internal/llm"
)

// TestCLISmoke execs the real, compiled binary for every read-only command —
// closing a real gap in CI: `go test ./...` exercises each package's pure
// logic thoroughly, but nothing previously invoked `vitals <command>` itself
// end to end, so a wiring mistake (a bad flag default, a panic on startup, a
// command that silently exits the wrong code) could reach a release
// unnoticed. This runs on every `go test ./...`, so it runs in CI on all
// three OSes the same as everything else.
//
// Deliberately excluded: anything destructive against real user state
// (`clean` without --dry-run, `tools --install`), anything that blocks
// forever by design (`serve`, `mcp`, `--watch`, `guide --web`, `dashboard`
// — see dashboard_smoke_test.go for that one's own dedicated test).
// `dupes --hardlink` IS included — safely, against a directory guaranteed
// to be empty, so it only exercises flag-wiring, never real file mutation.
// `advice` IS included too, forced onto a closed port (--ollama-url
// http://127.0.0.1:1) so it never depends on or waits on a real local LLM
// — this exercises Run's no-LLM-reachable path specifically (the
// heuristic findings print/encode and it exits 0), not a real completion;
// every cloud provider's API key env var is cleared below for the same
// reason, so this can never make a real network call even if the machine
// running the test happens to have one set for unrelated work.
func TestCLISmoke(t *testing.T) {
	bin := buildCLIOnce(t)
	scratch := t.TempDir()
	// Its own directory, never the shared scratch: by the time later cases
	// run, scratch already holds a config/history dir the earlier commands
	// wrote (scratch also serves as HOME/APPDATA below), and a dupes scan
	// of that would just be noise the test has to reason around.
	dupesRoot := t.TempDir()

	cases := []struct {
		name string
		args []string
	}{
		{"version", []string{"version"}},
		{"info", []string{"info"}},
		{"info-json", []string{"info", "--json"}},
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
		{"dupes-json", []string{"dupes", "--root", dupesRoot, "--json"}},
		// Safe because dupesRoot is guaranteed empty: no files exist to
		// hardlink, so this only exercises the flag-wiring/confirmation
		// path, never ApplyHardlinks' actual file mutation.
		{"dupes-hardlink-empty-dir", []string{"dupes", "--root", dupesRoot, "--hardlink", "--yes"}},
		{"clean-dry-run", []string{"clean", "--dry-run"}},
		{"clean-history", []string{"clean", "--history"}},
		{"export", []string{"export"}},
		// --ollama-url points at a closed port so this never depends on
		// (or waits on) a real local LLM being installed on the machine
		// running the test — deterministic exercise of Run's no-LLM path:
		// the heuristic findings print/encode and it exits 0, not the old
		// bare-error behavior. See internal/advice/advice_test.go for the
		// unit-level Heuristic/Generate coverage this complements.
		{"advice-no-llm", []string{"advice", "--ollama-url", "http://127.0.0.1:1"}},
		{"advice-no-llm-json", []string{"advice", "--ollama-url", "http://127.0.0.1:1", "--json"}},
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
			// Cleared unconditionally, not just for the advice cases: a
			// stray cloud API key in the environment running this test
			// must never turn any subcommand into a real network call to
			// a paid provider.
			for _, keyEnv := range llm.CloudAPIKeyEnvVars() {
				cmd.Env = append(cmd.Env, keyEnv+"=")
			}
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
// every subtest and every test function in this package (TestCLISmoke,
// TestDashboardSmoke) that needs it, so the smoke test suite doesn't pay a
// full build more than once. Deliberately uses os.MkdirTemp, not
// t.TempDir(): the latter is torn down when the specific *testing.T that
// created it finishes, which broke the very first time a second test
// function reused this helper after TestCLISmoke completed and deleted
// the binary out from under it. TestMain below removes this directory
// once the whole test binary exits, so it isn't left behind either.
func buildCLIOnce(t *testing.T) string {
	t.Helper()
	cliOnce.Do(func() {
		dir, err := os.MkdirTemp("", "vitals-smoke-test-")
		if err != nil {
			cliBuildE = err
			return
		}
		name := "vitals-smoke-test"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		cliPath = filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", cliPath, ".")
		out, buildErr := cmd.CombinedOutput()
		if buildErr != nil {
			cliBuildE = buildErr
			t.Logf("build output:\n%s", out)
		}
	})
	if cliBuildE != nil {
		t.Fatalf("building the CLI for smoke testing: %v", cliBuildE)
	}
	return cliPath
}

// TestMain lets buildCLIOnce's shared binary live in a directory outside
// any single test's t.TempDir() lifecycle (see buildCLIOnce) while still
// cleaning it up once, after every test in this package has run.
func TestMain(m *testing.M) {
	code := m.Run()
	if cliPath != "" {
		_ = os.RemoveAll(filepath.Dir(cliPath))
	}
	os.Exit(code)
}
