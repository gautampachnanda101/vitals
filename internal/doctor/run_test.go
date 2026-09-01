package doctor

import (
	"reflect"
	"testing"
	"time"

	"vitals/internal/diag"
)

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

func TestFinishAssessAddsALeakFindingWhenHistoryShowsASteadyClimber(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

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
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	_, report := finishAssess(Snapshot{CPU: CPU{Cores: 8, UsedPct: 10}, Memory: Memory{UsedPct: 20}})
	if report.Worst() != diag.OK {
		t.Errorf("no history and a healthy snapshot should stay healthy, got %v: %+v", report.Worst(), report.Findings)
	}
}

func TestFinishAssessRecordsHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

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
