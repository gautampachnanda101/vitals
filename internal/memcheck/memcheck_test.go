package memcheck

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"vitals/internal/diag"
	"vitals/internal/ui"
)

// captureStdout swaps both os.Stdout and os.Stderr for the duration of f and
// returns everything written to either — printIf/verdict print directly
// rather than building a string first (matching the rest of this codebase's
// terminal-output convention), and ui.Errf specifically writes critical
// findings to stderr, so a capture of stdout alone would silently miss
// every critical-severity line a user would still see in their terminal.
// Drained concurrently, not after f returns — a synchronous write bigger
// than Windows' small default pipe buffer deadlocks otherwise.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w

	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()

	f()
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-done
}

func TestPrintIfSkipsZeroAndPrintsNonzero(t *testing.T) {
	out := captureStdout(t, func() { printIf("Wired", 0) })
	if out != "" {
		t.Errorf("printIf with a zero value should print nothing, got %q", out)
	}

	out = captureStdout(t, func() { printIf("Wired", 4<<30) })
	if !strings.Contains(out, "Wired") || !strings.Contains(out, "GB") {
		t.Errorf("printIf with a nonzero value = %q, want the label and a human-readable size", out)
	}
}

func TestVerdictPrintsEachSeverityLine(t *testing.T) {
	cases := []struct {
		name       string
		vm         *mem.VirtualMemoryStat
		sw         *mem.SwapMemoryStat
		wantAll    []string // substrings that must appear
		wantAbsent string   // a substring that must NOT appear
	}{
		{
			name:       "healthy prints no fixes",
			vm:         &mem.VirtualMemoryStat{UsedPercent: 40},
			sw:         &mem.SwapMemoryStat{Total: 8 << 30, UsedPercent: 2},
			wantAll:    []string{"RAM healthy", "Swap healthy"},
			wantAbsent: "→",
		},
		{
			name:    "critical prints its fixes",
			vm:      &mem.VirtualMemoryStat{UsedPercent: 95},
			sw:      &mem.SwapMemoryStat{Total: 8 << 30, UsedPercent: 91},
			wantAll: []string{"RAM near capacity", "Swap nearly exhausted", "vitals memhogs", "sudo purge"},
		},
		{
			// The healthy and critical cases above never exercise
			// verdict's diag.Warn branch (ui.Warnf) — only OK and
			// Critical severities were hit.
			name:    "warning prints its fixes too",
			vm:      &mem.VirtualMemoryStat{UsedPercent: 80},
			sw:      &mem.SwapMemoryStat{Total: 8 << 30, UsedPercent: 20},
			wantAll: []string{"RAM elevated", "Swap in use", "close idle apps"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := ui.StripANSI(captureStdout(t, func() { verdict(c.vm, c.sw) }))
			for _, want := range c.wantAll {
				if !strings.Contains(out, want) {
					t.Errorf("verdict output missing %q:\n%s", want, out)
				}
			}
			if c.wantAbsent != "" && strings.Contains(out, c.wantAbsent) {
				t.Errorf("verdict output should not contain %q:\n%s", c.wantAbsent, out)
			}
		})
	}
}

func TestMemVerdict(t *testing.T) {
	t.Run("critical RAM and swap", func(t *testing.T) {
		r := memVerdict(
			&mem.VirtualMemoryStat{UsedPercent: 94},
			&mem.SwapMemoryStat{Total: 8 << 30, UsedPercent: 91},
		)
		if r.Worst() != diag.Critical || r.ExitCode() != 2 {
			t.Errorf("worst=%v exit=%d", r.Worst(), r.ExitCode())
		}
		if len(r.Findings) != 2 {
			t.Fatalf("want RAM + swap findings, got %d", len(r.Findings))
		}
	})

	t.Run("elevated is a warning", func(t *testing.T) {
		r := memVerdict(
			&mem.VirtualMemoryStat{UsedPercent: 80},
			&mem.SwapMemoryStat{Total: 8 << 30, UsedPercent: 20},
		)
		if r.Worst() != diag.Warn {
			t.Errorf("worst=%v, want warning", r.Worst())
		}
	})

	t.Run("healthy", func(t *testing.T) {
		r := memVerdict(
			&mem.VirtualMemoryStat{UsedPercent: 40},
			&mem.SwapMemoryStat{Total: 8 << 30, UsedPercent: 2},
		)
		if r.Worst() != diag.OK || r.ExitCode() != 0 {
			t.Errorf("worst=%v exit=%d", r.Worst(), r.ExitCode())
		}
	})

	t.Run("no swap device yields only the RAM finding", func(t *testing.T) {
		r := memVerdict(&mem.VirtualMemoryStat{UsedPercent: 40}, &mem.SwapMemoryStat{Total: 0})
		if len(r.Findings) != 1 {
			t.Errorf("want 1 finding, got %d", len(r.Findings))
		}
	})

	t.Run("nil swap is tolerated", func(t *testing.T) {
		r := memVerdict(&mem.VirtualMemoryStat{UsedPercent: 40}, nil)
		if len(r.Findings) != 1 {
			t.Errorf("want 1 finding, got %d", len(r.Findings))
		}
	})

	t.Run("every non-OK finding carries a fix", func(t *testing.T) {
		r := memVerdict(
			&mem.VirtualMemoryStat{UsedPercent: 94},
			&mem.SwapMemoryStat{Total: 8 << 30, UsedPercent: 91},
		)
		for _, f := range r.Findings {
			if f.Severity != diag.OK && len(f.Fixes) == 0 {
				t.Errorf("finding %q has severity %v but no fix", f.Title, f.Severity)
			}
		}
	})
}

