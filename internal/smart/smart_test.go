package smart

import (
	"errors"
	"strings"
	"testing"
)

// --- parseHealth ------------------------------------------------------

const nvmeJSON = `{
  "device": {"name": "/dev/nvme0", "type": "nvme"},
  "smart_status": {"passed": true},
  "temperature": {"current": 41},
  "nvme_smart_health_information_log": {"percentage_used": 7, "available_spare": 100}
}`

const ataJSON = `{
  "device": {"name": "/dev/sda", "type": "sat"},
  "smart_status": {"passed": true},
  "temperature": {"current": 33},
  "ata_smart_attributes": {"table": []}
}`

const failingJSON = `{"smart_status": {"passed": false}, "temperature": {"current": 55}}`

func TestParseHealthNVMe(t *testing.T) {
	h, ok := parseHealth([]byte(nvmeJSON))
	if !ok {
		t.Fatal("expected ok")
	}
	if !h.Passed || h.TempC != 41 || h.WearPct != 7 {
		t.Errorf("h = %+v", h)
	}
}

func TestParseHealthATAHasNoWearIndicator(t *testing.T) {
	h, ok := parseHealth([]byte(ataJSON))
	if !ok {
		t.Fatal("expected ok")
	}
	if !h.Passed || h.TempC != 33 {
		t.Errorf("h = %+v", h)
	}
	if h.WearPct != -1 {
		t.Errorf("ATA has no percentage_used; WearPct should be -1, got %v", h.WearPct)
	}
}

func TestParseHealthFailingDrive(t *testing.T) {
	h, ok := parseHealth([]byte(failingJSON))
	if !ok || h.Passed {
		t.Errorf("expected a parsed, failing verdict; ok=%v h=%+v", ok, h)
	}
}

func TestParseHealthRejectsNonSmartctlOutput(t *testing.T) {
	for _, in := range []string{"", "not json", `{"foo": 1}`, `{"smart_status": {}}`} {
		if _, ok := parseHealth([]byte(in)); ok {
			t.Errorf("parseHealth(%q) should be !ok — no pass/fail verdict", in)
		}
	}
}

// --- device resolution ---------------------------------------------------

func TestParsePartOfWhole(t *testing.T) {
	out := `   Device Identifier:        disk3s1s1
   Device Node:              /dev/disk3s1s1
   Part of Whole:            disk3
   Mount Point:              /
`
	if got := parsePartOfWhole([]byte(out)); got != "/dev/disk3" {
		t.Errorf("parsePartOfWhole = %q, want /dev/disk3", got)
	}
	if got := parsePartOfWhole([]byte("no such field here")); got != "" {
		t.Errorf("missing field should give empty, got %q", got)
	}
}

func TestLinuxWholeDisk(t *testing.T) {
	cases := map[string]string{
		"/dev/sda1":        "/dev/sda",
		"/dev/sdb":         "/dev/sdb",
		"/dev/vdb2":        "/dev/vdb",
		"/dev/nvme0n1p1":   "/dev/nvme0n1",
		"/dev/nvme1n1":     "/dev/nvme1n1",
		"/dev/mmcblk0p2":   "/dev/mmcblk0",
		"/dev/mapper/root": "/dev/mapper/root", // unrecognised: passed through
		"tmpfs":            "",
	}
	for in, want := range cases {
		if got := linuxWholeDisk(in); got != want {
			t.Errorf("linuxWholeDisk(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- probe orchestration ----------------------------------------------

func fakeDeps(goos string) (deps, *[]string) {
	var calls []string
	d := deps{
		goos:     goos,
		lookPath: func(string) (string, error) { return "/usr/bin/smartctl", nil },
		run: func(name string, args ...string) ([]byte, error) {
			calls = append(calls, name+" "+strings.Join(args, " "))
			return []byte(nvmeJSON), nil
		},
		diskutil: func(mount string) ([]byte, error) {
			return []byte("Part of Whole:            disk2\n"), nil
		},
	}
	return d, &calls
}

func TestProbeSkipsWindows(t *testing.T) {
	d, _ := fakeDeps("windows")
	if got := probe(d, []Target{{Mount: "C:", Device: `\\.\PhysicalDrive0`}}); got != nil {
		t.Errorf("windows is unsupported; want nil, got %+v", got)
	}
}

func TestProbeSkipsWhenSmartctlAbsent(t *testing.T) {
	d, _ := fakeDeps("linux")
	d.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if got := probe(d, []Target{{Mount: "/", Device: "/dev/sda1"}}); got != nil {
		t.Errorf("no smartctl -> nil, got %+v", got)
	}
}

func TestProbeMacResolvesViaDiskutilAndDedups(t *testing.T) {
	d, calls := fakeDeps("darwin")
	got := probe(d, []Target{
		{Mount: "/", Device: "/dev/disk3s1s1"},
		{Mount: "/System/Volumes/Data", Device: "/dev/disk3s5"}, // same physical disk
	})
	if len(got) != 1 {
		t.Fatalf("two mounts on one disk -> one Health; got %d: %+v", len(got), got)
	}
	if got[0].Device != "/dev/disk2" || got[0].Mount != "/" {
		t.Errorf("Health = %+v", got[0])
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "smartctl -a -j /dev/disk2") {
		t.Errorf("smartctl calls = %v", *calls)
	}
}

func TestProbeLinuxUsesStrippedDevice(t *testing.T) {
	d, calls := fakeDeps("linux")
	probe(d, []Target{{Mount: "/", Device: "/dev/nvme0n1p2"}})
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "/dev/nvme0n1") || strings.Contains((*calls)[0], "p2") {
		t.Errorf("expected the whole-disk device; calls = %v", *calls)
	}
}

func TestProbeUnsupportedUnixHasNoResolver(t *testing.T) {
	d, _ := fakeDeps("freebsd") // not darwin, not linux, not windows
	if got := probe(d, []Target{{Mount: "/", Device: "/dev/ada0p1"}}); got != nil {
		t.Errorf("no device resolver for this OS -> nil, got %+v", got)
	}
}

func TestProbeSkipsTargetWithUnusableOutput(t *testing.T) {
	d, _ := fakeDeps("linux")
	d.run = func(string, ...string) ([]byte, error) { return []byte("garbage"), errors.New("exit 2") }
	if got := probe(d, []Target{{Mount: "/", Device: "/dev/sda1"}}); got != nil {
		t.Errorf("unparseable smartctl output -> target skipped, got %+v", got)
	}
}

func TestProbeDefaultDepsWiringDoesNotPanic(t *testing.T) {
	// Exercises the real lookPath/exec wiring once — outcome depends on
	// whether the runner has smartctl and a SMART-capable disk, so just
	// assert it returns without blowing up.
	_ = Probe([]Target{{Mount: "/", Device: "/dev/none"}})
}

func TestDefaultDepsClosuresRunRealCommands(t *testing.T) {
	// Cover the defaultDeps.run / .diskutil closures themselves — a
	// bogus binary just errors, which is all we assert.
	if _, err := defaultDeps.run("vitals-no-such-binary-xyz"); err == nil {
		t.Error("expected an error running a nonexistent binary")
	}
	if _, err := defaultDeps.diskutil("/definitely/not/a/mount"); err == nil {
		t.Skip("diskutil not present or behaved unexpectedly on this OS")
	}
}
