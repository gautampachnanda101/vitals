package power

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParseTopPowerUsesTheSecondSampleBlock(t *testing.T) {
	procs := parseTopPower(fixture(t, "top_l2_power.txt"))
	if len(procs) == 0 {
		t.Fatal("parsed no processes")
	}
	// The first sample block reports every process at 0.0; the real
	// numbers are only in the second. If we picked the first block, the
	// top entry would be a random PID-ordered process at 0.0.
	if procs[0].Name != "Copilot" || procs[0].Power < 80 {
		t.Errorf("want Copilot with a high power score from the 2nd block, got %+v", procs[0])
	}
	// A command with embedded spaces / parens must survive intact.
	var sawSpaced bool
	for _, p := range procs {
		if p.Name == "Code Helper (Ren" {
			sawSpaced = true
		}
		if p.PID <= 0 {
			t.Errorf("bad PID in %+v", p)
		}
	}
	if !sawSpaced {
		t.Errorf("a multi-word command name was lost: %+v", procs)
	}
}

func TestParseTopPowerHandlesGarbageAndEmpty(t *testing.T) {
	if got := parseTopPower([]byte("no header here\njust noise\n")); got != nil {
		t.Errorf("want nil for output with no PID/POWER header, got %+v", got)
	}
	if got := parseTopPower(nil); got != nil {
		t.Errorf("want nil for empty input, got %+v", got)
	}
	// Header present but rows malformed -> skipped, not panicking.
	got := parseTopPower([]byte("PID    COMMAND          POWER\nlonely\nnotanint foo bar\n42 good 3.5\nx y notafloat\n"))
	if len(got) != 1 || got[0].PID != 42 || got[0].Power != 3.5 {
		t.Errorf("want just the one well-formed row (short line, bad pid, bad float all skipped), got %+v", got)
	}
}

func TestParseTopPowerStopsAtAFollowingSampleBlock(t *testing.T) {
	// Defensive: a rows section terminated by another "Processes:" banner
	// rather than a blank line must still stop cleanly.
	in := "PID COMMAND POWER\n10 a 5.0\n11 b 4.0\nProcesses: 999 total\n12 c 3.0\n"
	got := parseTopPower([]byte(in))
	if len(got) != 2 || got[0].PID != 10 || got[1].PID != 11 {
		t.Errorf("want the two rows before the next banner, got %+v", got)
	}
}

func TestParseTopPowerSanitisesHostileCommandNames(t *testing.T) {
	got := parseTopPower([]byte("PID COMMAND POWER\n7 \x1b[31mevil\x1b[0m 1.0\n"))
	if len(got) != 1 || got[0].Name == "" {
		t.Fatalf("unexpected parse: %+v", got)
	}
	if got[0].Name != "evil" {
		t.Errorf("terminal escape sequence not stripped from command name: %q", got[0].Name)
	}
}

func TestSampleReturnsFalseOffDarwin(t *testing.T) {
	d := deps{goos: "linux", run: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("run must not be called on a non-darwin platform")
		return nil, nil
	}}
	if procs, ok := sample(d, 5); ok || procs != nil {
		t.Errorf("want (nil,false) on linux, got (%v,%v)", procs, ok)
	}
}

func TestSampleParsesRanksAndCapsOnDarwin(t *testing.T) {
	raw := fixture(t, "top_l2_power.txt")
	var gotArgs []string
	d := deps{goos: "darwin", run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return raw, nil
	}}
	procs, ok := sample(d, 3)
	if !ok {
		t.Fatal("want ok=true on darwin with a good transcript")
	}
	if len(procs) != 3 {
		t.Fatalf("limit not applied: got %d rows", len(procs))
	}
	for i := 1; i < len(procs); i++ {
		if procs[i-1].Power < procs[i].Power {
			t.Errorf("not ranked by power desc: %+v", procs)
		}
	}
	if gotArgs[0] != "top" {
		t.Errorf("expected to shell out to top, got %v", gotArgs)
	}
}

func TestSampleReturnsFalseOnExecErrorOrEmptyParse(t *testing.T) {
	t.Run("exec error", func(t *testing.T) {
		d := deps{goos: "darwin", run: func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("top: command not found")
		}}
		if _, ok := sample(d, 5); ok {
			t.Error("want ok=false when top fails to run")
		}
	})
	t.Run("no parseable rows", func(t *testing.T) {
		d := deps{goos: "darwin", run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("Processes: 1 total\nnonsense\n"), nil
		}}
		if _, ok := sample(d, 5); ok {
			t.Error("want ok=false when the transcript has no PID/POWER block")
		}
	})
}

func TestDefaultDepsIsWired(t *testing.T) {
	if defaultDeps.goos == "" || defaultDeps.run == nil {
		t.Fatal("defaultDeps not fully populated")
	}
	// Exercise the real defaultDeps.run closure once, on every platform,
	// with a command present on any CI runner (`go` — the suite is running
	// under it) so the exec wiring is covered off macOS too.
	out, err := defaultDeps.run(context.Background(), "go", "version")
	if err != nil || !strings.Contains(string(out), "go") {
		t.Errorf("defaultDeps.run did not execute a plain command: out=%q err=%v", out, err)
	}

	// The exported one-liner runs on every platform: on macOS it should
	// return a real reading; anywhere else it must short-circuit to
	// (nil,false) without shelling out.
	procs, ok := Sample(5)
	if defaultDeps.goos != "darwin" {
		if ok || procs != nil {
			t.Errorf("Sample on %s should be (nil,false), got (%v,%v)", defaultDeps.goos, procs, ok)
		}
		return
	}
	if ok && len(procs) == 0 {
		t.Error("Sample reported ok on macOS but returned no processes")
	}
}
