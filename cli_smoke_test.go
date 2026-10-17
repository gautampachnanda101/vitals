package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"vitals/internal/doctor"
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
		{"licenses", []string{"licenses"}},
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
		{"view", []string{"view", "--ollama-url", "http://127.0.0.1:1"}},
		{"containers", []string{"containers"}},
		{"containers-json", []string{"containers", "--json"}},
		{"heal-dry-run", []string{"heal", "--dry-run"}},
		{"dupes-json", []string{"dupes", "--root", dupesRoot, "--json"}},
		// Safe because dupesRoot is guaranteed empty: no files exist to
		// hardlink, so this only exercises the flag-wiring/confirmation
		// path, never ApplyHardlinks' actual file mutation.
		{"dupes-hardlink-empty-dir", []string{"dupes", "--root", dupesRoot, "--hardlink", "--yes"}},
		{"clean-dry-run", []string{"clean", "--categories", "caches", "--dry-run"}},
		{"clean-plan-json", []string{"clean", "--categories", "caches", "--dry-run", "--json"}},
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
		{"advice-no-llm-scan-json", []string{"advice", "--ollama-url", "http://127.0.0.1:1", "--scan", "--top", "5", "--scan-budget", "1s", "--json"}},
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
			switch {
			case strings.HasPrefix(c.name, "doctor"),
				strings.HasSuffix(c.name, "-json") && isResourceFocus(c.args[0]),
				c.name == "view", c.name == "containers", c.name == "containers-json",
				c.name == "heal-dry-run":
				// verdict-carrying commands exit 0/1/2 by design
				allowed = []int{0, 1, 2}
			}
			if !slices.Contains(allowed, exitCode) {
				t.Errorf("vitals %s exited %d, want one of %v:\n%s", strings.Join(c.args, " "), exitCode, allowed, out.String())
			}
		})
	}
}

func TestDiskScanCLIReportsExactFixtureWithoutMutation(t *testing.T) {
	bin := buildCLIOnce(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"large/nested.txt": strings.Repeat("a", 6000),
		"small/tiny.txt":   "1234567890",
		"loose.txt":        strings.Repeat("b", 3000),
	}
	for name, contents := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// On Windows NTFS, directory modification timestamps update asynchronously after file writes.
	time.Sleep(100 * time.Millisecond)
	before := make(map[string]os.FileInfo)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err == nil {
			before[path] = info
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "scan.json")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "disk", "--scan", "--root", root, "--top", "1", "--json", "--output", output)
	scratch := t.TempDir()
	cmd.Env = append(os.Environ(), "HOME="+scratch, "APPDATA="+scratch, "XDG_CONFIG_HOME=", "NO_COLOR=1")
	for _, key := range llm.CloudAPIKeyEnvVars() {
		cmd.Env = append(cmd.Env, key+"=")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	processCode := 0
	if err != nil {
		var exit *exec.ExitError
		if !isExitError(err, &exit) || exit.ExitCode() > 2 {
			t.Fatalf("scan CLI: %v %s", err, stderr.String())
		}
		processCode = exit.ExitCode()
	}
	var result doctor.JSONEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("JSON: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	healthCode := 0
	for _, finding := range result.Findings {
		healthCode = max(healthCode, finding.Severity.ExitCode())
	}
	if processCode != healthCode || result.ExitCode != healthCode {
		t.Fatalf("complete scan exit: process=%d JSON=%d, want health verdict %d", processCode, result.ExitCode, healthCode)
	}
	scan := result.DiskUsage
	if scan == nil || !scan.Complete || scan.ObservedBytes != 9010 || len(scan.Directories) != 1 ||
		filepath.Base(scan.Directories[0].Path) != "large" || scan.Directories[0].Bytes != 6000 ||
		len(scan.Files) != 1 || scan.Files[0].Bytes != 6000 || len(scan.RootFiles) != 1 || scan.RootFiles[0].Bytes != 3000 {
		t.Fatalf("wrong fixture accounting: %+v", scan)
	}
	saved, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var savedResult doctor.JSONEnvelope
	if err := json.Unmarshal(saved, &savedResult); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, savedResult) {
		t.Fatal("scan stdout differs from --output")
	}
	count := 0
	entries := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		info, err := entry.Info()
		if err != nil {
			return err
		}
		original, exists := before[path]
		mtimeEqual := original.ModTime().Equal(info.ModTime())
		if entry.IsDir() && runtime.GOOS == "windows" {
			// On Windows NTFS, directory ModTime can shift as child file handles open and close during directory traversal.
			mtimeEqual = true
		}
		if !exists || original.Mode() != info.Mode() || !mtimeEqual ||
			(!entry.IsDir() && original.Size() != info.Size()) {
			t.Errorf("changed or unexpected metadata: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		count++
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(data) != files[filepath.ToSlash(rel)] {
			t.Errorf("changed or unexpected file: %s", path)
		}
		return nil
	})
	if err != nil || count != len(files) || entries != len(before) {
		t.Fatalf("fixture tree changed: files %d entries %d error %v", count, entries, err)
	}
}

