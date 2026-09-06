package consoleview

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/monitor"
)

func healthyInput() Input {
	return Input{
		Snapshot: doctor.Snapshot{
			CPU:    doctor.CPU{UsedPct: 10, Load1: 1.2, Cores: 8},
			Memory: doctor.Memory{UsedPct: 40, AvailablePct: 55},
			Disks:  []doctor.Disk{{Mount: "/", UsedPct: 45, FreeBytes: 200 << 30}},
		},
		Report: doctor.Analyze(doctor.Snapshot{
			CPU:    doctor.CPU{UsedPct: 10, Load1: 1.2, Cores: 8},
			Memory: doctor.Memory{UsedPct: 40, AvailablePct: 55},
			Disks:  []doctor.Disk{{Mount: "/", UsedPct: 45, FreeBytes: 200 << 30}},
		}),
		Procs: monitor.Snapshot{
			Host: monitor.HostInfo{Hostname: "testbox", OS: "linux", Kernel: "arm64", Uptime: 90000},
			Processes: []monitor.ProcInfo{
				{Name: "chrome", CPUPct: 42, RSSBytes: 1 << 30, PID: 111},
				{Name: "node", CPUPct: 8, RSSBytes: 512 << 20, PID: 222},
			},
		},
		Version: "1.0.0",
		Now:     time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
	}
}

func sickReport(n int) diag.Report {
	var r diag.Report
	r.Add(diag.Finding{Severity: diag.Critical, Title: "Swap thrashing",
		Detail: "swap is full and paging out", Fixes: []string{"quit the hog", "reboot"}})
	for i := 1; i < n; i++ {
		r.Add(diag.Finding{Severity: diag.Warn, Title: "problem"})
	}
	return r
}

// noLineExceedsWidth is the core layout invariant.
func assertNoLineExceedsWidth(t *testing.T, out string, width int) {
	t.Helper()
	for i, line := range strings.Split(out, "\n") {
		// measure display width, not bytes: ANSI codes and multibyte
		// runes don't take columns 1:1, but rune count is the same
		// approximation the renderer uses (ASCII-safe, v1).
		w := runeWidth(line)
		if w > width {
			t.Errorf("line %d is %d cols wide, over the %d-col budget:\n%q", i, w, width, line)
		}
	}
}

func runeWidth(s string) int { return len([]rune(stripANSIForTest(s))) }

