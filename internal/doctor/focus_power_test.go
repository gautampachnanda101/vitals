package doctor

import (
	"strings"
	"testing"

	"vitals/internal/power"
	"vitals/internal/ui"
)

func TestPrintPowerProcsRendersRankedTableWhenAvailable(t *testing.T) {
	orig := powerSampler
	defer func() { powerSampler = orig }()
	var gotLimit int
	powerSampler = func(limit int) ([]power.Proc, bool) {
		gotLimit = limit
		return []power.Proc{
			{PID: 101, Name: "Copilot", Power: 83.4},
			{PID: 55, Name: "WindowServer", Power: 12.0},
		}, true
	}

	out := ui.StripANSI(captureStdout(t, func() { printPowerProcs(false) }))
	if !strings.Contains(out, "energy impact by process") {
		t.Fatalf("missing section header:\n%s", out)
	}
	for _, want := range []string{"Copilot", "83.4", "101", "WindowServer"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if gotLimit != 5 {
		t.Errorf("non-verbose limit = %d, want 5", gotLimit)
	}

	powerSampler = func(limit int) ([]power.Proc, bool) {
		gotLimit = limit
		return []power.Proc{{PID: 1, Name: "x", Power: 1}}, true
	}
	_ = captureStdout(t, func() { printPowerProcs(true) })
	if gotLimit != 15 {
		t.Errorf("verbose limit = %d, want 15", gotLimit)
	}
}

func TestPrintPowerProcsSilentWhenUnavailable(t *testing.T) {
	orig := powerSampler
	defer func() { powerSampler = orig }()

	t.Run("platform has no source", func(t *testing.T) {
		powerSampler = func(int) ([]power.Proc, bool) { return nil, false }
		if out := captureStdout(t, func() { printPowerProcs(false) }); strings.TrimSpace(out) != "" {
			t.Errorf("want no output when unavailable, got:\n%s", out)
		}
	})
	t.Run("ok but empty", func(t *testing.T) {
		powerSampler = func(int) ([]power.Proc, bool) { return nil, true }
		if out := captureStdout(t, func() { printPowerProcs(false) }); strings.TrimSpace(out) != "" {
			t.Errorf("want no output for an empty reading, got:\n%s", out)
		}
	})
}
