package memcheck

import (
	"testing"

	"github.com/shirou/gopsutil/v4/mem"

	"vitals/internal/diag"
)

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
