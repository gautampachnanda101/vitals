package gpu

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout swaps os.Stdout for the duration of f and returns
// everything written to it. Drained concurrently, not after f returns, to
// avoid a pipe-buffer deadlock on Windows — same pattern as
// internal/monitor's, internal/dupes', and internal/memhogs' identical
// helpers.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	f()
	w.Close()
	os.Stdout = old
	return <-done
}

func TestPrintReportNoDevicesSuggestsNvtop(t *testing.T) {
	out := captureStdout(t, func() { printReport(nil) })
	if !strings.Contains(out, "no GPU tooling found") || !strings.Contains(out, "nvtop") {
		t.Errorf("printReport(nil) should say no tooling was found and suggest nvtop, got:\n%s", out)
	}
}

// TestPrintReportNvidiaShapedDeviceShowsEveryField is the "this must keep
// working on a real NVIDIA/AMD machine, not just Apple" case: a device
// with every field populated (the shape parseNvidiaSMI/parseRocmSMIJSON
// actually produce) must show VRAM, Utilisation AND Temp together, Power,
// and Clock — none of the new Apple-driven gating should suppress a real
// non-Apple reading.
func TestPrintReportNvidiaShapedDeviceShowsEveryField(t *testing.T) {
	d := Device{
		Vendor: NVIDIA, Index: 0, Name: "RTX 4090",
		MemUsedB: 18320 << 20, MemTotalB: 24564 << 20,
		UtilPct: 42, TempC: 61,
		PowerW: 180.5, PowerLimitW: 450,
		ClockMHz: 2520, ClockMaxMHz: 2520,
		Processes: []Proc{{PID: 123, Name: "python3", MemUseB: 4 << 30}},
	}
	out := captureStdout(t, func() { printReport([]Device{d}) })
	for _, want := range []string{
		"RTX 4090", "(nvidia)",
		"VRAM", "GB", // MemUsedB/MemTotalB present
		"Utilisation", "42%",
		"Temp", "61", // real temp shown alongside real utilisation
		"180", "450 W", // Power
		"2520 MHz", // Clock
		"python3",  // per-process VRAM
	} {
		if !strings.Contains(out, want) {
			t.Errorf("printReport(nvidia-shaped device) missing %q, got:\n%s", want, out)
		}
	}
}

// TestPrintReportAppleShapedDeviceOmitsFieldsWithNoRealReading is the
// regression test for the actual bug report: an Apple Silicon device has
// a real UtilPct and MemUsed/MemTotal (from ioreg's PerformanceStatistics,
// see gpu.go's parseIORegApple) but no PowerW/PowerLimitW/ClockMHz/TempC
// reading at all — those must never appear as a bare, meaningless "0".
func TestPrintReportAppleShapedDeviceOmitsFieldsWithNoRealReading(t *testing.T) {
	d := Device{
		Vendor: Apple, Index: 0, Name: "Apple M4",
		MemUsedB: 676 << 20, MemTotalB: 5910 << 20,
		UtilPct: 9,
		// TempC, PowerW, PowerLimitW, ClockMHz, ClockMaxMHz all zero — no
		// real reading exists for any of them on this hardware.
	}
	out := captureStdout(t, func() { printReport([]Device{d}) })
	for _, want := range []string{"Apple M4", "VRAM", "Utilisation", "9%"} {
		if !strings.Contains(out, want) {
			t.Errorf("printReport(apple-shaped device) missing %q, got:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Temp", "Power", "Clock", "0°C", "0 W", "0 MHz"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("printReport(apple-shaped device) should omit %q (no real reading), got:\n%s", unwanted, out)
		}
	}
}

// TestPrintReportZeroUtilZeroTempOmitsTheWholeLine covers a device that
// reports absolutely nothing for either field (no telemetry tool
// available at all) — the line itself must not appear, not appear with a
// bare "0%".
func TestPrintReportZeroUtilZeroTempOmitsTheWholeLine(t *testing.T) {
	d := Device{Vendor: AMD, Index: 0, Name: "Some AMD GPU"}
	out := captureStdout(t, func() { printReport([]Device{d}) })
	if strings.Contains(out, "Utilisation") {
		t.Errorf("printReport should omit the Utilisation line entirely when neither UtilPct nor TempC is real, got:\n%s", out)
	}
}
