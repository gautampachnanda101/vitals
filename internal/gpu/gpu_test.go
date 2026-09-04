package gpu

import (
	"reflect"
	"testing"
)

func TestParseNvidiaSMISkipsBlankAndMalformedLines(t *testing.T) {
	csv := "\n  \n0, RTX 4090, 24564, 18320, 42, 61, 180.5, 450.00, 2520, 2520\ntoo, short\n"
	devs := parseNvidiaSMI(csv)
	if len(devs) != 1 {
		t.Fatalf("want 1 device (blank and short lines skipped), got %d: %+v", len(devs), devs)
	}
}

func TestParseNvidiaAppsSkipsBlankAndMalformedLines(t *testing.T) {
	csv := "\n  \n1234, chrome, 512\ntoo,short\n"
	procs := parseNvidiaApps(csv)
	if len(procs) != 1 {
		t.Fatalf("want 1 process (blank and short lines skipped), got %d: %+v", len(procs), procs)
	}
}

func TestAttachNvidiaAppsNoOpOnEmptyInput(t *testing.T) {
	devs := []Device{{Name: "gpu0"}}
	attachNvidiaApps(devs, nil)
	if devs[0].Processes != nil {
		t.Errorf("attachNvidiaApps with no procs should leave Processes nil, got %v", devs[0].Processes)
	}
	attachNvidiaApps(nil, []Proc{{PID: 1}}) // must not panic on an empty device list
}

func TestAttachNvidiaAppsSingleDeviceGetsAllProcesses(t *testing.T) {
	devs := []Device{{Name: "gpu0"}}
	procs := []Proc{{PID: 1}, {PID: 2}}
	attachNvidiaApps(devs, procs)
	if !reflect.DeepEqual(devs[0].Processes, procs) {
		t.Errorf("single device should get every process, got %+v", devs[0].Processes)
	}
}

func TestAttachNvidiaAppsMultiDeviceGetsAllProcessesOnEach(t *testing.T) {
	devs := []Device{{Name: "gpu0"}, {Name: "gpu1"}}
	procs := []Proc{{PID: 1}}
	attachNvidiaApps(devs, procs)
	for i, d := range devs {
		if !reflect.DeepEqual(d.Processes, procs) {
			t.Errorf("device %d: Processes = %+v, want every process attached (best effort, no per-device index available)", i, d.Processes)
		}
	}
}

func TestAtoiOrFallsBackToDefaultOnUnparseable(t *testing.T) {
	if got := atoiOr("not-a-number", 42); got != 42 {
		t.Errorf("atoiOr(unparseable) = %d, want the default 42", got)
	}
	if got := atoiOr(" 7 ", 42); got != 7 {
		t.Errorf("atoiOr(\" 7 \") = %d, want 7 (trimmed and parsed)", got)
	}
}

func TestNumOrHandlesEveryDefaultCase(t *testing.T) {
	cases := []struct{ in string }{{""}, {"N/A"}, {"n/a"}, {"[N/A]"}, {"not-a-float"}}
	for _, c := range cases {
		if got := numOr(c.in, 99); got != 99 {
			t.Errorf("numOr(%q) = %v, want the default 99", c.in, got)
		}
	}
	if got := numOr("12.5", 0); got != 12.5 {
		t.Errorf("numOr(\"12.5\") = %v, want 12.5", got)
	}
}

func TestFirstNonEmptyAllEmptyReturnsEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", ""); got != "" {
		t.Errorf("firstNonEmpty(all empty) = %q, want empty", got)
	}
	if got := firstNonEmpty("", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty(\"\", \"x\", \"y\") = %q, want the first non-empty", got)
	}
}

func TestStrSortActuallySorts(t *testing.T) {
	s := []string{"card10", "card2", "card1", "card20"}
	strSort(s)
	want := []string{"card1", "card10", "card2", "card20"} // lexical, not numeric — matches the real implementation
	if !reflect.DeepEqual(s, want) {
		t.Errorf("strSort = %v, want %v", s, want)
	}
}

