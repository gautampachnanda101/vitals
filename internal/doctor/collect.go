package doctor

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
)

// Options configures a doctor run.
type Options struct {
	OllamaURL string
	Window    time.Duration // sampling window for rate-based signals
}

// Collect builds a Snapshot from the live system. It is deliberately thin: all
// judgement lives in Analyze, which is pure and fixture-tested.
func Collect(opts Options) Snapshot { return collect(defaultSource, opts) }

// collect is Collect's testable core: src is injected (defaultSource in
// production) so a test can substitute fakes for each signal. Named
// per-signal collectors below (collectCPU/collectMemory/collectGPUs/
// collectThermal/collectLLM) turn the raw before/after readings this
// function takes into their slice of the Snapshot — split out so each is
// independently testable/reviewable, rather than one 100-line function
// doing everything. Disk/Power are the deliberate exception, still
// collected inline via the existing collectDisks/collectPower calls; see
// source.go's own comment for why.
func collect(src source, opts Options) Snapshot {
	if opts.Window <= 0 {
		opts.Window = 700 * time.Millisecond
	}
	var s Snapshot

	// Two readings across the window for every rate-based signal, plus a
	// primed process list for top-consumer attribution — all riding the
	// same sleep, at no extra cost. This shared window is why CPU/Memory/
	// Net all get their raw before/after readings here in collect, not
	// inside their own named collector: splitting the sampling itself
	// across separate functions would mean either a shared window (no
	// simpler than this) or one sleep per signal (multiplying the real
	// wall-clock cost of every `vitals doctor` run by the number of
	// signals) — collectCPU/collectMemory/etc. are shaped as pure
	// functions over already-read data specifically to avoid that.
	t0 := firstTimes(src)
	pc0 := percoreTimes(src)
	sw0 := swapCounters(src)
	io0 := diskCounters()
	net0 := netCounters(src)
	procs, _ := src.processes()
	for _, p := range procs {
		_, _ = p.Percent(0) // prime; the real reading comes after the sleep below
	}
	time.Sleep(opts.Window)
	t1 := firstTimes(src)
	pc1 := percoreTimes(src)
	sw1 := swapCounters(src)
	io1 := diskCounters()
	net1 := netCounters(src)
	topCPU, topMem := topProcs(procs)

	s.CPU = collectCPU(src, t0, t1, pc0, pc1, topCPU)
	s.Memory = collectMemory(src, sw0, sw1, opts.Window, topMem)

	// Disks: usage per real mount, plus a device-wide busy/latency estimate.
	s.Disks = collectDisks(io0, io1, opts.Window)

	// Network: per-interface throughput over the window.
	s.Net = netDelta(net0, net1, opts.Window)

	// Power / battery, best effort via the OS tools.
	s.Power = collectPower()

	s.GPUs = collectGPUs(src)
	s.Thermal = collectThermal(src)
	s.LLM = collectLLM(src, opts)

	return s
}

// collectCPU turns two CPU-times readings (whole-machine and per-core)
// plus the already-computed top CPU consumer into the CPU signal. Pure
// over its arguments except for the three additional live reads
// (cpuCounts/loadAvg/cpuInfo) that have no "before/after" shape of their
// own.
func collectCPU(src source, t0, t1 cpu.TimesStat, pc0, pc1 []cpu.TimesStat, topCPU ProcRef) CPU {
	var c CPU
	c.UsedPct, c.IOWaitPct, c.StealPct = cpuStatePercents(t0, t1)
	c.Cores, _ = src.cpuCounts(true)
	c.PerCorePct = perCorePercents(pc0, pc1)
	if la, err := src.loadAvg(); err == nil {
		c.Load1 = la.Load1
	}
	if info, err := src.cpuInfo(); err == nil && len(info) > 0 && info[0].Mhz >= 100 {
		c.FreqMHz = info[0].Mhz // gopsutil reports a bogus tiny value on Apple silicon
	}
	c.TopProc = topCPU
	return c
}

// collectMemory turns a virtual-memory reading, a swap-rate before/after
// pair, and the already-computed top RSS consumer into the Memory signal.
func collectMemory(src source, sw0, sw1 swapIO, window time.Duration, topMem ProcRef) Memory {
	var m Memory
	if vm, err := src.virtualMemory(); err == nil {
		m.UsedPct = vm.UsedPercent
		if vm.Total > 0 {
			m.AvailablePct = float64(vm.Available) / float64(vm.Total) * 100
		}
	}
	if sm, err := src.swapMemory(); err == nil {
		m.SwapUsedPct = sm.UsedPercent
		m.SwapTotal = sm.Total
	}
	secs := window.Seconds()
	if secs > 0 && sw1.in >= sw0.in {
		m.SwapInPerSec = float64(sw1.in-sw0.in) / secs
	}
	if secs > 0 && sw1.out >= sw0.out {
		m.SwapOutPerSec = float64(sw1.out-sw0.out) / secs
	}
	m.TopProc = topMem
	return m
}

