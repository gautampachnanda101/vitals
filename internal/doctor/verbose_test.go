package doctor

import (
	"strings"
	"testing"

	"vitals/internal/clean"
)

func TestReclaimableLinesCapsAtLimit(t *testing.T) {
	entries := []clean.CacheEntry{
		{Path: "/a", Bytes: 500},
		{Path: "/b", Bytes: 400},
		{Path: "/c", Bytes: 300},
	}
	lines, shown, remaining := reclaimableLines(entries, 2)
	if len(lines) != 2 || shown != 2 {
		t.Fatalf("lines = %v (shown=%d), want 2 lines", lines, shown)
	}
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1", remaining)
	}
	if !strings.Contains(lines[0], "/a") || !strings.Contains(lines[1], "/b") {
		t.Errorf("expected the largest entries first, got %v", lines)
	}
}

func TestReclaimableLinesUnlimitedWhenLimitIsZeroOrNegative(t *testing.T) {
	entries := []clean.CacheEntry{{Path: "/a", Bytes: 500}, {Path: "/b", Bytes: 400}, {Path: "/c", Bytes: 300}}
	for _, limit := range []int{0, -1} {
		lines, shown, remaining := reclaimableLines(entries, limit)
		if len(lines) != 3 || shown != 3 || remaining != 0 {
			t.Errorf("limit=%d: lines=%v shown=%d remaining=%d, want all 3 with no remainder", limit, lines, shown, remaining)
		}
	}
}

func TestReclaimableLinesEmptyInput(t *testing.T) {
	lines, shown, remaining := reclaimableLines(nil, 5)
	if len(lines) != 0 || shown != 0 || remaining != 0 {
		t.Errorf("empty input should yield nothing, got lines=%v shown=%d remaining=%d", lines, shown, remaining)
	}
}

func TestAllCoresLineListsEveryCore(t *testing.T) {
	line := allCoresLine([]float64{12, 45, 99})
	for _, want := range []string{"1: 12%", "2: 45%", "3: 99%"} {
		if !strings.Contains(line, want) {
			t.Errorf("allCoresLine missing %q, got %q", want, line)
		}
	}
}

func TestFormatExcludedMountLine(t *testing.T) {
	got := formatExcludedMount(excludedMount{Mountpoint: "/dev", Fstype: "devfs", TotalBytes: 200 << 20, Reason: "pseudo filesystem (devfs)"})
	for _, want := range []string{"/dev", "devfs", "pseudo filesystem (devfs)"} {
		if !strings.Contains(got, want) {
			t.Errorf("formatExcludedMount missing %q, got %q", want, got)
		}
	}
}

func TestAllCoresLineEmptyInput(t *testing.T) {
	if got := allCoresLine(nil); got != "" {
		t.Errorf("allCoresLine(nil) = %q, want empty", got)
	}
}
