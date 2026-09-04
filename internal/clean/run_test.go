package clean

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout swaps os.Stdout for the duration of f and returns
// everything written to it. Drained concurrently, not after f returns, to
// avoid a pipe-buffer deadlock on Windows — same pattern as internal/
// monitor's, internal/dupes', and internal/memhogs' identical helpers.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

// isolateConfigDir points os.UserConfigDir() at a fresh temp directory —
// Windows reads %AppData%, not $HOME, so both must be set. Matches the
// pattern used across this codebase (internal/doctor, internal/memhogs).
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
}

func TestConfirmParsesYesNoAndEOF(t *testing.T) {
	cases := map[string]bool{"y\n": true, "yes\n": true, "Y\n": true, "n\n": false, "\n": false, "": false}
	for in, want := range cases {
		out := captureStdout(t, func() {
			if got := confirm(strings.NewReader(in)); got != want {
				t.Errorf("confirm(%q) = %v, want %v", in, got, want)
			}
		})
		if !strings.Contains(out, "Continue?") {
			t.Errorf("confirm should print the prompt, got:\n%s", out)
		}
	}
}

func TestRunUsesInjectedHomeDirAndPrintsTheDryRunSummary(t *testing.T) {
	home := t.TempDir()
	d := deps{homeDir: func() (string, error) { return home, nil }}
	out := captureStdout(t, func() {
		if err := run(d, Options{DryRun: true}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, home) {
		t.Errorf("run() should print the injected home dir, got:\n%s", out)
	}
	if !strings.Contains(out, "Would remove") || !strings.Contains(out, "dry-run complete") {
		t.Errorf("run() with DryRun should print the dry-run summary, got:\n%s", out)
	}
}

func TestRunErrorsWhenHomeDirFails(t *testing.T) {
	d := deps{homeDir: func() (string, error) { return "", errors.New("no home") }}
	if err := run(d, Options{}); err == nil || !strings.Contains(err.Error(), "cannot determine home directory") {
		t.Errorf("run() = %v, want a cannot-determine-home-directory error", err)
	}
}

func TestRunPromptsAndAbortsOnNo(t *testing.T) {
	home := t.TempDir()
	d := deps{homeDir: func() (string, error) { return home, nil }, confirmReader: strings.NewReader("n\n")}
	out := captureStdout(t, func() {
		if err := run(d, Options{}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "aborted") {
		t.Errorf("run() with a 'n' answer should report aborted, got:\n%s", out)
	}
}

func TestRunSkipsThePromptWithAssume(t *testing.T) {
	// Assume:true + DryRun:true is the one combination safe to actually
	// run end to end in a test: DryRun guarantees purgeContents/removeTree
	// never touch the real filesystem (see TestApplyDryRunNeverMutatesAndReturnsAStructuredResult's
	// own reasoning), so this proves Run's own prompt-skipping branch
	// without ever needing a real confirm() answer.
	home := t.TempDir()
	d := deps{homeDir: func() (string, error) { return home, nil }, confirmReader: strings.NewReader("")} // EOF would abort if read at all
	out := captureStdout(t, func() {
		if err := run(d, Options{DryRun: true, Assume: true}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if strings.Contains(out, "aborted") {
		t.Errorf("Assume:true should never prompt/abort, got:\n%s", out)
	}
}

func TestRunShowHistoryPrintsTheAuditLogInsteadOfCleaning(t *testing.T) {
	isolateConfigDir(t)
	path, ok := cleanHistoryPath()
	if !ok {
		t.Fatal("cleanHistoryPath() failed in an isolated config dir")
	}
	if err := appendCleanHistoryTo(path, RunRecord{TotalBytes: 42}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := run(defaultDeps, Options{ShowHistory: true}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "CLEAN HISTORY") {
		t.Errorf("run(ShowHistory) should print the history header, got:\n%s", out)
	}
}

func TestPublicRunGoesThroughDefaultDeps(t *testing.T) {
	// One real end-to-end call through Run() -> defaultDeps, proving the
	// real os.UserHomeDir wiring (ShowHistory sidesteps everything after
	// it, so this is safe to call for real without touching the
	// filesystem beyond a config-dir read).
	isolateConfigDir(t)
	out := captureStdout(t, func() {
		if err := Run(Options{ShowHistory: true}); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if !strings.Contains(out, "CLEAN HISTORY") {
		t.Errorf("Run(ShowHistory) produced no output, got:\n%s", out)
	}
}

func TestCleanHistoryPathHistoryAndRecordRunRoundTrip(t *testing.T) {
	isolateConfigDir(t)

	if got := History(); len(got) != 0 {
		t.Errorf("History() with nothing recorded yet = %v, want empty", got)
	}

	recordRun(RunRecord{TotalBytes: 123})
	got := History()
	if len(got) != 1 || got[0].TotalBytes != 123 {
		t.Errorf("History() after recordRun = %+v, want one 123-byte record", got)
	}
}

func TestRecordRunIsBestEffortWhenConfigDirIsUnresolvable(t *testing.T) {
	// os.UserConfigDir() fails when neither HOME nor XDG_CONFIG_HOME (nor
	// the OS-specific equivalents) resolve — recordRun must not panic or
	// otherwise surface that as a fatal error, matching its own "a write
	// failure never fails the clean run itself" contract.
	t.Setenv("HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	recordRun(RunRecord{TotalBytes: 1}) // must not panic
}

func TestOptionalSkipsWhenTheToolIsNotOnPath(t *testing.T) {
	r := &runner{opts: Options{}, lookPath: func(string) (string, error) { return "", errors.New("not found") }}
	out := captureStdout(t, func() { r.optional("brew", "cleanup") })
	if out != "" {
		t.Errorf("optional() should print nothing when the tool isn't on PATH, got:\n%s", out)
	}
}

func TestOptionalReportsWouldRunInDryMode(t *testing.T) {
	calls := 0
	r := &runner{
		opts:     Options{DryRun: true},
		lookPath: func(string) (string, error) { return "/usr/bin/brew", nil },
		runCmd:   func(string, ...string) error { calls++; return nil },
	}
	out := captureStdout(t, func() { r.optional("brew", "cleanup", "-s") })
	if !strings.Contains(out, "would run: brew cleanup -s") {
		t.Errorf("optional() dry-run should print the would-run line, got:\n%s", out)
	}
	if calls != 0 {
		t.Error("optional() must never actually run the command in dry-run mode")
	}
}

func TestOptionalRunsTheCommandForReal(t *testing.T) {
	// A recordingRunCmd proves the exact argv a call site builds without
	// ever shelling out to a real package manager — same technique
	// internal/tools' recordingRunCmd uses for exactly this reason.
	var gotName string
	var gotArgs []string
	r := &runner{
		opts:     Options{},
		lookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
		runCmd: func(name string, args ...string) error {
			gotName, gotArgs = name, args
			return nil
		},
	}
	out := captureStdout(t, func() { r.optional("docker", "system", "prune", "-f") })
	if gotName != "docker" || strings.Join(gotArgs, " ") != "system prune -f" {
		t.Errorf("optional() should have run docker with the exact args, got %q %v", gotName, gotArgs)
	}
	if !strings.Contains(out, "run: docker system prune -f") {
		t.Errorf("optional() should print the run line, got:\n%s", out)
	}
}

func TestReclaimableSummaryGoesThroughTheRealHomeDir(t *testing.T) {
	// One real end-to-end call — measureDirs (the pure core) is already
	// well tested via fixture directory lists; this proves
	// ReclaimableSummary's own os.UserHomeDir + devCacheDirs/osCacheDirs
	// wiring completes without error on a real machine.
	entries, _ := ReclaimableSummary(0) // budget 0: stop immediately, still exercises the wiring
	_ = entries
}

func TestFreeSpaceGoesThroughTheRealDiskUsage(t *testing.T) {
	if got := freeSpace(); got < 0 {
		t.Errorf("freeSpace() = %d, want >= 0", got)
	}
}

func TestReclaimableSummaryZeroBudgetReturnsIncomplete(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Not calling ReclaimableSummary itself here (that always resolves the
	// real home dir) — measureDirs is its already-tested core, so this
	// just confirms the zero-budget "stop before measuring anything"
	// contract still holds via the dirs devCacheDirs would actually build.
	entries, complete := measureDirs(devCacheDirs(home), 0)
	if complete || len(entries) != 0 {
		t.Errorf("measureDirs with a zero budget = %v, %v, want empty and incomplete", entries, complete)
	}
}