// collectGPUs maps the vendor-neutral gpu.Probe() result (nvidia-smi /
// rocm-smi / ioreg) onto this package's own GPU type; empty when none.
func collectGPUs(src source) []GPU {
	var out []GPU
	for _, g := range src.gpuProbe() {
		var procs []GPUProc
		for _, p := range g.Processes {
			procs = append(procs, GPUProc{PID: p.PID, Name: p.Name, VRAMUsed: p.MemUseB})
		}
		out = append(out, GPU{
			Name:         g.Name,
			VRAMUsed:     g.MemUsedB,
			VRAMTotal:    g.MemTotalB,
			UtilPct:      g.UtilPct,
			TempC:        g.TempC,
			ClockMHz:     g.ClockMHz,
			BaseClockMHz: g.ClockMaxMHz,
			Processes:    procs,
		})
	}
	return out
}

// collectThermal reports the hottest sensor reading found, best effort;
// throttle detection is platform-specific and TODO.
func collectThermal(src source) Thermal {
	var t Thermal
	if temps, err := src.sensorsTemps(); err == nil {
		for _, tp := range temps {
			if tp.Temperature > t.CPUTempC {
				t.CPUTempC = tp.Temperature
			}
		}
	}
	return t
}

// collectLLM reports Ollama's per-model offload, correlated with the
// runtime process's own host CPU usage — the "is this model actually
// running on the GPU or quietly falling back to CPU" signal.
func collectLLM(src source, opts Options) []LLMModel {
	models := src.ollamaModels(opts.OllamaURL)
	procs := src.scanProcesses()
	var hostCPU float64
	for _, p := range procs {
		if p.Runtime == "ollama" && p.CPUPct > hostCPU {
			hostCPU = p.CPUPct
		}
	}
	var out []LLMModel
	for _, m := range models {
		out = append(out, LLMModel{
			Name:       m.Name,
			OffloadPct: m.GPUOffload,
			HostCPUPct: hostCPU,
		})
	}
	return out
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

func firstTimes(src source) cpu.TimesStat {
	ts, err := src.cpuTimes(false)
	if err != nil || len(ts) == 0 {
		return cpu.TimesStat{}
	}
	return ts[0]
}

func percoreTimes(src source) []cpu.TimesStat {
	ts, err := src.cpuTimes(true)
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
func topProcs(procs []procSource) (topCPU, topMem ProcRef) {
	for _, p := range procs {
		cpuPct, err := p.Percent(0)
		if err == nil && cpuPct > topCPU.CPUPct {
			name, _ := p.Name()
			topCPU = ProcRef{PID: p.PID(), Name: name, CPUPct: cpuPct}
		}
		if mi, err := p.MemoryInfo(); err == nil && mi != nil && mi.RSS > topMem.RSSBytes {
			name, _ := p.Name()
			topMem = ProcRef{PID: p.PID(), Name: name, RSSBytes: mi.RSS}
		}
	}
	return
}

type swapIO struct{ in, out uint64 }

func swapCounters(src source) swapIO {
	sm, err := src.swapMemory()
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
	return filesystemFilterReason(fstype, mountpoint, totalBytes) == ""
}

// filesystemFilterReason is isRealFilesystem's actual source of truth: it
// names *why* a mount is excluded, empty meaning "kept". The reason powers
// `disk --verbose`'s "filtered out" section — the default view drops these
// silently, but --verbose should be able to say why, not just show more of
// what was already visible.
func filesystemFilterReason(fstype, mountpoint string, totalBytes uint64) string {
	if pseudoFS[fstype] {
		return fmt.Sprintf("pseudo filesystem (%s)", fstype)
	}
	if totalBytes < 1<<30 {
		return "smaller than 1 GiB"
	}
	switch {
	case mountpoint == "/dev", mountpoint == "/run",
		strings.HasPrefix(mountpoint, "/proc"),
		strings.HasPrefix(mountpoint, "/sys"):
		return "kernel/device pseudo-mount"
	// macOS APFS system volumes share their container's stats with "/";
	// keep the root and the user data volume, drop the rest.
	case strings.HasPrefix(mountpoint, "/System/Volumes/") && mountpoint != "/System/Volumes/Data":
		return "macOS system volume (shares space with /)"
	}
	return ""
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
	now := time.Now()
	seen := map[string]bool{}
	var out []Disk
	withDiskHistory(func(hist map[string]diskHistoryEntry) {
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
	})
	return out
}
