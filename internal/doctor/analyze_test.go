package doctor

import (
	"strings"
	"testing"

	"vitals/internal/config"
	"vitals/internal/diag"
)

// find returns the first finding whose title contains sub, or a zero Finding.
func find(r diag.Report, sub string) diag.Finding {
	for _, f := range r.Findings {
		if strings.Contains(strings.ToLower(f.Title), strings.ToLower(sub)) {
			return f
		}
	}
	return diag.Finding{}
}

func TestAnalyzeResourceDispatchesToTheRightAnalyzer(t *testing.T) {
	// AnalyzeResource is the switch `vitals cpu|mem|disk|net|power|gpu`
	// and the dashboard's resource pages both dispatch through — it had
	// no direct test in this package at all (only exercised indirectly
	// via internal/dashboard's own tests, which don't count toward this
	// package's coverage).
	snap := Snapshot{
		CPU:    CPU{UsedPct: 99, Load1: 20, Cores: 2},
		Memory: Memory{UsedPct: 95, SwapUsedPct: 91, SwapOutPerSec: 1},
		Disks:  []Disk{{Mount: "/", UsedPct: 99}},
		GPUs:   []GPU{{Name: "gpu0", VRAMUsed: 99, VRAMTotal: 100}},
		Net:    []NetIface{{Name: "en0", LinkSpeedbps: 1e9, RxBytesPerSec: 1e8, TxBytesPerSec: 1e8}},
		Power:  Power{OnBattery: true, Percent: 5},
	}
	cases := []struct {
		resource  string
		wantTitle string // a substring guaranteed to appear only if the right analyzer ran
	}{
		{"cpu", "CPU"},
		{"mem", "RAM"},
		{"memory", "RAM"},
		{"disk", "/"},
		{"gpu", "gpu0"},
		{"net", "en0"},
		{"power", "Battery"},
		{"battery", "Battery"},
	}
	for _, c := range cases {
		t.Run(c.resource, func(t *testing.T) {
			r := AnalyzeResource(snap, c.resource)
			if len(r.Findings) == 0 {
				t.Fatalf("AnalyzeResource(%q) found nothing, want at least one finding from this deliberately unhealthy snapshot", c.resource)
			}
			found := false
			for _, f := range r.Findings {
				if strings.Contains(f.Title, c.wantTitle) || strings.Contains(f.Detail, c.wantTitle) {
					found = true
				}
			}
			if !found {
				t.Errorf("AnalyzeResource(%q) = %+v, wanted a finding mentioning %q", c.resource, r.Findings, c.wantTitle)
			}
		})
	}
	if r := AnalyzeResource(snap, "not-a-real-resource"); len(r.Findings) != 0 {
		t.Errorf("AnalyzeResource(unknown) = %+v, want no findings", r.Findings)
	}
}

func TestProcSuffix(t *testing.T) {
	if got := procSuffix(ProcRef{}, true); got != "" {
		t.Errorf("procSuffix(empty) = %q, want empty", got)
	}
	if got := procSuffix(ProcRef{Name: "chrome", PID: 1, CPUPct: 50}, true); !strings.Contains(got, "chrome") || !strings.Contains(got, "CPU") {
		t.Errorf("procSuffix(byCPU) = %q", got)
	}
	if got := procSuffix(ProcRef{Name: "chrome", PID: 1, RSSBytes: 100 << 20}, false); !strings.Contains(got, "chrome") || !strings.Contains(got, "RSS") {
		t.Errorf("procSuffix(byRSS) = %q", got)
	}
}

func TestQuitFix(t *testing.T) {
	if got := quitFix(ProcRef{}); !strings.Contains(got, "memhogs") {
		t.Errorf("quitFix(empty) = %q, want the generic memhogs pointer", got)
	}
	if got := quitFix(ProcRef{Name: "chrome", PID: 42}); !strings.Contains(got, "chrome") || !strings.Contains(got, "42") {
		t.Errorf("quitFix(named) = %q, want it to name the process", got)
	}
}

func TestCoreSpread(t *testing.T) {
	if _, _, ok := coreSpread([]float64{50}); ok {
		t.Error("coreSpread with fewer than 2 cores should report ok=false")
	}
	second, hi, ok := coreSpread([]float64{10, 90, 50})
	if !ok || hi != 90 || second != 50 {
		t.Errorf("coreSpread([10 90 50]) = second=%v hi=%v ok=%v, want second=50 hi=90 ok=true", second, hi, ok)
	}
}

func TestTimeToFull(t *testing.T) {
	if got := timeToFull(600); !strings.Contains(got, "minutes") {
		t.Errorf("timeToFull(600s) = %q, want minutes", got)
	}
	if got := timeToFull(10 * 3600); !strings.Contains(got, "hours") {
		t.Errorf("timeToFull(10h) = %q, want hours", got)
	}
	if got := timeToFull(5 * 86400); !strings.Contains(got, "days") {
		t.Errorf("timeToFull(5d) = %q, want days", got)
	}
}

