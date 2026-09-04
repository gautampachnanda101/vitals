package doctor

import (
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"vitals/internal/config"
	"vitals/internal/diag"
	"vitals/internal/ui"
)

func TestPct(t *testing.T) {
	if got := ui.StripANSI(pct(50, 70, 90)); got != "50%" {
		t.Errorf("pct(50, 70, 90) = %q, want 50%%", got)
	}
	if got := ui.StripANSI(pct(95, 70, 90)); got != "95%" {
		t.Errorf("pct(95, 70, 90) = %q, want 95%%", got)
	}
}

func TestThrottleNote(t *testing.T) {
	if got := throttleNote(false); got != "" {
		t.Errorf("throttleNote(false) = %q, want empty", got)
	}
	if got := ui.StripANSI(throttleNote(true)); !strings.Contains(got, "throttling") {
		t.Errorf("throttleNote(true) = %q, want it to mention throttling", got)
	}
}

func TestFullestDiskPicksTheHighestUsagePercent(t *testing.T) {
	if _, ok := fullestDisk(nil); ok {
		t.Error("fullestDisk(nil) should report ok=false")
	}
	d, ok := fullestDisk([]Disk{{Mount: "/", UsedPct: 30}, {Mount: "/data", UsedPct: 90}, {Mount: "/tmp", UsedPct: 50}})
	if !ok || d.Mount != "/data" {
		t.Errorf("fullestDisk = %+v, want /data (90%%)", d)
	}
}

func TestSummaryLineIncludesDiskNetAndBatteryWhenPresent(t *testing.T) {
	s := Snapshot{
		CPU:    CPU{UsedPct: 10},
		Memory: Memory{UsedPct: 20},
		Disks:  []Disk{{Mount: "/", UsedPct: 30}, {Mount: "/data", UsedPct: 90}},
		Net:    []NetIface{{Name: "en0", RxBytesPerSec: 1024, TxBytesPerSec: 512}},
		Power:  Power{OnBattery: true, Percent: 55},
	}
	got := ui.StripANSI(SummaryLine(s))
	for _, want := range []string{"cpu 10%", "mem 20%", "disk 90% (/data)", "net", "battery 55%"} {
		if !strings.Contains(got, want) {
			t.Errorf("SummaryLine missing %q, got %q", want, got)
		}
	}
}

func TestSummaryLineOmitsBatteryWhenOnACPower(t *testing.T) {
	s := Snapshot{CPU: CPU{UsedPct: 10}, Memory: Memory{UsedPct: 20}}
	got := SummaryLine(s)
	if strings.Contains(got, "battery") {
		t.Errorf("SummaryLine on AC power should not mention battery, got %q", got)
	}
}

func TestSummaryLineShowsNetEvenAtZeroTraffic(t *testing.T) {
	// The whole point: an idle network interface must still show up here,
	// so a healthy/idle network reads as "checked, nothing to report" —
	// not indistinguishable from "never looked at" the way an omitted
	// resource would be. See advice.Run's own use of SummaryLine for the
	// exact complaint this closes.
	s := Snapshot{Net: []NetIface{{Name: "en0", RxBytesPerSec: 0, TxBytesPerSec: 0}}}
	got := ui.StripANSI(SummaryLine(s))
	if !strings.Contains(got, "net") {
		t.Errorf("SummaryLine should show net even at zero traffic, got %q", got)
	}
}

func TestSummaryLineOmitsNetWhenNoInterfacesCollected(t *testing.T) {
	s := Snapshot{CPU: CPU{UsedPct: 10}}
	got := SummaryLine(s)
	if strings.Contains(got, "net") {
		t.Errorf("SummaryLine should not claim to have checked net with zero interfaces collected, got %q", got)
	}
}

// finishAssess is the non-live half of Assess (everything after the real
// Collect() call), so it's testable against a fixture Snapshot without
// touching the live system — consistent with the rest of this package's
// "Analyze is pure, exercised from fixtures" testing style.
func TestFinishAssessReturnsTheAnalyzedReport(t *testing.T) {
	snap := Snapshot{CPU: CPU{Cores: 8, UsedPct: 99}, Memory: Memory{UsedPct: 20}}
	gotSnap, gotReport := finishAssess(snap)

	if !reflect.DeepEqual(gotSnap, snap) {
		t.Errorf("finishAssess should pass the snapshot through unchanged")
	}
	want := Analyze(snap)
	if len(gotReport.Findings) != len(want.Findings) {
		t.Errorf("finishAssess report = %+v, want Analyze(snap) = %+v", gotReport, want)
	}
}

