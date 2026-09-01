package doctor

import (
	"strings"
	"testing"

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