func TestStrSortEmptyAndSingleElement(t *testing.T) {
	empty := []string{}
	strSort(empty) // must not panic
	one := []string{"a"}
	strSort(one)
	if one[0] != "a" {
		t.Errorf("strSort([a]) mutated a single element: %v", one)
	}
}

const nvidiaCSV = `0, NVIDIA GeForce RTX 4090, 24564, 18320, 42, 61, 180.5, 450.00, 2520, 2520
1, NVIDIA GeForce RTX 3090, 24576, 1024, 3, 40, 90.0, 350.00, 1400, 1980`

func TestParseNvidiaSMI(t *testing.T) {
	devs := parseNvidiaSMI(nvidiaCSV)
	if len(devs) != 2 {
		t.Fatalf("want 2 devices, got %d", len(devs))
	}
	d := devs[0]
	if d.Vendor != NVIDIA || d.Name != "NVIDIA GeForce RTX 4090" {
		t.Errorf("device 0 = %+v", d)
	}
	if d.MemTotalB != 24564*mib || d.MemUsedB != 18320*mib {
		t.Errorf("memory not converted from MiB: %+v", d)
	}
	if d.UtilPct != 42 || d.TempC != 61 || d.PowerW != 180.5 || d.PowerLimitW != 450 {
		t.Errorf("telemetry fields = %+v", d)
	}
	if got := d.MemUsedPct(); got < 74 || got > 76 {
		t.Errorf("MemUsedPct = %.1f, want ~74.6", got)
	}
	if devs[1].ClockMHz != 1400 || devs[1].ClockMaxMHz != 1980 {
		t.Errorf("device 1 clocks = %+v", devs[1])
	}
}

func TestParseNvidiaSMIHandlesNA(t *testing.T) {
	devs := parseNvidiaSMI("0, NVIDIA T4, 15360, 512, [N/A], 55, [N/A], [N/A], 585, 1590")
	if len(devs) != 1 {
		t.Fatalf("got %d devices", len(devs))
	}
	if devs[0].UtilPct != 0 || devs[0].PowerW != 0 {
		t.Errorf("[N/A] fields should be zero: %+v", devs[0])
	}
	if devs[0].TempC != 55 {
		t.Errorf("valid field alongside [N/A] lost: %+v", devs[0])
	}
}

func TestParseNvidiaAppsAndAttach(t *testing.T) {
	procs := parseNvidiaApps("12345, python, 8192\n23456, ollama, 4096")
	if len(procs) != 2 || procs[0].PID != 12345 || procs[0].MemUseB != 8192*mib {
		t.Fatalf("procs = %+v", procs)
	}

	single := parseNvidiaSMI("0, GPU, 24000, 12000, 50, 60, 100, 300, 2000, 2000")
	attachNvidiaApps(single, procs)
	if len(single[0].Processes) != 2 {
		t.Errorf("single-GPU: processes not attached: %+v", single[0].Processes)
	}
}

func TestParseRocmSMIJSON(t *testing.T) {
	js := `{
		"card0": {
			"Card series": "Radeon RX 7900 XTX",
			"VRAM Total Memory (B)": "25753026560",
			"VRAM Total Used Memory (B)": "12000000000",
			"GPU use (%)": "88",
			"Temperature (Sensor junction) (C)": "72.0",
			"Current Socket Graphics Package Power (W)": "210.0"
		},
		"system": {"Driver version": "6.7.0"}
	}`
	devs := parseRocmSMIJSON(js)
	if len(devs) != 1 {
		t.Fatalf("want 1 card, got %d", len(devs))
	}
	d := devs[0]
	if d.Vendor != AMD || d.Name != "Radeon RX 7900 XTX" {
		t.Errorf("device = %+v", d)
	}
	if d.MemTotalB != 25753026560 || d.MemUsedB != 12000000000 {
		t.Errorf("memory = %+v", d)
	}
	if d.UtilPct != 88 || d.TempC != 72 || d.PowerW != 210 {
		t.Errorf("telemetry = %+v", d)
	}
}

