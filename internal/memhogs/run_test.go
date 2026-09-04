package memhogs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"

	"vitals/internal/ui"
)

// captureStdout swaps os.Stdout and os.Stderr for the duration of f and
// returns everything written to either — once() prints directly, and ui.Errf
// writes to stderr, so capturing stdout alone would miss output a user sees.
// Drained concurrently, not after f returns, to avoid a pipe-buffer deadlock
// on Windows (see internal/monitor's identical helper).
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	f()
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-done
}

// fakeProc is a canned procSource — no real process table.
type fakeProc struct {
	pid       int32
	rss       uint64
	name, cmd string
	exe       string
	memErr    error
}

func (p fakeProc) PID() int32 { return p.pid }
func (p fakeProc) MemoryInfo() (*process.MemoryInfoStat, error) {
	if p.memErr != nil {
		return nil, p.memErr
	}
	return &process.MemoryInfoStat{RSS: p.rss}, nil
}
func (p fakeProc) Name() (string, error)    { return p.name, nil }
func (p fakeProc) Cmdline() (string, error) { return p.cmd, nil }
func (p fakeProc) Exe() (string, error)     { return p.exe, nil }

func stubSource(procs []procSource, procErr error) source {
	return source{
		processes:  func() ([]procSource, error) { return procs, procErr },
		readCgroup: func(int32) string { return "" },
		virtualMemory: func() (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 16 << 30, Used: 8 << 30, Available: 8 << 30, UsedPercent: 50}, nil
		},
		swapMemory: func() (*mem.SwapMemoryStat, error) {
			return &mem.SwapMemoryStat{Total: 4 << 30, Used: 1 << 30, UsedPercent: 25}, nil
		},
		newSignalContext: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		},
	}
}

