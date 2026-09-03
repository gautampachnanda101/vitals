package monitor

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"vitals/internal/ui"
)

// captureStdout swaps both os.Stdout and os.Stderr for the duration of f and
// returns everything written to either — emit() and Run() print directly
// rather than building a string first (the same convention doctor.Run's
// verdict-printing switch uses), and some of vitals' own print helpers
// (ui.Errf) write to stderr specifically, so capturing stdout alone would
// silently miss output a user would still see in their terminal. Drained
// concurrently, not after f returns — a synchronous write bigger than
// Windows' small default pipe buffer deadlocks otherwise.
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

func TestBar(t *testing.T) {
	cases := []struct {
		pct      float64
		wantPct  string
		wantFill int // number of filled cells expected
	}{
		{0, "0.0%", 0},
		{50, "50.0%", 10},   // green: below the 60% yellow threshold
		{70, "70.0%", 14},   // yellow: 60-84%, never exercised before
		{100, "100.0%", 20}, // red: >= 85%
		{-5, "0.0%", 0},     // clamped low
		{150, "100.0%", 20}, // clamped high
	}
	for _, c := range cases {
		got := ui.StripANSI(bar(c.pct))
		if !strings.Contains(got, c.wantPct) {
			t.Errorf("bar(%v) = %q, want it to contain %q", c.pct, got, c.wantPct)
		}
		if n := strings.Count(got, "█"); n != c.wantFill {
			t.Errorf("bar(%v) has %d filled cells, want %d", c.pct, n, c.wantFill)
		}
	}
}

func TestRate(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0 B/s"},
		{512, "512 B/s"},
		{1536, "1.50 KB/s"},
		{1048576, "1.00 MB/s"},
		{-5, "0 B/s"},
	}
	for _, c := range cases {
		if got := rate(c.in); got != c.want {
			t.Errorf("rate(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMemBreakdownLine(t *testing.T) {
	cases := []struct {
		name string
		in   MemBreakdown
		want []string // substrings that must all appear
		none bool     // true means the result must be empty
	}{
		{"macOS-style", MemBreakdown{Wired: 1 << 30, Active: 2 << 30, Inactive: 3 << 30}, []string{"wired", "active", "inactive"}, false},
		{"linux-style", MemBreakdown{Buffers: 1 << 20, Cached: 5 << 20}, []string{"buffers", "cached"}, false},
		{"all zero — platform reports none of it", MemBreakdown{}, nil, true},
	}
	for _, c := range cases {
		got := memBreakdownLine(c.in)
		if c.none {
			if got != "" {
				t.Errorf("%s: memBreakdownLine = %q, want empty", c.name, got)
			}
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: memBreakdownLine = %q, missing %q", c.name, got, want)
			}
		}
		// A field that's zero on this platform must never appear, or the
		// line implies pressure in a category the OS never actually reported.
		if c.in.Cached == 0 && strings.Contains(got, "cached") {
			t.Errorf("%s: zero field leaked into output: %q", c.name, got)
		}
	}
}

func TestSampleWindow(t *testing.T) {
	if got := (Options{Interval: 5 * time.Second}).sampleWindow(); got != 500*time.Millisecond {
		t.Errorf("sampleWindow with a long interval = %v, want the fixed 500ms CPU-sample window", got)
	}
	if got := (Options{Interval: 200 * time.Millisecond}).sampleWindow(); got != 200*time.Millisecond {
		t.Errorf("sampleWindow with a sub-second interval = %v, want the interval itself so --watch stays responsive", got)
	}
}

func TestEmitJSONEncodesTheSnapshot(t *testing.T) {
	snap := Snapshot{
		Host:   HostInfo{Hostname: "test-host"},
		Memory: MemInfo{TotalBytes: 100, UsedBytes: 50, UsedPct: 50},
	}
	out := captureStdout(t, func() {
		if err := emit(snap, Options{JSON: true}); err != nil {
			t.Fatalf("emit: %v", err)
		}
	})
	var got Snapshot
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("emit --json output is not valid JSON: %v\n%s", err, out)
	}
	if got.Host.Hostname != "test-host" || got.Memory.UsedBytes != 50 {
		t.Errorf("round-tripped snapshot = %+v, want the fields emit was given", got)
	}
}

func TestEmitHumanReadableListsEveryProcessOnce(t *testing.T) {
	snap := Snapshot{
		Host:   HostInfo{Hostname: "test-host", OS: "testos 1.0"},
		Memory: MemInfo{TotalBytes: 100, UsedBytes: 50, UsedPct: 50},
		Processes: []ProcInfo{
			{PID: 1, Name: "alpha", CPUPct: 90, MemPct: 5},
			{PID: 2, Name: "beta", CPUPct: 10, MemPct: 30},
		},
	}
	out := captureStdout(t, func() {
		if err := emit(snap, Options{SortBy: "cpu"}); err != nil {
			t.Fatalf("emit: %v", err)
		}
	})
	plain := ui.StripANSI(out)
	for _, want := range []string{"test-host", "alpha", "beta"} {
		if !strings.Contains(plain, want) {
			t.Errorf("human output missing %q:\n%s", want, plain)
		}
	}
	if n := strings.Count(plain, "\n"); n < 3 {
		t.Errorf("suspiciously short output (%d lines):\n%s", n, plain)
	}
}

