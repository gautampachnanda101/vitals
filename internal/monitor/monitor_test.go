package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

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

// fakeProc is a canned procSource for topProcesses tests — a stand-in for
// the real *process.Process (wrapped by procHandle in production) so tests
// exercise the per-process assembly/sort/filter logic without scanning the
// real process table.
type fakeProc struct {
	pid     int32
	cpuPct  float64
	mem     *process.MemoryInfoStat
	memErr  error
	memPct  float32
	name    string
	user    string
	threads int32
	cmdline string
}

func (f fakeProc) PID() int32                                   { return f.pid }
func (f fakeProc) Percent(time.Duration) (float64, error)       { return f.cpuPct, nil }
func (f fakeProc) MemoryInfo() (*process.MemoryInfoStat, error) { return f.mem, f.memErr }
func (f fakeProc) MemoryPercent() (float32, error)              { return f.memPct, nil }
func (f fakeProc) Name() (string, error)                        { return f.name, nil }
func (f fakeProc) Username() (string, error)                    { return f.user, nil }
func (f fakeProc) NumThreads() (int32, error)                   { return f.threads, nil }
func (f fakeProc) Cmdline() (string, error)                     { return f.cmdline, nil }

// fakeSource builds a source with sane non-nil defaults for every field so
// individual tests only need to override the one gopsutil call they care
// about.
func fakeSource(overrides func(*source)) source {
	src := source{
		hostInfo: func() (*host.InfoStat, error) {
			return &host.InfoStat{Hostname: "test-host", Platform: "testos", PlatformVersion: "1.0", KernelArch: "x86_64", Uptime: 3600, Procs: 42}, nil
		},
		loadAvg: func() (*load.AvgStat, error) {
			return &load.AvgStat{Load1: 1, Load5: 2, Load15: 3}, nil
		},
		cpuCounts: func(bool) (int, error) { return 8, nil },
		cpuPercent: func(time.Duration, bool) ([]float64, error) {
			return []float64{42.5}, nil
		},
		virtualMemory: func() (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 16 << 30, Used: 8 << 30, UsedPercent: 50, Wired: 1 << 30, Active: 2 << 30}, nil
		},
		swapMemory: func() (*mem.SwapMemoryStat, error) {
			return &mem.SwapMemoryStat{Total: 4 << 30, Used: 1 << 30, UsedPercent: 25}, nil
		},
		diskIOCounters: func() (map[string]disk.IOCountersStat, error) {
			return map[string]disk.IOCountersStat{"disk0": {ReadBytes: 100, WriteBytes: 200}}, nil
		},
		netIOCounters: func(bool) ([]net.IOCountersStat, error) {
			return []net.IOCountersStat{{Name: "en0", BytesRecv: 100, BytesSent: 200}}, nil
		},
		processes: func() ([]procSource, error) { return nil, nil },
		newSignalContext: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		},
	}
	if overrides != nil {
		overrides(&src)
	}
	return src
}

func TestTopProcessesBuildsSortsFiltersAndCaps(t *testing.T) {
	procs := []procSource{
		fakeProc{pid: 1, cpuPct: 90, mem: &process.MemoryInfoStat{RSS: 100}, memPct: 5, name: "alpha", user: "root", threads: 2, cmdline: "alpha --flag"},
		fakeProc{pid: 2, cpuPct: 10, mem: &process.MemoryInfoStat{RSS: 900}, memPct: 50, name: "beta", user: "root", threads: 1, cmdline: ""},
		fakeProc{pid: 3, cpuPct: 50, mem: nil, memErr: errors.New("no access")}, // skipped: MemoryInfo fails
	}
	src := fakeSource(func(s *source) {
		s.processes = func() ([]procSource, error) { return procs, nil }
	})

	t.Run("sorted by cpu descending, mem-error process skipped", func(t *testing.T) {
		out := topProcesses(src, Options{SortBy: "cpu", Top: 15, Interval: time.Millisecond})
		if len(out) != 2 {
			t.Fatalf("want 2 processes (one skipped for MemoryInfo error), got %d: %+v", len(out), out)
		}
		if out[0].Name != "alpha" || out[1].Name != "beta" {
			t.Errorf("want alpha before beta by CPU%%, got %v then %v", out[0].Name, out[1].Name)
		}
		if out[1].Command != "beta" {
			t.Errorf("empty Cmdline should fall back to Name, got %q", out[1].Command)
		}
	})

	t.Run("sorted by mem descending", func(t *testing.T) {
		out := topProcesses(src, Options{SortBy: "mem", Top: 15, Interval: time.Millisecond})
		if out[0].Name != "beta" {
			t.Errorf("want beta first by RSS, got %v", out[0].Name)
		}
	})

	t.Run("caps at Top", func(t *testing.T) {
		out := topProcesses(src, Options{SortBy: "cpu", Top: 1, Interval: time.Millisecond})
		if len(out) != 1 {
			t.Errorf("want exactly 1 process (Top=1 cap), got %d", len(out))
		}
	})
}

