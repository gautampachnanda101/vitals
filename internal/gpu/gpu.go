// Package gpu reads GPU / accelerator telemetry by shelling out to the vendor
// tools that are already installed (nvidia-smi, rocm-smi) or, on macOS, ioreg —
// never by linking a vendor library, so the binary stays pure-static and cross
// compiles everywhere. The parsers are pure and fixture-tested; Probe is the
// thin impure orchestrator.
package gpu

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Vendor identifies the GPU maker.
type Vendor string

const (
	NVIDIA Vendor = "nvidia"
	AMD    Vendor = "amd"
	Apple  Vendor = "apple"
)

// Device is one GPU's current state. Zero fields mean "not reported".
type Device struct {
	Vendor      Vendor  `json:"vendor"`
	Index       int     `json:"index"`
	Name        string  `json:"name"`
	MemTotalB   uint64  `json:"mem_total_bytes"`
	MemUsedB    uint64  `json:"mem_used_bytes"`
	UtilPct     float64 `json:"util_percent"`
	TempC       float64 `json:"temp_c"`
	PowerW      float64 `json:"power_watts"`
	PowerLimitW float64 `json:"power_limit_watts"`
	ClockMHz    float64 `json:"clock_mhz"`
	ClockMaxMHz float64 `json:"clock_max_mhz"`
	Processes   []Proc  `json:"processes,omitempty"`
}

// Proc is a process holding VRAM on a device.
type Proc struct {
	PID     int32  `json:"pid"`
	Name    string `json:"name"`
	MemUseB uint64 `json:"mem_bytes"`
}

// MemUsedPct is VRAM utilisation, 0 when total is unknown.
func (d Device) MemUsedPct() float64 {
	if d.MemTotalB == 0 {
		return 0
	}
	return float64(d.MemUsedB) / float64(d.MemTotalB) * 100
}

// Probe returns every GPU it can see. It tries, in order: nvidia-smi, rocm-smi,
// and (macOS only) a minimal Apple-silicon device from ioreg. An empty result
// means no supported GPU tooling is present — callers must degrade gracefully.
func Probe() []Device {
	if out, ok := run("nvidia-smi",
		"--query-gpu=index,name,memory.total,memory.used,utilization.gpu,temperature.gpu,power.draw,power.limit,clocks.current.graphics,clocks.max.graphics",
		"--format=csv,noheader,nounits"); ok {
		devs := parseNvidiaSMI(out)
		if apps, ok := run("nvidia-smi",
			"--query-compute-apps=pid,process_name,used_memory",
			"--format=csv,noheader,nounits"); ok {
			attachNvidiaApps(devs, parseNvidiaApps(apps))
		}
		if len(devs) > 0 {
			return devs
		}
	}
	if out, ok := run("rocm-smi", "--showproductname", "--showmemuse", "--showuse", "--showtemp", "--showpower", "--json"); ok {
		if devs := parseRocmSMIJSON(out); len(devs) > 0 {
			return devs
		}
	}
	if runtime.GOOS == "darwin" {
		if out, ok := run("ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator"); ok {
			if devs := parseIORegApple(out); len(devs) > 0 {
				return devs
			}
		}
	}
	return nil
}

