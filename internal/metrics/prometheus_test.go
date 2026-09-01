package metrics

import (
	"strings"
	"testing"

	"vitals/internal/diag"
	"vitals/internal/doctor"
)

func TestRenderShape(t *testing.T) {
	s := doctor.Snapshot{
		CPU:    doctor.CPU{Cores: 8, UsedPct: 50, IOWaitPct: 10, Load1: 2.0},
		Memory: doctor.Memory{UsedPct: 80, SwapUsedPct: 20, SwapOutPerSec: 1024},
		Disks:  []doctor.Disk{{Mount: "/", UsedPct: 63, FreeBytes: 100 << 30, UtilPct: 5}},
		Net:    []doctor.NetIface{{Name: "en0", RxBytesPerSec: 2048, TxBytesPerSec: 512}},
		GPUs:   []doctor.GPU{{Name: "RTX 4090", UtilPct: 40, VRAMUsed: 12 << 30, VRAMTotal: 24 << 30, TempC: 61}},
		LLM:    []doctor.LLMModel{{Name: "qwen2.5:32b", OffloadPct: 62}},
		Power:  doctor.Power{OnBattery: true, Percent: 55},
	}
	var r diag.Report
	r.Add(diag.Finding{Severity: diag.Warn, Title: "x"})
	r.Add(diag.Finding{Severity: diag.Critical, Title: "y"})

	out := Render(s, r)

	must := []string{
		"# HELP system_cpu_utilization",
		"# TYPE system_cpu_utilization gauge",
		"system_cpu_utilization 0.5",
		"system_memory_utilization 0.8",
		"system_paging_out_bytes_per_second 1024",
		`system_filesystem_utilization{mountpoint="/"} 0.63`,
		`system_network_io_bytes_per_second{device="en0",direction="receive"} 2048`,
		`system_network_io_bytes_per_second{device="en0",direction="transmit"} 512`,
		`vitals_gpu_memory_utilization_ratio{gpu="RTX 4090"} 0.5`,
		`vitals_llm_gpu_offload_ratio{model="qwen2.5:32b"} 0.62`,
		"vitals_battery_on_battery 1",
		"vitals_verdict 2",
		`vitals_findings{severity="critical"} 1`,
		`vitals_findings{severity="warning"} 1`,
	}
	for _, want := range must {
		if !strings.Contains(out, want) {
			t.Errorf("output missing line:\n  %s\n--- full ---\n%s", want, out)
		}
	}

	// HELP/TYPE must appear exactly once per metric name.
	if n := strings.Count(out, "# TYPE system_cpu_utilization "); n != 1 {
		t.Errorf("system_cpu_utilization TYPE appeared %d times", n)
	}
}

func TestRenderDeterministic(t *testing.T) {
	s := doctor.Snapshot{
		CPU:   doctor.CPU{UsedPct: 12},
		Disks: []doctor.Disk{{Mount: "/a", UsedPct: 10}, {Mount: "/b", UsedPct: 20}},
		Net:   []doctor.NetIface{{Name: "z"}, {Name: "a"}},
	}
	var r diag.Report
	first := Render(s, r)
	for i := 0; i < 20; i++ {
		if got := Render(s, r); got != first {
			t.Fatalf("Render not deterministic across runs:\n--- first ---\n%s\n--- run %d ---\n%s", first, i, got)
		}
	}
}

func TestRenderEscapesLabels(t *testing.T) {
	s := doctor.Snapshot{LLM: []doctor.LLMModel{{Name: `weird"model`, OffloadPct: 100}}}
	var r diag.Report
	out := Render(s, r)
	if !strings.Contains(out, `model="weird\"model"`) {
		t.Errorf("label not escaped:\n%s", out)
	}
}