func TestTopProcessesReturnsNilWhenProcessesFails(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.processes = func() ([]procSource, error) { return nil, errors.New("boom") }
	})
	if out := topProcesses(src, Options{Interval: time.Millisecond}); out != nil {
		t.Errorf("want nil when processes() fails, got %+v", out)
	}
}

func TestReadDiskCountersMapsAndHandlesError(t *testing.T) {
	src := fakeSource(nil)
	got := readDiskCounters(src)
	if len(got) != 1 || got[0].Name != "disk0" || got[0].ReadBytes != 100 {
		t.Errorf("readDiskCounters = %+v, want one disk0 entry", got)
	}

	src.diskIOCounters = func() (map[string]disk.IOCountersStat, error) { return nil, errors.New("boom") }
	if got := readDiskCounters(src); got != nil {
		t.Errorf("readDiskCounters on error = %+v, want nil", got)
	}
}

func TestReadNetCountersSkipsIdleInterfacesAndHandlesError(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.netIOCounters = func(bool) ([]net.IOCountersStat, error) {
			return []net.IOCountersStat{
				{Name: "en0", BytesRecv: 10, BytesSent: 20},
				{Name: "lo0", BytesRecv: 0, BytesSent: 0},
			}, nil
		}
	})
	got := readNetCounters(src)
	if len(got) != 1 || got[0].Name != "en0" {
		t.Errorf("readNetCounters = %+v, want only en0 (lo0 never carried a byte)", got)
	}

	src.netIOCounters = func(bool) ([]net.IOCountersStat, error) { return nil, errors.New("boom") }
	if got := readNetCounters(src); got != nil {
		t.Errorf("readNetCounters on error = %+v, want nil", got)
	}
}

func TestSampleAssemblesASnapshotFromTheSource(t *testing.T) {
	src := fakeSource(nil)
	snap, err := sample(src, Options{Interval: time.Millisecond, Top: 5, SortBy: "cpu"})
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if snap.Host.Hostname != "test-host" {
		t.Errorf("Host.Hostname = %q, want test-host", snap.Host.Hostname)
	}
	if snap.Host.Load1 != 1 {
		t.Errorf("Host.Load1 = %v, want 1", snap.Host.Load1)
	}
	if snap.CPU.Cores != 8 || snap.CPU.UsedPct != 42.5 {
		t.Errorf("CPU = %+v, want cores=8 usedPct=42.5", snap.CPU)
	}
	if snap.Memory.UsedBytes != 8<<30 {
		t.Errorf("Memory.UsedBytes = %v, want %v", snap.Memory.UsedBytes, 8<<30)
	}
	if snap.Swap.UsedBytes != 1<<30 {
		t.Errorf("Swap.UsedBytes = %v, want %v", snap.Swap.UsedBytes, 1<<30)
	}
	if len(snap.DiskIO) != 1 || snap.DiskIO[0].Name != "disk0" {
		t.Errorf("DiskIO = %+v, want one disk0 entry", snap.DiskIO)
	}
	if len(snap.NetIO) != 1 || snap.NetIO[0].Name != "en0" {
		t.Errorf("NetIO = %+v, want one en0 entry", snap.NetIO)
	}
}

func TestSampleFallsBackToSleepWhenCPUPercentFails(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.cpuPercent = func(time.Duration, bool) ([]float64, error) { return nil, errors.New("boom") }
	})
	snap, err := sample(src, Options{Interval: time.Millisecond})
	if err != nil {
		t.Fatalf("sample: %v", err)
	}
	if snap.CPU.UsedPct != 0 {
		t.Errorf("CPU.UsedPct = %v, want 0 when cpuPercent fails", snap.CPU.UsedPct)
	}
}

