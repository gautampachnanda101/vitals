package monitor

import (
	"strings"
	"testing"
	"time"

	"vitals/internal/ui"
)

func TestBar(t *testing.T) {
	cases := []struct {
		pct      float64
		wantPct  string
		wantFill int // number of filled cells expected
	}{
		{0, "0.0%", 0},
		{50, "50.0%", 10},
		{100, "100.0%", 20},
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
