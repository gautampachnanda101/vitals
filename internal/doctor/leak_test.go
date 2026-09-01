package doctor

import (
	"testing"
	"time"
)

func mkPoint(t time.Time, pid int32, name string, rssMB uint64) HistoryPoint {
	return HistoryPoint{Time: t, TopMemPID: pid, TopMemName: name, TopMemRSS: rssMB << 20}
}

func TestDetectMemoryGrowthFindsASteadyClimber(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	points := []HistoryPoint{
		mkPoint(base, 42, "leaky", 100),
		mkPoint(base.Add(10*time.Minute), 42, "leaky", 250),
		mkPoint(base.Add(20*time.Minute), 42, "leaky", 400),
		mkPoint(base.Add(30*time.Minute), 42, "leaky", 550),
	}
	proc, growth, ok := DetectMemoryGrowth(points, 3, 200<<20)
	if !ok {
		t.Fatalf("expected a steady 100MB->550MB climb to be detected")
	}
	if proc.PID != 42 || proc.Name != "leaky" {
		t.Errorf("proc = %+v, want pid 42 / leaky", proc)
	}
	if want := uint64(450) << 20; growth != want {
		t.Errorf("growth = %d, want %d (550MB - 100MB)", growth, want)
	}
}

func TestDetectMemoryGrowthIgnoresFluctuatingUsage(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	points := []HistoryPoint{
		mkPoint(base, 42, "bouncy", 500),
		mkPoint(base.Add(10*time.Minute), 42, "bouncy", 100), // big drop breaks the climb
		mkPoint(base.Add(20*time.Minute), 42, "bouncy", 600),
		mkPoint(base.Add(30*time.Minute), 42, "bouncy", 150),
	}
	if _, _, ok := DetectMemoryGrowth(points, 3, 200<<20); ok {
		t.Error("a process that repeatedly drops back down should not be flagged as leaking")
	}
}

func TestDetectMemoryGrowthRequiresMinSamples(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	points := []HistoryPoint{
		mkPoint(base, 42, "leaky", 100),
		mkPoint(base.Add(10*time.Minute), 42, "leaky", 900),
	}
	if _, _, ok := DetectMemoryGrowth(points, 3, 200<<20); ok {
		t.Error("two samples should not be enough evidence with minSamples=3")
	}
}

func TestDetectMemoryGrowthRequiresMinGrowth(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	points := []HistoryPoint{
		mkPoint(base, 42, "steady", 100),
		mkPoint(base.Add(10*time.Minute), 42, "steady", 110),
		mkPoint(base.Add(20*time.Minute), 42, "steady", 120),
	}
	if _, _, ok := DetectMemoryGrowth(points, 3, 200<<20); ok {
		t.Error("a 20MB drift should not clear a 200MB growth threshold")
	}
}

func TestDetectMemoryGrowthPicksTheLargestGrowthAmongCandidates(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	points := []HistoryPoint{
		mkPoint(base, 1, "small-climber", 100),
		mkPoint(base.Add(1*time.Minute), 2, "big-climber", 100),
		mkPoint(base.Add(10*time.Minute), 1, "small-climber", 300), // +200MB
		mkPoint(base.Add(11*time.Minute), 2, "big-climber", 900),   // +800MB
		mkPoint(base.Add(20*time.Minute), 1, "small-climber", 320),
		mkPoint(base.Add(21*time.Minute), 2, "big-climber", 950),
	}
	proc, _, ok := DetectMemoryGrowth(points, 3, 100<<20)
	if !ok || proc.Name != "big-climber" {
		t.Errorf("expected big-climber (larger growth) to win, got %+v ok=%v", proc, ok)
	}
}

func TestDetectMemoryGrowthWithNoHistoryIsFalse(t *testing.T) {
	if _, _, ok := DetectMemoryGrowth(nil, 3, 100<<20); ok {
		t.Error("no history should never be reported as a leak")
	}
}

func TestDetectMemoryGrowthSkipsUnattributedPoints(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	points := []HistoryPoint{
		mkPoint(base, 0, "", 999), // PID 0 == nothing attributed, must be ignored
		mkPoint(base.Add(10*time.Minute), 0, "", 1200),
		mkPoint(base.Add(20*time.Minute), 0, "", 1500),
	}
	if _, _, ok := DetectMemoryGrowth(points, 3, 100<<20); ok {
		t.Error("points with no attributed process (PID 0) should never be flagged")
	}
}
