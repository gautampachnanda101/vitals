package doctor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPointFromSnapshotCapturesTheAtAGlanceNumbers(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s := Snapshot{
		CPU:    CPU{UsedPct: 42},
		Memory: Memory{UsedPct: 77, TopProc: ProcRef{PID: 99, Name: "leaky", RSSBytes: 500 << 20}},
		Disks:  []Disk{{Mount: "/", UsedPct: 30}, {Mount: "/data", UsedPct: 88}},
	}
	p := pointFromSnapshot(s, now)

	if !p.Time.Equal(now) {
		t.Errorf("Time = %v, want %v", p.Time, now)
	}
	if p.CPUPercent != 42 {
		t.Errorf("CPUPercent = %v, want 42", p.CPUPercent)
	}
	if p.MemPercent != 77 {
		t.Errorf("MemPercent = %v, want 77", p.MemPercent)
	}
	if p.DiskPercent != 88 {
		t.Errorf("DiskPercent = %v, want 88 (fullest mount)", p.DiskPercent)
	}
	if p.TopMemPID != 99 || p.TopMemName != "leaky" || p.TopMemRSS != 500<<20 {
		t.Errorf("top-mem fields = %d/%q/%d, want 99/leaky/%d", p.TopMemPID, p.TopMemName, p.TopMemRSS, 500<<20)
	}
}

func TestPointFromSnapshotWithNoDisksLeavesDiskPercentZero(t *testing.T) {
	p := pointFromSnapshot(Snapshot{}, time.Now())
	if p.DiskPercent != 0 {
		t.Errorf("DiskPercent = %v, want 0 with no disks measured", p.DiskPercent)
	}
}

func TestPruneHistoryDropsPointsOlderThanMaxAge(t *testing.T) {
	now := time.Now()
	points := []HistoryPoint{
		{Time: now.Add(-48 * time.Hour)}, // too old
		{Time: now.Add(-1 * time.Hour)},  // kept
		{Time: now},                      // kept
	}
	got := pruneHistory(points, now, 24*time.Hour, 1000)
	if len(got) != 2 {
		t.Fatalf("pruneHistory kept %d points, want 2: %+v", len(got), got)
	}
	for _, p := range got {
		if now.Sub(p.Time) > 24*time.Hour {
			t.Errorf("pruneHistory kept a point older than maxAge: %v", p.Time)
		}
	}
}

func TestPruneHistoryCapsPointCountKeepingTheMostRecent(t *testing.T) {
	now := time.Now()
	var points []HistoryPoint
	for i := 0; i < 10; i++ {
		points = append(points, HistoryPoint{Time: now.Add(time.Duration(i) * time.Second)})
	}
	got := pruneHistory(points, now, 24*time.Hour, 3)
	if len(got) != 3 {
		t.Fatalf("pruneHistory kept %d points, want 3", len(got))
	}
	// The most recent 3 (indices 7, 8, 9) should survive, oldest-first order preserved.
	if !got[len(got)-1].Time.Equal(points[9].Time) {
		t.Errorf("pruneHistory should keep the most recent points, last kept = %v, want %v", got[len(got)-1].Time, points[9].Time)
	}
}

func TestAppendAndLoadHistoryRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	p1 := HistoryPoint{Time: time.Now().Add(-time.Minute).UTC().Truncate(time.Second), CPUPercent: 10}
	p2 := HistoryPoint{Time: time.Now().UTC().Truncate(time.Second), CPUPercent: 20}

	if err := appendHistory(path, p1); err != nil {
		t.Fatal(err)
	}
	if err := appendHistory(path, p2); err != nil {
		t.Fatal(err)
	}

	got := loadHistoryFrom(path)
	if len(got) != 2 {
		t.Fatalf("loadHistoryFrom = %d points, want 2: %+v", len(got), got)
	}
	if got[0].CPUPercent != 10 || got[1].CPUPercent != 20 {
		t.Errorf("history out of order or wrong values: %+v", got)
	}
}

func TestAppendHistoryPrunesOldEntriesOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	old := HistoryPoint{Time: time.Now().Add(-48 * time.Hour)}
	if err := appendHistory(path, old); err != nil {
		t.Fatal(err)
	}
	fresh := HistoryPoint{Time: time.Now()}
	if err := appendHistory(path, fresh); err != nil {
		t.Fatal(err)
	}

	got := loadHistoryFrom(path)
	if len(got) != 1 {
		t.Fatalf("expected the stale point to be pruned on the next append, got %+v", got)
	}
}

func TestLoadHistoryFromMissingFileReturnsEmpty(t *testing.T) {
	got := loadHistoryFrom(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if len(got) != 0 {
		t.Errorf("loadHistoryFrom(missing) = %+v, want empty", got)
	}
}

func TestLoadHistoryFromSkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	content := `{"time":"2026-01-01T00:00:00Z","cpu_percent":5}
not json at all
{"time":"2026-01-02T00:00:00Z","cpu_percent":6}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadHistoryFrom(path)
	if len(got) != 2 {
		t.Fatalf("loadHistoryFrom should skip the malformed line, got %d points: %+v", len(got), got)
	}
}