func TestNz(t *testing.T) {
	if got := nz(""); got != "(unnamed)" {
		t.Errorf("nz(\"\") = %q, want (unnamed)", got)
	}
	if got := nz("gpu0"); got != "gpu0" {
		t.Errorf("nz(\"gpu0\") = %q, want it unchanged", got)
	}
}

func TestAnalyzeHealthy(t *testing.T) {
	r := Analyze(Snapshot{
		CPU:    CPU{Cores: 8, UsedPct: 12, Load1: 1.0},
		Memory: Memory{UsedPct: 45, SwapUsedPct: 1, SwapTotal: 8 << 30},
		Disks:  []Disk{{Mount: "/", UsedPct: 40, FreeBytes: 200 << 30}},
	})
	if r.Worst() != diag.OK || r.ExitCode() != 0 {
		t.Fatalf("healthy snapshot: worst=%v exit=%d\n%+v", r.Worst(), r.ExitCode(), r.Findings)
	}
}

func TestAnalyzeIOWaitMasquerade(t *testing.T) {
	r := Analyze(Snapshot{
		CPU:   CPU{Cores: 8, UsedPct: 92, IOWaitPct: 38, Load1: 9},
		Disks: []Disk{{Mount: "/", UsedPct: 55, UtilPct: 99, AwaitMS: 45}},
	})
	f := find(r, "I/O wait")
	if f.Title == "" {
		t.Fatalf("expected an I/O-wait finding, got %+v", r.Findings)
	}
	if f.Severity == diag.OK {
		t.Errorf("I/O-wait masquerade should not be OK severity")
	}
	if len(f.Fixes) == 0 {
		t.Errorf("finding should carry a fix")
	}
}

func TestAnalyzeSwapThrash(t *testing.T) {
	r := Analyze(Snapshot{
		CPU:    CPU{Cores: 8, UsedPct: 30},
		Memory: Memory{UsedPct: 96, SwapUsedPct: 91, SwapTotal: 8 << 30, SwapOutPerSec: 40 << 20},
	})
	f := find(r, "thrash")
	if f.Title == "" || f.Severity != diag.Critical {
		t.Fatalf("expected a critical thrash finding, got %+v", r.Findings)
	}
	if r.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", r.ExitCode())
	}
}

func TestAnalyzeSwapHeavyButNotPaging(t *testing.T) {
	r := Analyze(Snapshot{
		CPU:    CPU{Cores: 8, UsedPct: 15},
		Memory: Memory{UsedPct: 60, SwapUsedPct: 88, SwapTotal: 8 << 30, SwapOutPerSec: 0},
	})
	f := find(r, "swap")
	if f.Title == "" {
		t.Fatalf("expected a swap finding, got %+v", r.Findings)
	}
	if f.Severity != diag.Warn {
		t.Errorf("high swap with no paging should warn, not %v", f.Severity)
	}
}

func TestAnalyzeReclaimablePressureIsNotCritical(t *testing.T) {
	// High RAM %, but zero swap-out: much of "used" may be reclaimable cache.
	r := Analyze(Snapshot{
		CPU:    CPU{Cores: 8, UsedPct: 20},
		Memory: Memory{UsedPct: 93, SwapUsedPct: 3, SwapTotal: 8 << 30, SwapOutPerSec: 0},
	})
	f := find(r, "RAM")
	if f.Title == "" {
		t.Fatalf("expected a RAM finding, got %+v", r.Findings)
	}
	if f.Severity == diag.Critical {
		t.Errorf("no swap pressure => RAM finding should be a warning, not critical")
	}
}

func TestAnalyzeThermalCap(t *testing.T) {
	r := Analyze(Snapshot{
		CPU:     CPU{Cores: 8, UsedPct: 99, FreqMHz: 2100, BaseMHz: 3200},
		Thermal: Thermal{CPUTempC: 98, Throttling: true},
	})
	f := find(r, "throttl")
	if f.Title == "" || f.Severity == diag.OK {
		t.Fatalf("expected a thermal-throttle finding, got %+v", r.Findings)
	}
}

func TestAnalyzeCPUOnlyInference(t *testing.T) {
	r := Analyze(Snapshot{
		CPU: CPU{Cores: 10, UsedPct: 85},
		LLM: []LLMModel{{Name: "qwen2.5:32b", OffloadPct: 0, HostCPUPct: 380}},
	})
	f := find(r, "CPU")
	if f.Title == "" {
		t.Fatalf("expected a CPU-only-inference finding, got %+v", r.Findings)
	}
	if !strings.Contains(strings.ToLower(f.Detail+f.Title), "qwen2.5:32b") {
		t.Errorf("finding should name the model: %+v", f)
	}
}