func TestParseIORegApple(t *testing.T) {
	devs := parseIORegApple(`+-o AGXAcceleratorG14X  <class AGXAcceleratorG14X>
      "model" = <"Apple M3 Max">
      "IOAccelerator" = ...`)
	if len(devs) != 1 || devs[0].Vendor != Apple || devs[0].Name != "Apple M3 Max" {
		t.Errorf("apple device = %+v", devs)
	}
	if got := parseIORegApple("nothing here"); got != nil {
		t.Errorf("want nil for irrelevant output, got %+v", got)
	}
}

// TestParseIORegAppleReadsPerformanceStatistics uses the exact text `ioreg
// -r -d 1 -w 0 -c IOAccelerator` (Probe's own invocation) captured on a
// real M4 Mac — per AGENTS.md's "verify OS command output empirically"
// rule, not a guessed shape. PerformanceStatistics is the same dict
// asitop/mactop/stats.app read for Apple Silicon GPU utilization; there's
// no separate fixed VRAM budget on this hardware (unified memory), but
// "In use system memory"/"Alloc system memory" are real, live figures for
// how much of that shared pool the GPU driver currently has mapped —
// answering "what's used by the GPU vs. by everything else" without
// pretending there's a discrete VRAM chip.
func TestParseIORegAppleReadsPerformanceStatistics(t *testing.T) {
	const captured = `+-o AGXAcceleratorG16G  <class AGXAcceleratorG16G, id 0x1000003d5, registered, matched, active, busy 0 (262 ms), retain 67>
    {
      "model" = "Apple M4"
      "PerformanceStatistics" = {"In use system memory (driver)"=0,"Alloc system memory"=6383386624,"Tiler Utilization %"=10,"recoveryCount"=0,"lastRecoveryTime"=0,"Renderer Utilization %"=10,"TiledSceneBytes"=1048576,"Device Utilization %"=10,"SplitSceneCount"=0,"Allocated PB Size"=85196800,"In use system memory"=3153936384}
      "IOClass" = "AGXAcceleratorG16G"
    }`
	devs := parseIORegApple(captured)
	if len(devs) != 1 {
		t.Fatalf("expected exactly one device, got %+v", devs)
	}
	d := devs[0]
	if d.Name != "Apple M4" {
		t.Errorf("Name = %q, want Apple M4", d.Name)
	}
	if d.UtilPct != 10 {
		t.Errorf("UtilPct = %v, want 10 (from Device Utilization %%)", d.UtilPct)
	}
	if d.MemUsedB != 3153936384 {
		t.Errorf("MemUsedB = %d, want 3153936384 (from the second, un-parenthesised 'In use system memory')", d.MemUsedB)
	}
	if d.MemTotalB != 6383386624 {
		t.Errorf("MemTotalB = %d, want 6383386624 (from Alloc system memory)", d.MemTotalB)
	}
}

// TestParseIORegAppleWithoutPerformanceStatisticsStaysZero covers an
// AGXAccelerator entry (or driver version) that doesn't publish the dict
// at all — the name-only fallback this package always had must still
// work, not error or guess.
func TestParseIORegAppleWithoutPerformanceStatisticsStaysZero(t *testing.T) {
	devs := parseIORegApple(`+-o AGXAcceleratorG13G  <class AGXAcceleratorG13G>
      "model" = "Apple M1"`)
	if len(devs) != 1 {
		t.Fatalf("expected exactly one device, got %+v", devs)
	}
	if d := devs[0]; d.UtilPct != 0 || d.MemUsedB != 0 || d.MemTotalB != 0 {
		t.Errorf("with no PerformanceStatistics dict, telemetry should stay zero, got %+v", d)
	}
}

func TestMemUsedPctZeroTotal(t *testing.T) {
	if (Device{}).MemUsedPct() != 0 {
		t.Error("MemUsedPct with zero total should be 0, not NaN")
	}
}
