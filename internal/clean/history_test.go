package clean

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAndLoadCleanHistoryRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean_history.jsonl")
	r1 := RunRecord{Time: time.Now().Add(-time.Hour).UTC().Truncate(time.Second), TotalBytes: 100,
		Purges: []PurgeRecord{{Dir: "/a", Bytes: 100, Entries: 3}}}
	r2 := RunRecord{Time: time.Now().UTC().Truncate(time.Second), TotalBytes: 200,
		Purges: []PurgeRecord{{Dir: "/b", Bytes: 200, Entries: 1}}}

	if err := appendCleanHistoryTo(path, r1); err != nil {
		t.Fatal(err)
	}
	if err := appendCleanHistoryTo(path, r2); err != nil {
		t.Fatal(err)
	}

	got := loadCleanHistoryFrom(path)
	if len(got) != 2 {
		t.Fatalf("loadCleanHistoryFrom = %d records, want 2: %+v", len(got), got)
	}
	if got[0].TotalBytes != 100 || got[1].TotalBytes != 200 {
		t.Errorf("records out of order or wrong: %+v", got)
	}
	if len(got[1].Purges) != 1 || got[1].Purges[0].Dir != "/b" {
		t.Errorf("purge detail lost on round trip: %+v", got[1])
	}
}

func TestAppendCleanHistoryCapsAtMaxRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean_history.jsonl")
	for i := 0; i < cleanHistoryMaxRuns+10; i++ {
		if err := appendCleanHistoryTo(path, RunRecord{Time: time.Now(), TotalBytes: int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	got := loadCleanHistoryFrom(path)
	if len(got) != cleanHistoryMaxRuns {
		t.Fatalf("history has %d records, want capped at %d", len(got), cleanHistoryMaxRuns)
	}
	// The most recent runs should survive, not the oldest.
	if got[len(got)-1].TotalBytes != int64(cleanHistoryMaxRuns+9) {
		t.Errorf("cap should keep the most recent runs, last kept TotalBytes = %d", got[len(got)-1].TotalBytes)
	}
}

func TestLoadCleanHistoryFromMissingFileReturnsEmpty(t *testing.T) {
	got := loadCleanHistoryFrom(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if len(got) != 0 {
		t.Errorf("expected empty history for a missing file, got %+v", got)
	}
}

func TestLoadCleanHistoryFromSkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean_history.jsonl")
	content := `{"time":"2026-01-01T00:00:00Z","total_bytes":10}
not json
{"time":"2026-01-02T00:00:00Z","total_bytes":20}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadCleanHistoryFrom(path)
	if len(got) != 2 {
		t.Fatalf("expected the malformed line to be skipped, got %d records", len(got))
	}
}

func TestRenderCleanHistoryListsRunsNewestFirst(t *testing.T) {
	records := []RunRecord{
		{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), TotalBytes: 100},
		{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), TotalBytes: 200},
	}
	out := renderCleanHistory(records)
	iJan1 := strings.Index(out, "2026-01-01")
	iJan2 := strings.Index(out, "2026-01-02")
	if iJan1 == -1 || iJan2 == -1 {
		t.Fatalf("output missing a date:\n%s", out)
	}
	if iJan2 > iJan1 {
		t.Errorf("expected the newer run (2026-01-02) listed first, got:\n%s", out)
	}
}

func TestRenderCleanHistoryEmptyIsFriendly(t *testing.T) {
	out := renderCleanHistory(nil)
	if !strings.Contains(out, "no") {
		t.Errorf("expected a friendly message for an empty history, got %q", out)
	}
}