// isolateConfigDir points os.UserConfigDir() at a fresh, empty temp
// directory on every OS a test might run on — HOME for macOS/Linux,
// APPDATA for Windows — so history/config file state never leaks between
// tests or into a shared CI runner's real config directory.
func isolateConfigDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
}

func TestFinishAssessAddsALeakFindingWhenHistoryShowsASteadyClimber(t *testing.T) {
	isolateConfigDir(t)

	base := time.Now().Add(-time.Hour)
	for i, rssMB := range []uint64{100, 250, 400, 600, 900} {
		p := mkPoint(base.Add(time.Duration(i)*10*time.Minute), 777, "creeper", rssMB)
		path, _ := historyPath()
		if err := appendHistory(path, p); err != nil {
			t.Fatal(err)
		}
	}

	// The current snapshot is otherwise healthy — only the history shows the problem.
	_, report := finishAssess(Snapshot{CPU: CPU{Cores: 8, UsedPct: 10}, Memory: Memory{UsedPct: 20}})

	f := find(report, "creeper")
	if f.Title == "" {
		t.Fatalf("expected a finding naming the steadily-climbing process, got %+v", report.Findings)
	}
	if f.Severity != diag.Warn {
		t.Errorf("a sustained memory climb should warn, got %v", f.Severity)
	}
	// The placeholder "No bottleneck detected" OK finding must not linger
	// alongside a real one.
	for _, other := range report.Findings {
		if other.Severity == diag.OK {
			t.Errorf("stale OK placeholder finding left in the report: %+v", other)
		}
	}
}

func TestFinishAssessLeavesHealthyReportAloneWithNoClimbInHistory(t *testing.T) {
	isolateConfigDir(t)

	_, report := finishAssess(Snapshot{CPU: CPU{Cores: 8, UsedPct: 10}, Memory: Memory{UsedPct: 20}})
	if report.Worst() != diag.OK {
		t.Errorf("no history and a healthy snapshot should stay healthy, got %v: %+v", report.Worst(), report.Findings)
	}
}

func TestFinishAssessRecordsHistory(t *testing.T) {
	isolateConfigDir(t)

	if before := LoadHistory(); len(before) != 0 {
		t.Fatalf("expected no history before recording, got %+v", before)
	}

	snap := Snapshot{CPU: CPU{UsedPct: 55}, Memory: Memory{UsedPct: 61}}
	finishAssess(snap)

	got := LoadHistory()
	if len(got) != 1 {
		t.Fatalf("expected one recorded point after finishAssess, got %d: %+v", len(got), got)
	}
	if got[0].CPUPercent != 55 || got[0].MemPercent != 61 {
		t.Errorf("recorded point = %+v, want cpu 55 / mem 61", got[0])
	}
}

func TestScaffoldConfigIfMissingWritesAFileWithNoExistingOne(t *testing.T) {
	isolateConfigDir(t)
	path, _ := config.Path()

	out := captureStdout(t, scaffoldConfigIfMissing)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("scaffoldConfigIfMissing should have written a config file, got: %v", err)
	}
	if !strings.Contains(out, "wrote default config") {
		t.Errorf("scaffoldConfigIfMissing should print a one-time notice, got: %q", out)
	}
}

func TestScaffoldConfigIfMissingIsSilentTheSecondTime(t *testing.T) {
	isolateConfigDir(t)
	scaffoldConfigIfMissing() // first call: writes it

	out := captureStdout(t, scaffoldConfigIfMissing) // second call: file already exists

	if out != "" {
		t.Errorf("scaffoldConfigIfMissing should print nothing once the config file already exists, got: %q", out)
	}
}

// captureStdout redirects os.Stdout for the duration of f and returns what
// it wrote — reads on a goroutine so a large write can't deadlock against
// an unread pipe buffer, matching the pattern used across this codebase
// (internal/dupes, internal/llm).
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
