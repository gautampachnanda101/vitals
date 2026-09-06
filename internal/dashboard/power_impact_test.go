package dashboard

import (
	"strings"
	"testing"
	"time"

	"vitals/internal/doctor"
	"vitals/internal/monitor"
	"vitals/internal/power"
)

func withFakePowerImpactCache(t *testing.T, fn func() ([]power.Proc, bool)) {
	t.Helper()
	old := defaultPowerImpactCache
	defaultPowerImpactCache = &powerImpactCache{ttl: time.Hour, sample: fn}
	t.Cleanup(func() { defaultPowerImpactCache = old })
}

func TestPowerImpactSectionEmptyWhenNoRealReading(t *testing.T) {
	withFakePowerImpactCache(t, func() ([]power.Proc, bool) { return nil, false })
	if got := powerImpactSection(); got != "" {
		t.Errorf("want no section when the platform has no per-process power source, got: %s", got)
	}
}

func TestPowerImpactSectionRendersRealReading(t *testing.T) {
	withFakePowerImpactCache(t, func() ([]power.Proc, bool) {
		return []power.Proc{
			{PID: 101, Name: "Copilot", Power: 83.4},
			{PID: 55, Name: "WindowServer", Power: 12.0},
		}, true
	})
	out := powerImpactSection()
	for _, want := range []string{"Energy impact by process", "Copilot", "83.4", "101", "Real macOS power score"} {
		if !strings.Contains(out, want) {
			t.Errorf("powerImpactSection missing %q, got: %s", want, out)
		}
	}
	if strings.Contains(out, "CPU-based estimate") {
		t.Errorf("a real reading must not carry the estimate caption: %s", out)
	}
}

func TestPowerImpactSectionEscapesHostileNames(t *testing.T) {
	withFakePowerImpactCache(t, func() ([]power.Proc, bool) {
		return []power.Proc{{PID: 1, Name: "<b>x</b>", Power: 1}}, true
	})
	if strings.Contains(powerImpactSection(), "<b>x</b>") {
		t.Error("powerImpactSection did not escape a crafted process name")
	}
}

func TestRenderPowerPrefersRealReadingOverEstimate(t *testing.T) {
	stubResourceExtras(t) // real reading unavailable...
	withFakeProcessCache(t, func() (monitor.Snapshot, error) {
		return monitor.Snapshot{Processes: []monitor.ProcInfo{{PID: 9, Name: "hungry", CPUPct: 80}}}, nil
	})
	s := doctor.Snapshot{}
	s.Power.OnBattery = true
	s.Power.Percent = 55

	estimate := renderPower(s)
	if !strings.Contains(estimate, "CPU-based estimate") {
		t.Errorf("with no real reading the page should fall back to the CPU estimate: %s", estimate)
	}

	// ...now a real reading is available: it wins, estimate caption gone.
	withFakePowerImpactCache(t, func() ([]power.Proc, bool) {
		return []power.Proc{{PID: 101, Name: "Copilot", Power: 83.4}}, true
	})
	real := renderPower(s)
	if !strings.Contains(real, "Energy impact by process") || strings.Contains(real, "CPU-based estimate") {
		t.Errorf("a real reading should replace the estimate entirely: %s", real)
	}
}

func TestPowerImpactCacheDefaultIsWired(t *testing.T) {
	c := newPowerImpactCache()
	if c.ttl <= 0 || c.sample == nil {
		t.Fatal("newPowerImpactCache not fully wired")
	}
	// One real call through the default wiring — ok is platform-dependent
	// (true on macOS, false elsewhere); either way it must not panic and
	// must be internally consistent.
	procs, ok := c.Get()
	if ok && len(procs) == 0 {
		t.Error("cache reported ok but returned no processes")
	}
}