func stripANSIForTest(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc && (r == 'm' || r == 'K' || r == 'J'):
			inEsc = false
		case inEsc:
			// swallow
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestRenderHealthyMachineFitsAndLeadsWithTheVerdict(t *testing.T) {
	out := Render(healthyInput(), 100, 40, true)
	if !strings.Contains(out, "healthy") {
		t.Errorf("healthy machine should say so:\n%s", out)
	}
	if !strings.Contains(out, "TOP PROCESSES") || !strings.Contains(out, "chrome") {
		t.Errorf("expected the process table:\n%s", out)
	}
	if !strings.Contains(out, "CPU") || !strings.Contains(out, "MEMORY") || !strings.Contains(out, "DISK") {
		t.Errorf("expected resource panels:\n%s", out)
	}
	assertNoLineExceedsWidth(t, out, 100)
	// verdict comes before the first panel
	if strings.Index(out, "healthy") > strings.Index(out, "CPU") {
		t.Error("verdict must lead, before the panels")
	}
}

func TestRenderCriticalVerdict(t *testing.T) {
	in := healthyInput()
	in.Report = sickReport(1)
	out := Render(in, 90, 40, true)
	if !strings.Contains(out, "CRITICAL") || !strings.Contains(out, "Swap thrashing") {
		t.Errorf("critical verdict + finding expected:\n%s", out)
	}
	if !strings.Contains(out, "quit the hog") {
		t.Errorf("a finding's fixes must render:\n%s", out)
	}
	assertNoLineExceedsWidth(t, out, 90)
}

func TestRenderNarrowTerminalDropsTo1Up(t *testing.T) {
	out := Render(healthyInput(), 40, 60, true)
	assertNoLineExceedsWidth(t, out, 40)
	// still shows the verdict and at least the CPU panel
	if !strings.Contains(out, "healthy") || !strings.Contains(out, "CPU") {
		t.Errorf("narrow render lost core content:\n%s", out)
	}
}

func TestRenderFitToHeightNeverDropsTheVerdict(t *testing.T) {
	in := healthyInput()
	in.Report = sickReport(6) // 1 critical + 5 warnings
	for _, h := range []int{8, 12, 20, 60} {
		out := Render(in, 100, h, true)
		if !strings.Contains(out, "CRITICAL") {
			t.Errorf("height %d: verdict was dropped:\n%s", h, out)
		}
		if !strings.Contains(out, "Swap thrashing") {
			t.Errorf("height %d: the worst finding was dropped:\n%s", h, out)
		}
		// the process table / panels should be the first things to go
		if h <= 12 && strings.Contains(out, "TOP PROCESSES") {
			t.Errorf("height %d: process table should have been trimmed:\n%s", h, out)
		}
	}
}

func TestRenderUnknownSizeRendersEverything(t *testing.T) {
	in := healthyInput()
	in.Report = sickReport(6)
	// fitHeight false: ignore the height, render it all
	out := Render(in, 100, 5, false)
	if !strings.Contains(out, "TOP PROCESSES") || !strings.Contains(out, "MEMORY") {
		t.Errorf("unknown-size render should include everything regardless of the height arg:\n%s", out)
	}
}

func TestRenderSanitisesHostileProcessNames(t *testing.T) {
	in := healthyInput()
	in.Procs.Processes[0].Name = "evil\x1b[2J\x1b[1;1H\x1b[32mfake"
	in.Procs.Host.Hostname = "box\rwiped"
	out := Render(in, 100, 40, true)
	if strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\r") {
		t.Errorf("control sequences reached the output:\n%q", out)
	}
}

func TestRenderNoProcessesNoPanelsIsStillValid(t *testing.T) {
	in := Input{
		Snapshot: doctor.Snapshot{CPU: doctor.CPU{UsedPct: 5, Cores: 4}, Memory: doctor.Memory{UsedPct: 30}},
		Report:   doctor.Analyze(doctor.Snapshot{CPU: doctor.CPU{UsedPct: 5, Cores: 4}, Memory: doctor.Memory{UsedPct: 30}}),
	}
	out := Render(in, 80, 24, true)
	if out == "" || !strings.Contains(out, "healthy") {
		t.Errorf("a bare snapshot should still render a verdict:\n%s", out)
	}
	assertNoLineExceedsWidth(t, out, 80)
}

func TestRenderTinyWidthClampsToDefault(t *testing.T) {
	out := Render(healthyInput(), 5, 40, true) // absurdly narrow
	// should not panic; clamps to DefaultWrapWidth internally
	if !strings.Contains(out, "healthy") {
		t.Errorf("tiny width still needs a verdict:\n%s", out)
	}
}

func TestHelpers(t *testing.T) {
	if shortDuration(90*time.Second) != "1m" {
		t.Errorf("shortDuration(90s) = %q", shortDuration(90*time.Second))
	}
	if shortDuration(2*time.Hour+15*time.Minute) != "2h 15m" {
		t.Errorf("shortDuration = %q", shortDuration(2*time.Hour+15*time.Minute))
	}
	if shortDuration(50*time.Hour) != "2d 2h" {
		t.Errorf("shortDuration = %q", shortDuration(50*time.Hour))
	}
	if got := clip("abcdef", 4); got != "abc…" {
		t.Errorf("clip = %q", got)
	}
	if got := clip("ab", 10); got != "ab" {
		t.Errorf("clip(no-op) = %q", got)
	}
	if nz("", "x") != "x" || nz("y", "x") != "y" {
		t.Error("nz")
	}
	if diskSev(98) != "critical" || diskSev(92) != "warning" || diskSev(50) != "ok" {
		t.Error("diskSev thresholds")
	}
}

