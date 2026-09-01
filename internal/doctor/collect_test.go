package doctor

import (
	"math"
	"testing"
	"time"

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
		{"apfs", "/System/Volumes/VM", 500 * gb, false},
		{"apfs", "/System/Volumes/Data", 500 * gb, true},
		{"nfs4", "/mnt/nas", 4 * gb, true},
		{"smbfs", "/Volumes/NAS", 4 * gb, true},
		{"cifs", "/mnt/share", 4 * gb, true},
	}
	for _, c := range cases {
		if got := isRealFilesystem(c.fstype, c.mount, c.total); got != c.want {
			t.Errorf("isRealFilesystem(%q, %q, %d) = %v, want %v", c.fstype, c.mount, c.total, got, c.want)
		}
	}
}

func TestDiskUsageUnknownMountFailsFastAndCoolsDown(t *testing.T) {
	mount := t.TempDir() + "/does-not-exist"
	start := time.Now()
	if _, ok := diskUsage(mount); ok {
		t.Fatalf("expected diskUsage(%q) to fail for a nonexistent mount", mount)
	}
	if elapsed := time.Since(start); elapsed >= diskUsageTimeout {
		t.Errorf("a plain stat error should fail immediately, not wait out the timeout (took %v)", elapsed)
	}
	badMountsMu.Lock()
	_, onCooldown := badMounts[mount]
	badMountsMu.Unlock()
	if !onCooldown {
		t.Errorf("a failed mount should be recorded so repeated collections don't retry it immediately")
	}
}

func TestFilesystemFilterReasonExplainsEachExclusion(t *testing.T) {
	const gb = uint64(1) << 30
	cases := []struct {
		fstype, mount string
		total         uint64
		wantEmpty     bool // true means "kept", i.e. isRealFilesystem would be true
	}{
		{"apfs", "/", 500 * gb, true},
		{"devfs", "/dev", 200 * gb, false},
		{"apfs", "/", 512 * 1024 * 1024, false},
		{"apfs", "/System/Volumes/VM", 500 * gb, false},
	}
	for _, c := range cases {
		reason := filesystemFilterReason(c.fstype, c.mount, c.total)
		if (reason == "") != c.wantEmpty {
			t.Errorf("filesystemFilterReason(%q, %q, %d) = %q, wantEmpty=%v", c.fstype, c.mount, c.total, reason, c.wantEmpty)
		}
		// isRealFilesystem must stay in lockstep with this — it's defined in terms of it.
		if got := isRealFilesystem(c.fstype, c.mount, c.total); got != c.wantEmpty {
			t.Errorf("isRealFilesystem disagrees with filesystemFilterReason for (%q, %q, %d)", c.fstype, c.mount, c.total)
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
