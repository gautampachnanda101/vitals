package doctor

import (
	"errors"
	"strings"
	"testing"

	"vitals/internal/monitor"
	"vitals/internal/ui"
)

func TestPrintDiskIOProcsRanksAndCaps(t *testing.T) {
	orig := diskIOSampler
	defer func() { diskIOSampler = orig }()
	diskIOSampler = func(int) ([]monitor.ProcInfo, error) {
		return []monitor.ProcInfo{
			{PID: 1, Name: "light", DiskReadBytesPerSec: 1024},
			{PID: 2, Name: "heavy", DiskReadBytesPerSec: 5 << 20, DiskWriteBytesPerSec: 2 << 20},
			{PID: 3, Name: "idle"},
			{PID: 4, Name: "mid", DiskWriteBytesPerSec: 100 << 10},
		}, nil
	}

	out := ui.StripANSI(captureStdout(t, func() { printDiskIOProcs(false) }))

	if !strings.Contains(out, "top processes by disk I/O") {
		t.Fatalf("missing section header:\n%s", out)
	}
	hi := strings.Index(out, "heavy")
	mid := strings.Index(out, "mid")
	light := strings.Index(out, "light")
	if hi < 0 || mid < 0 || light < 0 || !(hi < mid && mid < light) {
		t.Errorf("processes not ordered by read+write rate (heavy<mid<light): heavy=%d mid=%d light=%d\n%s", hi, mid, light, out)
	}
	if strings.Contains(out, "idle") {
		t.Errorf("a process with a zero I/O rate must not appear:\n%s", out)
	}
}

func TestPrintDiskIOProcsSilentWhenNoRealRates(t *testing.T) {
	orig := diskIOSampler
	defer func() { diskIOSampler = orig }()

	t.Run("all zero (the macOS case)", func(t *testing.T) {
		diskIOSampler = func(int) ([]monitor.ProcInfo, error) {
			return []monitor.ProcInfo{{PID: 1, Name: "a"}, {PID: 2, Name: "b"}}, nil
		}
		if out := captureStdout(t, func() { printDiskIOProcs(false) }); strings.TrimSpace(out) != "" {
			t.Errorf("want no output when every rate is zero, got:\n%s", out)
		}
	})

	t.Run("sampler error", func(t *testing.T) {
		diskIOSampler = func(int) ([]monitor.ProcInfo, error) { return nil, errors.New("boom") }
		if out := captureStdout(t, func() { printDiskIOProcs(true) }); strings.TrimSpace(out) != "" {
			t.Errorf("want no output when the sampler fails, got:\n%s", out)
		}
	})
}