// fakeSource builds a source with sane non-nil defaults for every field so
// individual tests only need to override the one gopsutil call they care
// about.
func fakeSource(overrides func(*source)) source {
	src := source{
		hostInfo: func() (*host.InfoStat, error) {
			return &host.InfoStat{Hostname: "test-host", Platform: "testos", PlatformVersion: "1.0", KernelArch: "x86_64", Uptime: 3600}, nil
		},
		virtualMemory: func() (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Total: 16 << 30, Used: 8 << 30, UsedPercent: 50, Available: 8 << 30, Free: 4 << 30}, nil
		},
		swapMemory: func() (*mem.SwapMemoryStat, error) {
			return &mem.SwapMemoryStat{Total: 4 << 30, Used: 1 << 30, UsedPercent: 25}, nil
		},
		swapDevices: func() ([]*mem.SwapDevice, error) {
			return nil, nil
		},
	}
	if overrides != nil {
		overrides(&src)
	}
	return src
}

func TestRunReturnsErrorWhenVirtualMemoryFails(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.virtualMemory = func() (*mem.VirtualMemoryStat, error) {
			return nil, errors.New("permission denied")
		}
	})

	var err error
	out := captureStdout(t, func() { err = run(src) })

	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("run() error = %v, want it to wrap the virtualMemory error", err)
	}
	if !strings.Contains(out, "MEMORY & PRESSURE OVERVIEW") {
		t.Errorf("run() should still print the header before failing, got %q", out)
	}
}

func TestRunToleratesHostInfoFailure(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.hostInfo = func() (*host.InfoStat, error) {
			return nil, errors.New("no host info")
		}
	})

	out := captureStdout(t, func() {
		if err := run(src); err != nil {
			t.Errorf("run() = %v, want nil", err)
		}
	})
	if strings.Contains(out, "Host      :") {
		t.Errorf("run() should skip the Host line when hostInfo fails, got %q", out)
	}
}

func TestRunWarnsWhenSwapMemoryFails(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.swapMemory = func() (*mem.SwapMemoryStat, error) {
			return nil, errors.New("swap read failed")
		}
	})

	out := captureStdout(t, func() {
		if err := run(src); err != nil {
			t.Errorf("run() = %v, want nil (swap errors are non-fatal)", err)
		}
	})
	if !strings.Contains(out, "swap stats unavailable") {
		t.Errorf("run() should warn about the unavailable swap stats, got %q", out)
	}
}

func TestRunPrintsSwapCumulativeAndDeviceLines(t *testing.T) {
	src := fakeSource(func(s *source) {
		s.swapMemory = func() (*mem.SwapMemoryStat, error) {
			return &mem.SwapMemoryStat{Total: 4 << 30, Used: 1 << 30, UsedPercent: 25, Sin: 2 << 20, Sout: 1 << 20}, nil
		}
		s.swapDevices = func() ([]*mem.SwapDevice, error) {
			return []*mem.SwapDevice{{Name: "/dev/swap0", UsedBytes: 1 << 30, FreeBytes: 3 << 30}}, nil
		}
	})

	out := captureStdout(t, func() {
		if err := run(src); err != nil {
			t.Errorf("run() = %v, want nil", err)
		}
	})
	for _, want := range []string{"Cumulative swap-in", "Cumulative swap-out", "/dev/swap0"} {
		if !strings.Contains(out, want) {
			t.Errorf("run() output missing %q:\n%s", want, out)
		}
	}
}

func TestRunSkipsSwapDeviceLinesWhenNoneReported(t *testing.T) {
	src := fakeSource(nil) // default swapDevices returns nil, nil

	out := captureStdout(t, func() {
		if err := run(src); err != nil {
			t.Errorf("run() = %v, want nil", err)
		}
	})
	if strings.Contains(out, "device ") {
		t.Errorf("run() should print no device lines when swapDevices is empty, got %q", out)
	}
}

func TestRunWithDefaultSourceExercisesTheRealGopsutilWiring(t *testing.T) {
	// Run is a thin pass-through to run(defaultSource); this exercises the
	// real gopsutil calls end to end the same way a user invoking
	// `vitals memcheck` would. VirtualMemory succeeding is assumed true of
	// any machine this test suite runs on.
	out := captureStdout(t, func() {
		if err := Run(); err != nil {
			t.Errorf("Run() = %v, want nil on a real host", err)
		}
	})
	if !strings.Contains(out, "DIAGNOSTIC VERDICT") {
		t.Errorf("Run() output missing the verdict section:\n%s", out)
	}
}
