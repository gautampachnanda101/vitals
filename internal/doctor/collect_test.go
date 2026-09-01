package doctor

import (
	"math"
	"testing"

	"github.com/shirou/gopsutil/v4/cpu"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestIsRealFilesystem(t *testing.T) {
	const gb = uint64(1) << 30
	cases := []struct {
		fstype, mount string
		total         uint64
		want          bool
	}{
		{"apfs", "/", 500 * gb, true},
		{"ext4", "/home", 200 * gb, true},
		{"xfs", "/data", 4 * gb, true},
		{"devfs", "/dev", 200 * gb, false},
		{"tmpfs", "/run", 8 * gb, false},
		{"apfs", "/dev", 200 * gb, false},
		{"ext4", "/proc/sys", 100 * gb, false},
		{"apfs", "/", 512 * 1024 * 1024, false}, // under 1 GiB -> pseudo
		{"overlay", "/var/lib/docker/overlay2/x", 100 * gb, false},
	}
	for _, c := range cases {
		if got := isRealFilesystem(c.fstype, c.mount, c.total); got != c.want {
			t.Errorf("isRealFilesystem(%q, %q, %d) = %v, want %v", c.fstype, c.mount, c.total, got, c.want)
		}
	}
}

func TestCPUStatePercents(t *testing.T) {
	t.Run("splits busy / iowait / steal over the interval", func(t *testing.T) {
		a := cpu.TimesStat{User: 100, System: 20, Idle: 800, Iowait: 50, Steal: 5}
		b := cpu.TimesStat{User: 140, System: 30, Idle: 900, Iowait: 90, Steal: 15}
		// deltas: user 40, sys 10, idle 100, iowait 40, steal 10 -> total 200
		// busy = 200 - 100(idle) - 40(iowait) = 60
		used, iowait, steal := cpuStatePercents(a, b)
		if !approx(used, 30) || !approx(iowait, 20) || !approx(steal, 5) {
			t.Errorf("used=%.2f iowait=%.2f steal=%.2f, want 30 / 20 / 5", used, iowait, steal)
		}
	})

	t.Run("no elapsed time yields zeros", func(t *testing.T) {
		a := cpu.TimesStat{User: 100, Idle: 800}
		used, iowait, steal := cpuStatePercents(a, a)
		if used != 0 || iowait != 0 || steal != 0 {
			t.Errorf("got %.2f/%.2f/%.2f, want zeros", used, iowait, steal)
		}
	})

	t.Run("counter reset does not go negative", func(t *testing.T) {
		a := cpu.TimesStat{User: 500, Idle: 5000, Iowait: 100}
		b := cpu.TimesStat{User: 10, Idle: 100, Iowait: 2}
		used, iowait, steal := cpuStatePercents(a, b)
		if used < 0 || iowait < 0 || steal < 0 {
			t.Errorf("negative percentage after reset: %.2f/%.2f/%.2f", used, iowait, steal)
		}
	})
}