func TestEmitHumanReadablePrintsMemBreakdownSwapAndIO(t *testing.T) {
	// The other emit test uses a minimal Snapshot that skips every
	// optional section (mem breakdown, swap, disk/net I/O) — this
	// exercises the branches that only fire when that data is present.
	snap := Snapshot{
		Host:      HostInfo{Hostname: "test-host"},
		Memory:    MemInfo{TotalBytes: 100, UsedBytes: 50, UsedPct: 50},
		MemDetail: MemBreakdown{Wired: 10 << 20},
		Swap:      MemInfo{TotalBytes: 200, UsedBytes: 10, UsedPct: 5},
		// 5 entries each: emit caps the printed list at 4, so this also
		// exercises that "if i >= 4 { break }" line, not just the happy
		// path of a single entry.
		DiskIO: []IORate{
			{Name: "disk0", ReadPerSec: 1024, WritePerSec: 512},
			{Name: "disk1", ReadPerSec: 1, WritePerSec: 1},
			{Name: "disk2", ReadPerSec: 1, WritePerSec: 1},
			{Name: "disk3", ReadPerSec: 1, WritePerSec: 1},
			{Name: "disk4-should-be-capped", ReadPerSec: 1, WritePerSec: 1},
		},
		NetIO: []IORate{
			{Name: "en0", ReadPerSec: 2048, WritePerSec: 1024},
			{Name: "en1", ReadPerSec: 1, WritePerSec: 1},
			{Name: "en2", ReadPerSec: 1, WritePerSec: 1},
			{Name: "en3", ReadPerSec: 1, WritePerSec: 1},
			{Name: "en4-should-be-capped", ReadPerSec: 1, WritePerSec: 1},
		},
	}
	out := ui.StripANSI(captureStdout(t, func() {
		if err := emit(snap, Options{SortBy: "mem"}); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}))
	for _, want := range []string{"wired", "SWAP", "Disk I/O", "disk0", "Network I/O", "en0"} {
		if !strings.Contains(out, want) {
			t.Errorf("emit output missing %q when the data is present, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "disk4-should-be-capped") || strings.Contains(out, "en4-should-be-capped") {
		t.Errorf("emit should cap disk/net I/O at 4 rows each, got:\n%s", out)
	}
}

func TestIODelta(t *testing.T) {
	t.Run("computes per-second rates", func(t *testing.T) {
		prev := []IOStat{{Name: "sda", ReadBytes: 1000, WriteBytes: 2000}}
		curr := []IOStat{{Name: "sda", ReadBytes: 3000, WriteBytes: 5000}}
		got := ioDelta(prev, curr, 2*time.Second)
		if len(got) != 1 {
			t.Fatalf("want 1 row, got %d", len(got))
		}
		if got[0].ReadPerSec != 1000 {
			t.Errorf("ReadPerSec = %v, want 1000", got[0].ReadPerSec)
		}
		if got[0].WritePerSec != 1500 {
			t.Errorf("WritePerSec = %v, want 1500", got[0].WritePerSec)
		}
		if got[0].ReadBytes != 3000 || got[0].WriteBytes != 5000 {
			t.Errorf("cumulative counters not carried through: %+v", got[0])
		}
	})

	t.Run("device unseen in prev yields zero rate", func(t *testing.T) {
		curr := []IOStat{{Name: "eth0", ReadBytes: 9000, WriteBytes: 9000}}
		got := ioDelta(nil, curr, time.Second)
		if got[0].ReadPerSec != 0 || got[0].WritePerSec != 0 {
			t.Errorf("first-seen device should have zero rate, got %+v", got[0])
		}
	})

	t.Run("counter reset clamps to zero, never negative", func(t *testing.T) {
		prev := []IOStat{{Name: "sda", ReadBytes: 5000, WriteBytes: 5000}}
		curr := []IOStat{{Name: "sda", ReadBytes: 100, WriteBytes: 100}}
		got := ioDelta(prev, curr, time.Second)
		if got[0].ReadPerSec < 0 || got[0].WritePerSec < 0 {
			t.Errorf("negative rate after counter reset: %+v", got[0])
		}
	})

	t.Run("zero interval does not divide by zero", func(t *testing.T) {
		prev := []IOStat{{Name: "sda", ReadBytes: 1000, WriteBytes: 1000}}
		curr := []IOStat{{Name: "sda", ReadBytes: 2000, WriteBytes: 2000}}
		got := ioDelta(prev, curr, 0)
		if got[0].ReadPerSec != 0 || got[0].WritePerSec != 0 {
			t.Errorf("want zero rates for zero interval, got %+v", got[0])
		}
	})

	t.Run("sorted by total throughput descending", func(t *testing.T) {
		prev := []IOStat{
			{Name: "slow", ReadBytes: 0, WriteBytes: 0},
			{Name: "fast", ReadBytes: 0, WriteBytes: 0},
		}
		curr := []IOStat{
			{Name: "slow", ReadBytes: 10, WriteBytes: 10},
			{Name: "fast", ReadBytes: 1000, WriteBytes: 1000},
		}
		got := ioDelta(prev, curr, time.Second)
		if got[0].Name != "fast" {
			t.Errorf("want 'fast' device first, got %q", got[0].Name)
		}
	})
}