func TestProcRefShort(t *testing.T) {
	if procRefShort(doctor.ProcRef{}, false) != "—" {
		t.Error("empty ProcRef")
	}
	if !strings.Contains(procRefShort(doctor.ProcRef{Name: "x", CPUPct: 40}, false), "40%") {
		t.Error("byCPU")
	}
	if !strings.Contains(procRefShort(doctor.ProcRef{Name: "x", RSSBytes: 1 << 30}, true), "GB") {
		t.Error("byMem")
	}
}

func TestSetVersionAndRunWiring(t *testing.T) {
	SetVersion("9.9.9")
	if version() != "9.9.9" {
		t.Errorf("SetVersion/version mismatch: %q", version())
	}
	SetVersion("") // empty is ignored
	if version() != "9.9.9" {
		t.Errorf("empty SetVersion should be ignored, got %q", version())
	}

	// Run once against the real collectors, size forced small & known.
	var buf bytes.Buffer
	code := Run(&buf, func() (int, int, bool) { return 100, 30, true }, Options{OllamaURL: "http://127.0.0.1:0"})
	if buf.Len() == 0 {
		t.Error("Run produced no output")
	}
	if code < 0 || code > 2 {
		t.Errorf("Run exit code out of range: %d", code)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("Run output should end with a newline")
	}
}

func TestDefaultSizeReturns(t *testing.T) {
	// under `go test` stdout is a pipe -> ok=false, fallback dims.
	c, r, ok := DefaultSize()
	if ok {
		t.Skip("real terminal in this environment")
	}
	if c <= 0 || r <= 0 {
		t.Errorf("DefaultSize fallback = (%d,%d)", c, r)
	}
}

func TestRenderAllPanelsIncludingOptionalOnes(t *testing.T) {
	in := healthyInput()
	in.Snapshot.Disks = []doctor.Disk{{Mount: "/", UsedPct: 92, FreeBytes: 5 << 30,
		SMART: &doctor.DiskSMART{Passed: false, TempC: 55}}}
	in.Snapshot.Net = []doctor.NetIface{{Name: "en0", RxBytesPerSec: 2 << 20, TxBytesPerSec: 1 << 20}}
	in.Snapshot.Power = doctor.Power{OnBattery: true, Percent: 44, MinutesLeft: 95}
	in.Snapshot.GPUs = []doctor.GPU{{Name: "RTX 4090", UtilPct: 60, VRAMUsed: 8 << 30, VRAMTotal: 24 << 30}}
	in.Report = doctor.Analyze(in.Snapshot)

	out := Render(in, 120, 50, true)
	for _, want := range []string{"NETWORK", "en0", "POWER", "battery", "44%", "1h 35m",
		"GPU", "RTX 4090", "S.M.A.R.T. FAILED", "24.00 GB"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the full render:\n%s", want, out)
		}
	}
	assertNoLineExceedsWidth(t, out, 120)
}

func TestRenderHealthyOKFindingBecomesOneLine(t *testing.T) {
	in := healthyInput() // Analyze returns a single OK "No bottleneck detected"
	out := Render(in, 100, 40, true)
	// the lone OK finding shows as a green line, not a findings block with a mark
	if strings.Count(out, "No bottleneck detected") != 1 {
		t.Errorf("the lone OK finding should render exactly once:\n%s", out)
	}
}

func TestRenderPanelGridPadsUnevenPairs(t *testing.T) {
	in := healthyInput()
	// odd number of panels -> last row is a single panel; the grid must
	// still emit clean lines (no index panic).
	in.Snapshot.Power = doctor.Power{Percent: 80} // adds a 5th panel (cpu/mem/disk + power; net absent)
	in.Report = doctor.Analyze(in.Snapshot)
	out := Render(in, 100, 40, true)
	assertNoLineExceedsWidth(t, out, 100)
	if !strings.Contains(out, "POWER") {
		t.Errorf("odd panel count lost the last panel:\n%s", out)
	}
}

func TestPadRight(t *testing.T) {
	if got := padRight("ab", 5, 2); got != "ab   " {
		t.Errorf("padRight = %q", got)
	}
	if got := padRight("abcdef", 3, 6); got != "abcdef" {
		t.Errorf("padRight(over) = %q", got)
	}
}
