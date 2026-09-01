package doctor

import (
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
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

	// CPU: two Times readings across the window for per-state percentages, plus
	// per-core times for imbalance detection and a primed process list for
	// top-consumer attribution — all riding the same sleep, at no extra cost.
	t0 := firstTimes()
	pc0 := percoreTimes()
	sw0 := swapCounters()
	io0 := diskCounters()
	net0 := netCounters()
	allProcs, _ := process.Processes()
	for _, p := range allProcs {
		_, _ = p.Percent(0) // prime; the real reading comes after the sleep below
	}
	time.Sleep(opts.Window)
	t1 := firstTimes()
	pc1 := percoreTimes()
	sw1 := swapCounters()
	io1 := diskCounters()
	net1 := netCounters()

	used, iowait, steal := cpuStatePercents(t0, t1)
	s.CPU.UsedPct, s.CPU.IOWaitPct, s.CPU.StealPct = used, iowait, steal
	s.CPU.Cores, _ = cpu.Counts(true)
	s.CPU.PerCorePct = perCorePercents(pc0, pc1)
	if la, err := load.Avg(); err == nil {
		s.CPU.Load1 = la.Load1
	}
	if info, err := cpu.Info(); err == nil && len(info) > 0 && info[0].Mhz >= 100 {
		s.CPU.FreqMHz = info[0].Mhz // gopsutil reports a bogus tiny value on Apple silicon
	}
	s.CPU.TopProc, s.Memory.TopProc = topProcs(allProcs)

	// Memory + swap rate.
	if vm, err := mem.VirtualMemory(); err == nil {
		s.Memory.UsedPct = vm.UsedPercent
		if vm.Total > 0 {
			s.Memory.AvailablePct = float64(vm.Available) / float64(vm.Total) * 100
		}
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

	// Network: per-interface throughput over the window.
	s.Net = netDelta(net0, net1, opts.Window)

	// Power / battery, best effort via the OS tools.
	s.Power = collectPower()

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

func percoreTimes() []cpu.TimesStat {
	ts, err := cpu.Times(true)
	if err != nil {
		return nil
	}
	return ts
}

// perCorePercents converts two per-core Times readings into busy percentages,
// core by core. gopsutil returns cores in a stable order within a run, so
// pairing by index across the two calls is safe.
func perCorePercents(a, b []cpu.TimesStat) []float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return nil
	}
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		used, _, _ := cpuStatePercents(a[i], b[i])
		out[i] = used
	}
	return out
}

// topProcs scans a primed process list (Percent(0) already called once before
// the caller's sample window elapsed) and returns the top CPU consumer and the
// top RSS consumer in a single pass.
func topProcs(procs []*process.Process) (topCPU, topMem ProcRef) {
	for _, p := range procs {
		cpuPct, err := p.Percent(0)
		if err == nil && cpuPct > topCPU.CPUPct {
			name, _ := p.Name()
			topCPU = ProcRef{PID: p.Pid, Name: name, CPUPct: cpuPct}
		}
		if mi, err := p.MemoryInfo(); err == nil && mi != nil && mi.RSS > topMem.RSSBytes {
			name, _ := p.Name()
			topMem = ProcRef{PID: p.Pid, Name: name, RSSBytes: mi.RSS}
		}
	}
	return
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
		strings.HasPrefix(mountpoint, "/proc"),
		strings.HasPrefix(mountpoint, "/sys"),
		// macOS APFS system volumes share their container's stats with "/";
		// keep the root and the user data volume, drop the rest.
		strings.HasPrefix(mountpoint, "/System/Volumes/") && mountpoint != "/System/Volumes/Data":
		return false
	}
	return true
}

// diskUsageTimeout bounds how long collectDisks waits on a single mount's
// space stat. A network mount (NFS/SMB/AFP) whose server has vanished blocks
// the underlying syscall in the kernel indefinitely and cannot be cancelled
// from userspace — this timeout only bounds vitals' own wait, not the
// goroutine, which can outlive it and leak. mountCooldown keeps a mount that
// just timed out from spawning a fresh leaked goroutine on every subsequent
// collection (relevant to `vitals serve`, which collects on every scrape).
const diskUsageTimeout = 1500 * time.Millisecond
const mountCooldown = 60 * time.Second

var (
	badMountsMu sync.Mutex
	badMounts   = map[string]time.Time{}
)

// diskUsage wraps disk.Usage with the timeout/cooldown behaviour above.
func diskUsage(mount string) (*disk.UsageStat, bool) {
	badMountsMu.Lock()
	last, onCooldown := badMounts[mount]
	badMountsMu.Unlock()
	if onCooldown && time.Since(last) < mountCooldown {
		return nil, false
	}

	type result struct {
		u   *disk.UsageStat
		err error
	}
	ch := make(chan result, 1)
	go func() { u, err := disk.Usage(mount); ch <- result{u, err} }()
	select {
	case r := <-ch:
		if r.err != nil {
			markMountBad(mount)
			return nil, false
		}
		return r.u, true
	case <-time.After(diskUsageTimeout):
		markMountBad(mount)
		return nil, false
	}
}

func markMountBad(mount string) {
	badMountsMu.Lock()
	badMounts[mount] = time.Now()
	badMountsMu.Unlock()
}

func collectDisks(io0, io1 map[string]disk.IOCountersStat, window time.Duration) []Disk {
	// true: include network/remote filesystems (NFS, SMB, AFP) alongside local
	// disks — isRealFilesystem below still filters out pseudo and system mounts.
	parts, err := disk.Partitions(true)
	if err != nil {
		return nil
	}
	hist := loadDiskHistory()
	now := time.Now()
	seen := map[string]bool{}
	var out []Disk
	for _, p := range parts {
		if seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		u, ok := diskUsage(p.Mountpoint)
		if !ok || u.Total == 0 {
			continue
		}
		if !isRealFilesystem(p.Fstype, p.Mountpoint, u.Total) {
			continue
		}
		d := Disk{Mount: p.Mountpoint, UsedPct: u.UsedPercent, FreeBytes: u.Free, InodesUsedPct: u.InodesUsedPercent}
		d.GrowthBytesPerSec = diskGrowthRate(hist, p.Mountpoint, u.Free, now)
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
				if secs := window.Seconds(); secs > 0 {
					d.IOPS = float64(ops) / secs
				}
			}
		}
		out = append(out, d)
	}
	saveDiskHistory(hist)
	return out
}
