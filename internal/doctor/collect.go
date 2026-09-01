package doctor

import (
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/sensors"

	"vitals/internal/gpu"
	"vitals/internal/llm"
)

// Options configures a doctor run.
type Options struct {
	OllamaURL string
	Window    time.Duration // sampling window for rate-based signals
}

// Collect builds a Snapshot from the live system. It is deliberately thin: all
// judgement lives in Analyze, which is pure and fixture-tested.
func Collect(opts Options) Snapshot {
	if opts.Window <= 0 {
		opts.Window = 700 * time.Millisecond
	}
	var s Snapshot

	// CPU: two Times readings across the window for per-state percentages.
	t0 := firstTimes()
	sw0 := swapCounters()
	io0 := diskCounters()
	time.Sleep(opts.Window)
	t1 := firstTimes()
	sw1 := swapCounters()
	io1 := diskCounters()

	used, iowait, steal := cpuStatePercents(t0, t1)
	s.CPU.UsedPct, s.CPU.IOWaitPct, s.CPU.StealPct = used, iowait, steal
	s.CPU.Cores, _ = cpu.Counts(true)
	if la, err := load.Avg(); err == nil {
		s.CPU.Load1 = la.Load1
	}
	if info, err := cpu.Info(); err == nil && len(info) > 0 {
		s.CPU.FreqMHz = info[0].Mhz
	}

	// Memory + swap rate.
	if vm, err := mem.VirtualMemory(); err == nil {
		s.Memory.UsedPct = vm.UsedPercent
	}
	if sm, err := mem.SwapMemory(); err == nil {
		s.Memory.SwapUsedPct = sm.UsedPercent
		s.Memory.SwapTotal = sm.Total
	}
	secs := opts.Window.Seconds()
	if secs > 0 && sw1.in >= sw0.in {
		s.Memory.SwapInPerSec = float64(sw1.in-sw0.in) / secs
	}
	if secs > 0 && sw1.out >= sw0.out {
		s.Memory.SwapOutPerSec = float64(sw1.out-sw0.out) / secs
	}

	// Disks: usage per real mount, plus a device-wide busy/latency estimate.
	s.Disks = collectDisks(io0, io1, opts.Window)

	// GPUs via the vendor CLIs (nvidia-smi / rocm-smi / ioreg); empty when none.
	for _, g := range gpu.Probe() {
		s.GPUs = append(s.GPUs, GPU{
			Name:         g.Name,
			VRAMUsed:     g.MemUsedB,
			VRAMTotal:    g.MemTotalB,
			UtilPct:      g.UtilPct,
			TempC:        g.TempC,
			ClockMHz:     g.ClockMHz,
			BaseClockMHz: g.ClockMaxMHz,
		})
	}

	// Thermal (best effort; throttle detection is platform-specific and TODO).
	if temps, err := sensors.SensorsTemperatures(); err == nil {
		for _, tp := range temps {
			if tp.Temperature > s.Thermal.CPUTempC {
				s.Thermal.CPUTempC = tp.Temperature
			}
		}
	}

	// LLM: Ollama per-model offload, correlated with runtime-process CPU.
	models := llm.OllamaModels(opts.OllamaURL)
	procs := llm.ScanProcesses()
	var hostCPU float64
	for _, p := range procs {
		if p.Runtime == "ollama" && p.CPUPct > hostCPU {
			hostCPU = p.CPUPct
		}
	}
	for _, m := range models {
		s.LLM = append(s.LLM, LLMModel{
			Name:       m.Name,
			OffloadPct: m.GPUOffload,
			HostCPUPct: hostCPU,
		})
	}

	return s
}

// cpuStatePercents converts two cumulative CPU time readings into busy / iowait
// / steal percentages over the interval. Pure and unit-tested.
func cpuStatePercents(a, b cpu.TimesStat) (used, iowait, steal float64) {
	d := func(x, y float64) float64 {
		if y < x {
			return 0
		}
		return y - x
	}
	dUser := d(a.User, b.User)
	dSys := d(a.System, b.System)
	dNice := d(a.Nice, b.Nice)
	dIdle := d(a.Idle, b.Idle)
	dIowait := d(a.Iowait, b.Iowait)
	dIrq := d(a.Irq, b.Irq)
	dSoft := d(a.Softirq, b.Softirq)
	dSteal := d(a.Steal, b.Steal)

	total := dUser + dSys + dNice + dIdle + dIowait + dIrq + dSoft + dSteal
	if total <= 0 {
		return 0, 0, 0
	}
	busy := total - dIdle - dIowait
	return busy / total * 100, dIowait / total * 100, dSteal / total * 100
}

func firstTimes() cpu.TimesStat {
	ts, err := cpu.Times(false)
	if err != nil || len(ts) == 0 {
		return cpu.TimesStat{}
	}
	return ts[0]
}

type swapIO struct{ in, out uint64 }

func swapCounters() swapIO {
	sm, err := mem.SwapMemory()
	if err != nil {
		return swapIO{}
	}
	return swapIO{in: sm.Sin, out: sm.Sout}
}

func diskCounters() map[string]disk.IOCountersStat {
	m, err := disk.IOCounters()
	if err != nil {
		return nil
	}
	return m
}

// pseudoFS are filesystem types that are never a real, fillable disk.
var pseudoFS = map[string]bool{
	"devfs": true, "devtmpfs": true, "tmpfs": true, "overlay": true,
	"squashfs": true, "autofs": true, "proc": true, "sysfs": true,
	"cgroup": true, "cgroup2": true, "nullfs": true, "none": true,
	"ramfs": true, "fuse.portal": true, "fdescfs": true, "efivarfs": true,
}

// isRealFilesystem decides whether a mount is worth a disk-pressure check:
// a known non-pseudo fstype backing at least a gigabyte.
func isRealFilesystem(fstype, mountpoint string, totalBytes uint64) bool {
	if pseudoFS[fstype] {
		return false
	}
	if totalBytes < 1<<30 {
		return false
	}
	switch {
	case mountpoint == "/dev", mountpoint == "/run",
		len(mountpoint) >= 5 && mountpoint[:5] == "/proc",
		len(mountpoint) >= 4 && mountpoint[:4] == "/sys":
		return false
	}
	return true
}

func collectDisks(io0, io1 map[string]disk.IOCountersStat, window time.Duration) []Disk {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Disk
	for _, p := range parts {
		if seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		if !isRealFilesystem(p.Fstype, p.Mountpoint, u.Total) {
			continue
		}
		d := Disk{Mount: p.Mountpoint, UsedPct: u.UsedPercent, FreeBytes: u.Free}
		if c0, ok0 := io0[p.Device]; ok0 {
			if c1, ok1 := io1[p.Device]; ok1 {
				ms := float64(window.Milliseconds())
				if ms > 0 && c1.IoTime >= c0.IoTime {
					d.UtilPct = float64(c1.IoTime-c0.IoTime) / ms * 100
				}
				ops := (c1.ReadCount + c1.WriteCount) - (c0.ReadCount + c0.WriteCount)
				busy := c1.IoTime - c0.IoTime
				if ops > 0 {
					d.AwaitMS = float64(busy) / float64(ops)
				}
			}
		}
		out = append(out, d)
	}
	return out
}