func TestAnalyzePartialOffload(t *testing.T) {
	r := Analyze(Snapshot{
		LLM: []LLMModel{{Name: "llama3:70b", OffloadPct: 62, HostCPUPct: 210}},
	})
	f := find(r, "offload")
	if f.Title == "" || f.Severity != diag.Warn {
		t.Fatalf("expected a warning partial-offload finding, got %+v", r.Findings)
	}
}

func TestAnalyzeDiskTimeToFull(t *testing.T) {
	r := Analyze(Snapshot{
		Disks: []Disk{{
			Mount: "/", UsedPct: 96,
			FreeBytes:         23 << 30,
			GrowthBytesPerSec: 800 << 20 / 3600.0, // ~800 MB/hr
		}},
	})
	f := find(r, "/")
	if f.Title == "" || f.Severity == diag.OK {
		t.Fatalf("expected a disk finding, got %+v", r.Findings)
	}
	if !strings.Contains(strings.ToLower(f.Detail), "hour") && !strings.Contains(strings.ToLower(f.Detail), "day") {
		t.Errorf("near-full growing disk should project a time-to-full: %q", f.Detail)
	}
}

func TestAnalyzeNetSaturation(t *testing.T) {
	r := Analyze(Snapshot{
		Net: []NetIface{{
			Name: "en0", RxBytesPerSec: 110e6, TxBytesPerSec: 5e6, LinkSpeedbps: 1e9,
		}},
	})
	f := find(r, "en0")
	if f.Title == "" || f.Severity != diag.Warn {
		t.Fatalf("expected a saturation warning, got %+v", r.Findings)
	}
}

func TestAnalyzeNetLoss(t *testing.T) {
	r := Analyze(Snapshot{Net: []NetIface{{Name: "wlan0", RetransPct: 9}}})
	if find(r, "losing packets").Title == "" {
		t.Errorf("expected a packet-loss finding, got %+v", r.Findings)
	}
}

func TestAnalyzePowerLowBattery(t *testing.T) {
	warn := Analyze(Snapshot{Power: Power{OnBattery: true, Percent: 15, MinutesLeft: 22}})
	if warn.Worst() != diag.Warn {
		t.Errorf("15%% battery should warn, got %v", warn.Worst())
	}
	crit := Analyze(Snapshot{Power: Power{OnBattery: true, Percent: 5}})
	if crit.Worst() != diag.Critical {
		t.Errorf("5%% battery should be critical, got %v", crit.Worst())
	}
}

func TestAnalyzePowerDrainingOnAC(t *testing.T) {
	r := Analyze(Snapshot{Power: Power{OnBattery: false, Percent: 60, ChargeRateW: -12}})
	if find(r, "plugged in").Title == "" {
		t.Errorf("expected an underpowered-charger finding, got %+v", r.Findings)
	}
}

func TestAnalyzePowerDegradedHealth(t *testing.T) {
	r := Analyze(Snapshot{Power: Power{Percent: 80, DesignCapacityF: 0.72}})
	if find(r, "health").Title == "" {
		t.Errorf("expected a battery-health finding, got %+v", r.Findings)
	}
}

func TestConfiguredThresholdsChangeVerdicts(t *testing.T) {
	defer SetThresholds(config.Default()) // don't leak into other tests

	snap := Snapshot{
		CPU:   CPU{Cores: 8, UsedPct: 30},
		Disks: []Disk{{Mount: "/data", UsedPct: 93, FreeBytes: 50 << 30}},
	}
	if f := find(Analyze(snap), "nearly full"); f.Title == "" {
		t.Fatalf("93%% full should trigger the default 90%% threshold, got %+v", Analyze(snap).Findings)
	}

	// A media server or NAS legitimately runs near-full; raise the bar.
	SetThresholds(config.Config{DiskWarnPercent: 95, DiskCriticalPercent: 99})
	if f := find(Analyze(snap), "nearly full"); f.Title != "" {
		t.Errorf("93%% full should not fire once the configured warn threshold is 95%%, got %+v", f)
	}
}

func TestAnalyzeSortsCriticalFirst(t *testing.T) {
	r := Analyze(Snapshot{
		CPU:    CPU{Cores: 8, UsedPct: 78}, // warn-ish
		Memory: Memory{UsedPct: 97, SwapUsedPct: 92, SwapTotal: 8 << 30, SwapOutPerSec: 50 << 20},
	})
	sorted := r.SortedBySeverity()
	if len(sorted) < 2 {
		t.Skip("need at least two findings to check ordering")
	}
	if sorted[0].Severity != diag.Critical {
		t.Errorf("most severe finding should sort first, got %v", sorted[0].Severity)
	}
}