func run(name string, args ...string) (string, bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// --- pure parsers --------------------------------------------------------------

const mib = 1024 * 1024

// parseNvidiaSMI parses `--query-gpu=... --format=csv,noheader,nounits` output.
// Memory columns are MiB; a field of "[N/A]" or "" leaves the value zero.
func parseNvidiaSMI(csv string) []Device {
	var out []Device
	for _, line := range strings.Split(strings.TrimSpace(csv), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := splitCSV(line)
		if len(f) < 10 {
			continue
		}
		out = append(out, Device{
			Vendor:      NVIDIA,
			Index:       atoiOr(f[0], len(out)),
			Name:        f[1],
			MemTotalB:   uint64(numOr(f[2], 0) * mib),
			MemUsedB:    uint64(numOr(f[3], 0) * mib),
			UtilPct:     numOr(f[4], 0),
			TempC:       numOr(f[5], 0),
			PowerW:      numOr(f[6], 0),
			PowerLimitW: numOr(f[7], 0),
			ClockMHz:    numOr(f[8], 0),
			ClockMaxMHz: numOr(f[9], 0),
		})
	}
	return out
}

// parseNvidiaApps parses `--query-compute-apps=pid,process_name,used_memory`.
func parseNvidiaApps(csv string) []Proc {
	var out []Proc
	for _, line := range strings.Split(strings.TrimSpace(csv), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := splitCSV(line)
		if len(f) < 3 {
			continue
		}
		out = append(out, Proc{
			PID:     int32(atoiOr(f[0], 0)),
			Name:    f[1],
			MemUseB: uint64(numOr(f[2], 0) * mib),
		})
	}
	return out
}

// attachNvidiaApps assigns compute-app processes to devices. nvidia-smi's
// compute-apps query does not report a device index, so with a single GPU all
// processes belong to it; with multiple GPUs the processes are attached to
// every device (best effort) unless a future --query adds the index.
func attachNvidiaApps(devs []Device, procs []Proc) {
	if len(devs) == 0 || len(procs) == 0 {
		return
	}
	if len(devs) == 1 {
		devs[0].Processes = procs
		return
	}
	for i := range devs {
		devs[i].Processes = procs
	}
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

// numOr parses a float, treating "[N/A]", "N/A", "" and unparseable values as def.
func numOr(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "n/a") || strings.HasPrefix(s, "[") {
		return def
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return n
	}
	return def
}

// rocmCard is the subset of `rocm-smi --json` we read; rocm-smi's key names
// have drifted across versions, so several spellings are accepted.
type rocmCard struct {
	Name     string `json:"Card series"`
	NameAlt  string `json:"Card model"`
	MemUsed  string `json:"VRAM Total Used Memory (B)"`
	MemTotal string `json:"VRAM Total Memory (B)"`
	GPUUse   string `json:"GPU use (%)"`
	Temp     string `json:"Temperature (Sensor edge) (C)"`
	TempAlt  string `json:"Temperature (Sensor junction) (C)"`
	Power    string `json:"Average Graphics Package Power (W)"`
	PowerAlt string `json:"Current Socket Graphics Package Power (W)"`
}

var rocmCardKey = regexp.MustCompile(`^card\d+$`)

// parseRocmSMIJSON parses `rocm-smi --json` output: an object keyed by "card0",
// "card1", ...
func parseRocmSMIJSON(s string) []Device {
	var raw map[string]rocmCard
	if json.Unmarshal([]byte(s), &raw) != nil {
		return nil
	}
	// Deterministic order by card index.
	keys := make([]string, 0, len(raw))
	for k := range raw {
		if rocmCardKey.MatchString(k) {
			keys = append(keys, k)
		}
	}
	strSort(keys)

	var out []Device
	for i, k := range keys {
		c := raw[k]
		name := firstNonEmpty(c.Name, c.NameAlt, "AMD GPU")
		out = append(out, Device{
			Vendor:    AMD,
			Index:     i,
			Name:      name,
			MemUsedB:  uint64(numOr(c.MemUsed, 0)),
			MemTotalB: uint64(numOr(c.MemTotal, 0)),
			UtilPct:   numOr(c.GPUUse, 0),
			TempC:     numOr(firstNonEmpty(c.Temp, c.TempAlt), 0),
			PowerW:    numOr(firstNonEmpty(c.Power, c.PowerAlt), 0),
		})
	}
	return out
}

var appleGPUModel = regexp.MustCompile(`"model"\s*=\s*<"?([^">]+)"?>`)

// parseIORegApple pulls just the Apple-silicon GPU name out of
// `ioreg -c IOAccelerator`. Unified memory means there is no separate VRAM
// figure to report; doctor treats Apple GPU pressure via system RAM instead.
func parseIORegApple(s string) []Device {
	if !strings.Contains(s, "IOAccelerator") && !strings.Contains(s, "AGXAccelerator") {
		return nil
	}
	name := "Apple GPU"
	if m := appleGPUModel.FindStringSubmatch(s); m != nil {
		name = strings.TrimSpace(m[1])
	}
	return []Device{{Vendor: Apple, Index: 0, Name: name}}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func strSort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