func TestDiskScanCLIIncompleteAndMissingRootExitCodes(t *testing.T) {
	bin := buildCLIOnce(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, root, budget string
		wantError          bool
	}{
		{"budget", root, "1ns", false},
		{"missing", filepath.Join(root, "missing"), "1s", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, "disk", "--scan", "--root", tc.root,
				"--scan-budget", tc.budget, "--json", "--ollama-url", "http://127.0.0.1:1")
			scratch := t.TempDir()
			cmd.Env = append(os.Environ(), "HOME="+scratch, "APPDATA="+scratch, "XDG_CONFIG_HOME=")
			for _, key := range llm.CloudAPIKeyEnvVars() {
				cmd.Env = append(cmd.Env, key+"=")
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if !isExitError(err, &exit) {
				t.Fatalf("incomplete scan must exit nonzero: %v\n%s", err, stderr.String())
			}
			var result doctor.JSONEnvelope
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("JSON: %v\n%s", err, stdout.String())
			}
			if result.DiskUsage == nil || result.DiskUsage.Complete {
				t.Fatalf("missing incomplete result: %+v", result.DiskUsage)
			}
			wantCode := 1
			for _, finding := range result.Findings {
				wantCode = max(wantCode, finding.Severity.ExitCode())
			}
			if tc.wantError {
				wantCode = 2
				if result.DiskUsage.ErrorCount == 0 || len(result.DiskUsage.Errors) == 0 {
					t.Fatal("missing root must provide structured access errors")
				}
			} else if !slices.Contains(result.DiskUsage.StopReasons, "budget_exceeded") {
				t.Fatalf("missing budget reason: %v", result.DiskUsage.StopReasons)
			}
			if exit.ExitCode() != wantCode || result.ExitCode != wantCode {
				t.Fatalf("exit process=%d JSON=%d want=%d", exit.ExitCode(), result.ExitCode, wantCode)
			}
		})
	}
}

// TestCLISmokeLaunchWithNoCompanionTool runs `vitals live` / `vitals
// explore` with an empty PATH so no companion tool can be found — the
// cross-OS check that the handoff commands fail with a helpful,
// vitals-native pointer rather than a bare error or a hang.
func TestCLISmokeLaunchWithNoCompanionTool(t *testing.T) {
	bin := buildCLIOnce(t)
	scratch := t.TempDir()
	for _, c := range []struct {
		name, cmd, wantTool, wantFallback string
	}{
		{"live", "live", "btop", "vitals top"},
		{"explore", "explore", "gdu", "vitals disk"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, c.cmd)
			// Empty PATH: exec.LookPath finds nothing, so `installed()` is
			// false for every candidate and launch() takes the not-installed
			// branch. (The already-built binary needs no PATH to run.)
			cmd.Env = []string{"PATH=", "HOME=" + scratch, "APPDATA=" + scratch, "NO_COLOR=1"}
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()

			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("vitals %s hung with no tool installed:\n%s", c.cmd, out.String())
			}
			if err == nil {
				t.Fatalf("vitals %s should fail when no companion tool is installed:\n%s", c.cmd, out.String())
			}
			s := out.String()
			if !strings.Contains(s, c.wantTool) || !strings.Contains(s, "vitals tools install") {
				t.Errorf("vitals %s should name the candidates and how to install one:\n%s", c.cmd, s)
			}
			if !strings.Contains(s, c.wantFallback) {
				t.Errorf("vitals %s should point at the vitals-native fallback %q:\n%s", c.cmd, c.wantFallback, s)
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
