// Package smart reads S.M.A.R.T. health for physical disks by shelling
// out to smartmontools' `smartctl` — an optional companion tool, never a
// linked library, so the binary stays pure-static. The parsers are pure
// and fixture-tested; Probe is the thin impure orchestrator.
//
// Scope (roadmap item 010, validated against real hardware where noted):
//   - smart_status.passed and temperature.current are the universal
//     fields across NVMe and ATA/SATA smartctl JSON — the baseline.
//   - percentage_used (NVMe's own 0–100 wear indicator) is read when
//     present; ATA wear lives in a vendor-inconsistent attribute table
//     that isn't parsed here.
//   - macOS device resolution uses `diskutil info <mount>` ("Part of
//     Whole") — validated: smartctl rejects gopsutil's /dev/diskNsM
//     partition path but accepts the whole-disk /dev/diskN.
//   - Linux resolution strips a partition suffix off the gopsutil
//     device name (/dev/sda1 → /dev/sda, /dev/nvme0n1p1 → /dev/nvme0n1).
//   - Windows is unsupported for now: its \\.\PhysicalDriveN addressing
//     wasn't validated. Probe returns nothing there.
package smart

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// Target is one mount and the raw device gopsutil reported for it.
type Target struct {
	Mount  string
	Device string
}

// Health is one physical disk's S.M.A.R.T. summary. WearPct is -1 when
// no wear indicator was available (ATA drives, or an NVMe log without
// the field).
type Health struct {
	Device  string  `json:"device"`
	Mount   string  `json:"mount"`
	Passed  bool    `json:"passed"`
	TempC   float64 `json:"temp_c"`
	WearPct float64 `json:"wear_percent"`
}

// deps is the impure surface, injectable for tests.
type deps struct {
	goos     string
	lookPath func(string) (string, error)
	run      func(name string, args ...string) ([]byte, error)
	diskutil func(mount string) ([]byte, error) // macOS "Part of Whole" resolution
}

var defaultDeps = deps{
	goos:     runtime.GOOS,
	lookPath: exec.LookPath,
	run:      func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).Output() },
	diskutil: func(mount string) ([]byte, error) { return exec.Command("diskutil", "info", mount).Output() },
}

// Probe resolves each target to a whole-disk device and runs
// `smartctl -a -j` against it. Targets that can't be assessed — no
// smartctl on PATH, an unsupported OS, a device that can't be resolved
// or isn't SMART-capable — are simply absent from the result. Multiple
// mounts on one physical disk yield a single Health.
func Probe(targets []Target) []Health { return probe(defaultDeps, targets) }

func probe(d deps, targets []Target) []Health {
	if d.goos == "windows" {
		return nil
	}
	if _, err := d.lookPath("smartctl"); err != nil {
		return nil
	}

	seen := map[string]bool{}
	var out []Health
	for _, t := range targets {
		dev := resolveDevice(d, t)
		if dev == "" || seen[dev] {
			continue
		}
		seen[dev] = true

		// smartctl exits non-zero for benign reasons (a passing disk
		// with logged errors, standby, ...) yet still prints valid
		// JSON, so parse whatever it wrote and only bail if the JSON
		// itself is unusable — the error is deliberately ignored.
		raw, _ := d.run("smartctl", "-a", "-j", dev)
		h, ok := parseHealth(raw)
		if !ok {
			continue
		}
		h.Device = dev
		h.Mount = t.Mount
		out = append(out, h)
	}
	return out
}

// resolveDevice turns a mount + gopsutil device into something smartctl
// accepts: the whole physical disk.
func resolveDevice(d deps, t Target) string {
	switch d.goos {
	case "darwin":
		raw, err := d.diskutil(t.Mount)
		if err != nil {
			return ""
		}
		return parsePartOfWhole(raw)
	case "linux":
		return linuxWholeDisk(t.Device)
	default:
		return ""
	}
}

var partOfWholeRE = regexp.MustCompile(`(?m)^\s*Part of Whole:\s*(\S+)\s*$`)

// parsePartOfWhole pulls the whole-disk name out of `diskutil info`
// output and returns it as a /dev path.
func parsePartOfWhole(diskutilOutput []byte) string {
	m := partOfWholeRE.FindSubmatch(diskutilOutput)
	if m == nil {
		return ""
	}
	name := string(m[1]) // \S+ in the regex — already non-empty, no whitespace
	if strings.HasPrefix(name, "/dev/") {
		return name
	}
	return "/dev/" + name
}

var (
	linuxNVMePartRE = regexp.MustCompile(`^(/dev/nvme\d+n\d+)p\d+$`)
	linuxMMCPartRE  = regexp.MustCompile(`^(/dev/mmcblk\d+)p\d+$`)
	linuxSCSIPartRE = regexp.MustCompile(`^(/dev/[a-z]+)\d+$`) // sda1 -> sda, vdb2 -> vdb
)

// linuxWholeDisk strips a partition suffix off a Linux device name. An
// already-whole device (or an unrecognised shape, e.g. a mapper/LVM
// path) is returned unchanged — smartctl can often still address those,
// and if it can't, parseHealth just fails and the target is skipped.
func linuxWholeDisk(dev string) string {
	if !strings.HasPrefix(dev, "/dev/") {
		return ""
	}
	for _, re := range []*regexp.Regexp{linuxNVMePartRE, linuxMMCPartRE, linuxSCSIPartRE} {
		if m := re.FindStringSubmatch(dev); m != nil {
			return m[1]
		}
	}
	return dev
}

// smartctlJSON is the subset of `smartctl -a -j` output vitals reads.
type smartctlJSON struct {
	SmartStatus struct {
		Passed *bool `json:"passed"`
	} `json:"smart_status"`
	Temperature struct {
		Current *float64 `json:"current"`
	} `json:"temperature"`
	NVMeHealth struct {
		PercentageUsed *float64 `json:"percentage_used"`
	} `json:"nvme_smart_health_information_log"`
}

// parseHealth turns smartctl JSON into a Health. ok is false when the
// output isn't smartctl JSON at all or carries no pass/fail verdict —
// the one field vitals actually needs.
func parseHealth(raw []byte) (Health, bool) {
	var j smartctlJSON
	if json.Unmarshal(raw, &j) != nil {
		return Health{}, false
	}
	if j.SmartStatus.Passed == nil {
		return Health{}, false
	}
	h := Health{Passed: *j.SmartStatus.Passed, WearPct: -1}
	if j.Temperature.Current != nil {
		h.TempC = *j.Temperature.Current
	}
	if j.NVMeHealth.PercentageUsed != nil {
		h.WearPct = *j.NVMeHealth.PercentageUsed
	}
	return h, true
}
