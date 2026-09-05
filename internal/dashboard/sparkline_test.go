package dashboard

import (
	"strings"
	"testing"

	"vitals/internal/doctor"
)

func TestSparklineNeedsAtLeastTwoPoints(t *testing.T) {
	if sparkline(nil, 0, 100, "ok") != "" {
		t.Error("nil series should render nothing")
	}
	if sparkline([]float64{42}, 0, 100, "ok") != "" {
		t.Error("a single point can't be a line")
	}
}

func TestSparklineDrawsAPolylineWithOnePointPerValue(t *testing.T) {
	out := string(sparkline([]float64{0, 50, 100}, 0, 100, "ok"))
	if !strings.Contains(out, `<polyline`) || !strings.Contains(out, `class="spark"`) {
		t.Fatalf("expected an svg polyline, got: %s", out)
	}
	// 3 values -> 3 "x,y" coordinate pairs
	if n := strings.Count(out, ","); n != 3 {
		t.Errorf("expected 3 coordinate pairs, got %d: %s", n, out)
	}
	// x spans 0..100; first x is 0, last is 100
	if !strings.Contains(out, "points=\"0.0,") || !strings.Contains(out, " 100.0,") {
		t.Errorf("x axis should span the full width: %s", out)
	}
	// y inverts: value 0 -> bottom (y=24), value 100 -> top (y=0)
	if !strings.Contains(out, "0.0,24.0") || !strings.Contains(out, "100.0,0.0") {
		t.Errorf("y axis should invert (higher value = higher on screen): %s", out)
	}
}

func TestSparklineClampsOutOfDomainValues(t *testing.T) {
	out := string(sparkline([]float64{-30, 250}, 0, 100, "ok"))
	// -30 clamps to 0 -> y=24; 250 clamps to 100 -> y=0
	if !strings.Contains(out, "0.0,24.0") || !strings.Contains(out, "100.0,0.0") {
		t.Errorf("values outside [dmin,dmax] should clamp, got: %s", out)
	}
}

func TestSparklineStrokeFollowsSeverity(t *testing.T) {
	for sev, want := range map[string]string{
		"":         "var(--accent)",
		"ok":       "var(--accent)",
		"warning":  "var(--warn)",
		"critical": "var(--crit)",
	} {
		if got := string(sparkline([]float64{1, 2}, 0, 100, sev)); !strings.Contains(got, "stroke=\""+want+"\"") {
			t.Errorf("severity %q -> want stroke %q, got: %s", sev, want, got)
		}
	}
}

func TestSparklineCapsThePointCount(t *testing.T) {
	vals := make([]float64, 500)
	for i := range vals {
		vals[i] = float64(i % 100)
	}
	out := string(sparkline(vals, 0, 100, "ok"))
	// capped at 60 points -> 60 coordinate pairs, not 500
	if n := strings.Count(out, ","); n != 60 {
		t.Errorf("expected the series capped to 60 points, got %d pairs", n)
	}
}

func TestSparklineFlatSeriesStaysFlat(t *testing.T) {
	// a fixed domain (not auto-scaled) means a steady 40% reads as steady,
	// not as a dramatic zig-zag.
	out := string(sparkline([]float64{40, 40, 40, 40}, 0, 100, "ok"))
	// every y should be the same: 24 - (40/100)*24 = 14.4
	if strings.Count(out, "14.4") != 4 {
		t.Errorf("a flat series should have constant y, got: %s", out)
	}
}

func TestDiskSeriesDropsTheUnmeasuredSentinel(t *testing.T) {
	hist := []doctor.HistoryPoint{
		{DiskPercent: 0}, // "no mount measured" sentinel
		{DiskPercent: 55},
		{DiskPercent: 0},
		{DiskPercent: 56},
	}
	got := diskSeries(hist)
	if len(got) != 2 || got[0] != 55 || got[1] != 56 {
		t.Errorf("diskSeries should drop zero points, got %v", got)
	}
}

func TestRenderOverviewDrawsSparklinesWhenHistoryExists(t *testing.T) {
	stubResourceExtras(t)
	hist := make([]doctor.HistoryPoint, 10)
	for i := range hist {
		hist[i] = doctor.HistoryPoint{CPUPercent: float64(10 + i), MemPercent: float64(30 + i), DiskPercent: float64(60 + i)}
	}
	out := renderOverview(PageContext{
		Snapshot: doctor.Snapshot{
			CPU:    doctor.CPU{UsedPct: 19},
			Memory: doctor.Memory{UsedPct: 39},
			Disks:  []doctor.Disk{{Mount: "/", UsedPct: 69, FreeBytes: 1 << 30}},
		},
		History: hist,
	})
	if n := strings.Count(out, `class="spark"`); n != 3 {
		t.Errorf("expected a sparkline on the CPU, Memory and Disk cards, got %d: %s", n, out)
	}
}

func TestRenderOverviewOmitsSparklinesWithNoHistory(t *testing.T) {
	stubResourceExtras(t)
	out := renderOverview(PageContext{Snapshot: doctor.Snapshot{
		CPU:    doctor.CPU{UsedPct: 4},
		Memory: doctor.Memory{UsedPct: 40},
		Disks:  []doctor.Disk{{Mount: "/", UsedPct: 50, FreeBytes: 1 << 30}},
	}})
	if strings.Contains(out, `class="spark"`) {
		t.Errorf("no history -> no sparkline, got: %s", out)
	}
}