func TestOnceRendersAllThreeSectionsAndProtectsCriticalProcesses(t *testing.T) {
	isolateConfigDir(t)
	procs := []procSource{
		fakeProc{pid: 10, rss: 900 << 20, name: "Slack", cmd: "/Applications/Slack.app/Contents/MacOS/Slack", exe: "/Applications/Slack.app/Contents/MacOS/Slack"},
		fakeProc{pid: 11, rss: 400 << 20, name: "Google Chrome Helper (Renderer)", cmd: "Chrome Helper (Renderer)", exe: "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Helper (Renderer).app/Contents/MacOS/x"},
		fakeProc{pid: 12, rss: 300 << 20, name: "WindowServer", cmd: "WindowServer"},
		fakeProc{pid: 13, rss: 50 << 20, name: "kernel_task", cmd: "kernel_task"},
		fakeProc{pid: 14, rss: 0, name: "zero-rss-skipped", cmd: "x"},
	}
	out := ui.StripANSI(captureStdout(t, func() {
		if err := once(stubSource(procs, nil), 10); err != nil {
			t.Fatalf("once: %v", err)
		}
	}))

	for _, want := range []string{
		"APPLICATION FAMILY FOOTPRINTS",
		"TOP 10 PROCESSES BY RESIDENT MEMORY",
		"SYSTEM MEMORY & REMEDIES",
		"Slack",
		"do NOT kill (display server)",
		"do NOT kill (kernel thermal control)",
		"Total RAM",
		"Swap used",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("once() output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "zero-rss-skipped") {
		t.Errorf("a process with RSS 0 should be skipped, got:\n%s", out)
	}
}

func TestOnceReturnsErrorWhenProcessEnumerationFails(t *testing.T) {
	err := once(stubSource(nil, errors.New("boom")), 5)
	if err == nil || !strings.Contains(err.Error(), "enumerate processes") {
		t.Fatalf("want an enumerate-processes error, got %v", err)
	}
}

func TestOnceToleratesMissingMemoryAndSwapReadings(t *testing.T) {
	isolateConfigDir(t)
	src := stubSource([]procSource{fakeProc{pid: 1, rss: 10 << 20, name: "x", cmd: "x"}}, nil)
	src.virtualMemory = func() (*mem.VirtualMemoryStat, error) { return nil, errors.New("no vm") }
	src.swapMemory = func() (*mem.SwapMemoryStat, error) { return nil, errors.New("no swap") }

	out := ui.StripANSI(captureStdout(t, func() {
		if err := once(src, 5); err != nil {
			t.Fatalf("once: %v", err)
		}
	}))
	if strings.Contains(out, "Total RAM") {
		t.Errorf("no vm reading — the RAM block should be skipped, got:\n%s", out)
	}
	if !strings.Contains(out, "SYSTEM MEMORY & REMEDIES") {
		t.Errorf("the remedies section should still print without a vm reading:\n%s", out)
	}
}

func TestRunNonWatchGoesThroughDefaultSourceEndToEnd(t *testing.T) {
	isolateConfigDir(t)
	// One real pass over the live process table — the memcheck-style single
	// end-to-end call that defaultSource's wiring is otherwise never exercised by.
	out := captureStdout(t, func() {
		if err := Run(Options{Top: 1}); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if !strings.Contains(ui.StripANSI(out), "APPLICATION FAMILY FOOTPRINTS") {
		t.Errorf("Run() produced no report:\n%s", out)
	}
}

func TestRunWatchStopsWhenTheSignalContextIsDone(t *testing.T) {
	isolateConfigDir(t)
	src := stubSource([]procSource{fakeProc{pid: 1, rss: 10 << 20, name: "x", cmd: "x"}}, nil)
	src.newSignalContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already done — watch should render once, then return
		return ctx, cancel
	}
	done := make(chan error, 1)
	go func() {
		done <- captureRunWatch(t, src)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run(watch): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run() with an already-cancelled signal context did not return")
	}
}

// captureRunWatch runs run() in --watch mode, draining stdout/stderr
// concurrently via captureStdout so a run producing enough output to fill
// the OS pipe buffer (a --watch loop can render dozens of times before its
// context times out) can't deadlock on the write end — the exact "os.Pipe
// deadlock in captureStdout helpers" class of bug this file's own
// captureStdout exists to avoid; an earlier version of this helper swapped
// os.Stdout for a pipe's write end without ever draining the read end,
// which passed on macOS/Linux's larger default pipe buffers but deadlocked
// for real on Windows CI.
func captureRunWatch(t *testing.T, src source) error {
	var runErr error
	captureStdout(t, func() {
		runErr = run(src, Options{Watch: true, Interval: time.Millisecond})
	})
	return runErr
}

func TestWatchLoopsThroughTheTickerThenStops(t *testing.T) {
	isolateConfigDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	src := stubSource([]procSource{fakeProc{pid: 1, rss: 10 << 20, name: "x", cmd: "x"}}, nil)
	done := make(chan error, 1)
	go func() {
		var err error
		captureStdout(t, func() { err = watch(ctx, src, Options{Interval: time.Millisecond}) })
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch did not stop after its context timed out")
	}
}

func TestWatchKeepsGoingAfterAFailedIterationThenExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// processes() errors every call — once() returns an error, watch() must
	// report it via ui.Errf and still exit cleanly on ctx.Done.
	err := watch(ctx, stubSource(nil, errors.New("boom")), Options{Interval: time.Millisecond})
	if err != nil {
		t.Fatalf("watch should return nil on ctx.Done even after a failed once(): %v", err)
	}
}

func TestLinuxAppFromCgroupIgnoresNonAppScopes(t *testing.T) {
	cases := map[string]string{
		"0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-firefox-1a2b.scope": "firefox",
		"0::/user.slice/user-1000.slice/session-2.scope":                                    "", // no app scope
		"0::/system.slice/app-user@1000.service":                                            "", // @-wrapper, not an app
		"0::/user.slice/snap.spotify.spotify.abc.scope":                                     "spotify",
	}
	for cgroup, want := range cases {
		if got := linuxAppFromCgroup(cgroup); got != want {
			t.Errorf("linuxAppFromCgroup(%q) = %q, want %q", cgroup, got, want)
		}
	}
}

func TestWindowsAppFromPathFallbacksAndRejects(t *testing.T) {
	cases := map[string]string{
		`C:\Program Files\Microsoft VS Code\Code.exe`:       "Microsoft VS Code",
		`C:\Users\x\AppData\Local\Programs\Slack\slack.exe`: "Slack",
		`C:\tools\myapp\myapp.exe`:                          "myapp", // parent-folder fallback
		`C:\Windows\System32\svchost.exe`:                   "",      // generic parent rejected
		`solo.exe`:                                          "",      // fewer than 2 segments
	}
	for p, want := range cases {
		if got := windowsAppFromPath(p); got != want {
			t.Errorf("windowsAppFromPath(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestParseFamiliesRejectsAnEmptyName(t *testing.T) {
	if _, err := parseFamilies([]byte(`[{"name":"","pattern":"x","stop":"kill"}]`)); err == nil {
		t.Error("expected an error for a family with an empty name")
	}
}

func TestFamiliesMergesAUserOverrideFile(t *testing.T) {
	dir := isolateConfigDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "vitals"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A brand-new family name — families() must return the embedded base plus this.
	fam := `[{"name":"ZZ Test Family","pattern":"zz-vitals-test","stop":"kill"}]`
	if err := os.WriteFile(filepath.Join(dir, "vitals", "families.json"), []byte(fam), 0o644); err != nil {
		t.Fatal(err)
	}
	got := families()
	var found bool
	for _, f := range got {
		if f.name == "ZZ Test Family" {
			found = true
		}
	}
	if !found {
		t.Errorf("families() did not merge the user override file (%d families, none named ZZ Test Family)", len(got))
	}
	if len(got) < 2 {
		t.Errorf("families() should return the embedded base merged with the override, got only %d", len(got))
	}
}

func TestOnceHandlesEmptyCmdlineAndTruncatesToTopN(t *testing.T) {
	isolateConfigDir(t)
	procs := []procSource{
		fakeProc{pid: 1, rss: 500 << 20, name: "alpha", cmd: "", exe: "/opt/alpha/alpha"},
		fakeProc{pid: 2, rss: 400 << 20, name: "Code Helper (Plugin)", cmd: "Code Helper (Plugin)", exe: "/x"},
		fakeProc{pid: 3, rss: 300 << 20, name: "chrome", cmd: "Chrome Helper (GPU)", exe: "/x"},
		fakeProc{pid: 4, rss: 200 << 20, name: "beta", cmd: "beta", exe: "/opt/beta/beta"},
	}
	out := ui.StripANSI(captureStdout(t, func() {
		if err := once(stubSource(procs, nil), 3); err != nil {
			t.Fatalf("once: %v", err)
		}
	}))
	if !strings.Contains(out, "VS Code extension host") {
		t.Errorf("describe() should label the Code Helper (Plugin) process:\n%s", out)
	}
	if !strings.Contains(out, "Chrome GPU process") {
		t.Errorf("describe() should label the Chrome GPU process:\n%s", out)
	}
	// topN == 3: the fourth-heaviest process (beta) is past the cap in both
	// the family and process sections, so it must not appear.
	if strings.Contains(out, "beta") {
		t.Errorf("topN=3 should have truncated the lists before beta:\n%s", out)
	}
}

func TestReadCgroupForCoversEveryBranch(t *testing.T) {
	fail := func(string) ([]byte, error) { return nil, errors.New("nope") }
	ok := func(string) ([]byte, error) { return []byte("0::/user.slice/app-firefox.scope"), nil }

	if got := readCgroupFor("darwin", 1, ok); got != "" {
		t.Errorf("non-Linux should short-circuit to empty, got %q", got)
	}
	if got := readCgroupFor("linux", 1, fail); got != "" {
		t.Errorf("a failed read should return empty, got %q", got)
	}
	if got := readCgroupFor("linux", 1, ok); !strings.Contains(got, "firefox") {
		t.Errorf("a successful Linux read should return the cgroup body, got %q", got)
	}
	// The default wiring: never panics, empty off Linux.
	if got := realReadCgroup(int32(os.Getpid())); runtime.GOOS != "linux" && got != "" {
		t.Errorf("realReadCgroup off Linux should be empty, got %q", got)
	}
}
