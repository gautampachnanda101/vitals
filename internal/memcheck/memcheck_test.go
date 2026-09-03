package memcheck

import (
	"io"
	"os"
	"strings"
	"testing"

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
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w
	f()
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ := io.ReadAll(r)
	return string(out)
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