func TestSampleTreatsHostLoadMemAndSwapFailuresAsNonFatal(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.hostInfo = func() (*host.InfoStat, error) { return nil, errors.New("x") }
		s.loadAvg = func() (*load.AvgStat, error) { return nil, errors.New("x") }
		s.virtualMemory = func() (*mem.VirtualMemoryStat, error) { return nil, errors.New("x") }
		s.swapMemory = func() (*mem.SwapMemoryStat, error) { return nil, errors.New("x") }
	})
	snap, err := sample(src, Options{Interval: time.Millisecond})
	if err != nil {
		t.Fatalf("sample: %v, want nil (every field here is best-effort)", err)
	}
	if snap.Host.Hostname != "" {
		t.Errorf("Host.Hostname = %q, want zero-value when hostInfo fails", snap.Host.Hostname)
	}
	if snap.Memory.TotalBytes != 0 {
		t.Errorf("Memory = %+v, want zero-value when virtualMemory fails", snap.Memory)
	}
}

func TestRunSingleShotSamplesAndEmitsJSON(t *testing.T) {
	src := fakeSource(nil)
	out := captureStdout(t, func() {
		if err := run(src, Options{Interval: time.Millisecond, JSON: true}); err != nil {
			t.Errorf("run: %v", err)
		}
	})
	var snap Snapshot
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("run --json output is not valid JSON: %v\n%s", err, out)
	}
	if snap.Host.Hostname != "test-host" {
		t.Errorf("run() snapshot host = %q, want test-host", snap.Host.Hostname)
	}
}

func TestRunAppliesDefaultsForZeroValueOptions(t *testing.T) {
	// Top<=0, Interval<=0, and any SortBy other than "mem" should fall back
	// to their documented defaults rather than propagating zero values
	// downstream (a zero Interval would mean a zero-length CPU sample
	// window).
	src := fakeSource(nil)
	out := captureStdout(t, func() {
		if err := run(src, Options{JSON: true}); err != nil {
			t.Errorf("run: %v", err)
		}
	})
	if !strings.Contains(out, "test-host") {
		t.Errorf("run() with zero-value opts should still produce output, got %q", out)
	}
}

func TestRunWatchLoopsUntilContextDoneThenReturns(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.newSignalContext = func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 15*time.Millisecond)
		}
	})
	out := captureStdout(t, func() {
		if err := run(src, Options{Watch: true, Interval: 5 * time.Millisecond, JSON: true}); err != nil {
			t.Errorf("run watch: %v", err)
		}
	})
	// One emit for the initial sample plus at least one more 5ms-interval
	// tick inside the 15ms window before the injected context expires.
	if n := strings.Count(out, `"hostname"`); n < 2 {
		t.Errorf("want at least 2 emitted snapshots before the watch loop exits, got %d in:\n%s", n, out)
	}
}

func TestWatchClearsTheScreenBeforeEachNonJSONEmit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done: exercise exactly one loop iteration
	src := fakeSource(nil)

	out := captureStdout(t, func() {
		if err := watch(ctx, src, Options{Interval: time.Millisecond}); err != nil {
			t.Errorf("watch: %v", err)
		}
	})
	if !strings.Contains(out, "\033[H\033[2J") {
		t.Errorf("watch should clear the screen before each non-JSON emit, got %q", out)
	}
}

func TestDefaultSourceNewSignalContextWiresRealSignalNotify(t *testing.T) {
	ctx, stop := defaultSource.newSignalContext()
	defer stop()
	if ctx == nil {
		t.Fatal("newSignalContext returned a nil context")
	}
	select {
	case <-ctx.Done():
		t.Fatal("a freshly created signal context should not already be done")
	default:
	}
}

func TestRunWithDefaultSourceExercisesTheRealGopsutilWiring(t *testing.T) {
	// Run is a thin pass-through to run(defaultSource, opts); this exercises
	// the real gopsutil calls (including realProcesses/procHandle, since the
	// test binary itself is always at least one running process) end to end
	// the same way a user invoking `vitals monitor` would.
	out := captureStdout(t, func() {
		if err := Run(Options{JSON: true, Interval: 50 * time.Millisecond, Top: 3}); err != nil {
			t.Errorf("Run() = %v, want nil on a real host", err)
		}
	})
	var snap Snapshot
	if err := json.Unmarshal([]byte(out), &snap); err != nil {
		t.Fatalf("Run() --json output is not valid JSON: %v\n%s", err, out)
	}
}
