package gpu

import "testing"

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

func TestMemUsedPctZeroTotal(t *testing.T) {
	if (Device{}).MemUsedPct() != 0 {
		t.Error("MemUsedPct with zero total should be 0, not NaN")
	}
}
