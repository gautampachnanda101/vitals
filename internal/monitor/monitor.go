// Package monitor is a cross-platform "Activity Monitor" style snapshot: system
// CPU / memory / load, disk and network I/O counters, and the top processes
// ranked by CPU or memory. Pure Go via gopsutil, identical output on macOS,
// Linux and Windows.
package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"vitals/internal/ui"
)

// Options configures a snapshot.
type Options struct {
	Top      int           // processes to show
	SortBy   string        // "cpu" or "mem"
	Watch    bool          // loop until interrupted
	Interval time.Duration // sample window / refresh period
	JSON     bool          // machine-readable output
}

type Snapshot struct {
	Timestamp time.Time  `json:"timestamp"`
	Host      HostInfo   `json:"host"`
	CPU       CPUInfo    `json:"cpu"`
	Memory    MemInfo    `json:"memory"`
	Swap      MemInfo    `json:"swap"`
	DiskIO    []IORate   `json:"disk_io"`
	NetIO     []IORate   `json:"net_io"`
	Processes []ProcInfo `json:"processes"`
}

type HostInfo struct {
	Hostname string  `json:"hostname"`
	OS       string  `json:"os"`
	Kernel   string  `json:"kernel_arch"`
	Uptime   uint64  `json:"uptime_seconds"`
	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
	Procs    int     `json:"process_count"`
}

type CPUInfo struct {
	Cores   int     `json:"logical_cores"`
	UsedPct float64 `json:"used_percent"`
}

type MemInfo struct {
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	UsedPct    float64 `json:"used_percent"`
}

// IOStat is one cumulative counter reading for a device or interface.
type IOStat struct {
	Name       string `json:"name"`
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
}

// IORate is throughput for a device or interface over the sample window,
// carrying both the current cumulative counters and the derived per-second rate.
type IORate struct {
	Name        string  `json:"name"`
	ReadBytes   uint64  `json:"read_bytes"`
	WriteBytes  uint64  `json:"write_bytes"`
	ReadPerSec  float64 `json:"read_bytes_per_sec"`
	WritePerSec float64 `json:"write_bytes_per_sec"`
}

// ioDelta computes per-second rates between two cumulative readings. A device
// absent from prev is reported first-seen with a zero rate; a counter reset
// (curr < prev) clamps to zero rather than going negative; a non-positive
// interval yields zero rates. The result is sorted by total throughput, busiest
// first.
func ioDelta(prev, curr []IOStat, dt time.Duration) []IORate {
	idx := make(map[string]IOStat, len(prev))
	for _, p := range prev {
		idx[p.Name] = p
	}
	secs := dt.Seconds()
	out := make([]IORate, 0, len(curr))
	for _, c := range curr {
		r := IORate{Name: c.Name, ReadBytes: c.ReadBytes, WriteBytes: c.WriteBytes}
		if p, ok := idx[c.Name]; ok && secs > 0 {
			if c.ReadBytes >= p.ReadBytes {
				r.ReadPerSec = float64(c.ReadBytes-p.ReadBytes) / secs
			}
			if c.WriteBytes >= p.WriteBytes {
				r.WritePerSec = float64(c.WriteBytes-p.WriteBytes) / secs
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReadPerSec+out[i].WritePerSec > out[j].ReadPerSec+out[j].WritePerSec
	})
	return out
}

type ProcInfo struct {
	PID      int32   `json:"pid"`
	User     string  `json:"user"`
	CPUPct   float64 `json:"cpu_percent"`
	MemPct   float32 `json:"mem_percent"`
	RSSBytes uint64  `json:"rss_bytes"`
	Threads  int32   `json:"threads"`
	Name     string  `json:"name"`
	Command  string  `json:"command"`
}

// Run produces one snapshot, or loops when Watch is set.
func Run(opts Options) error {
	if opts.Top <= 0 {
		opts.Top = 15
	}
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	if opts.SortBy != "mem" {
		opts.SortBy = "cpu"
	}

	if !opts.Watch {
		snap, err := sample(opts)
		if err != nil {
			return err
		}
		return emit(snap, opts)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for {
		snap, err := sample(opts)
		if err != nil {
			ui.Errf("%v", err)
		} else {
			if !opts.JSON {
				fmt.Print("\033[H\033[2J")
			}
			_ = emit(snap, opts)
		}
		select {
		case <-ctx.Done():
			fmt.Println()
			return nil
		case <-time.After(opts.Interval):
		}
	}
}

func sample(opts Options) (Snapshot, error) {
	s := Snapshot{Timestamp: time.Now()}

	if hi, err := host.Info(); err == nil {
		s.Host.Hostname = hi.Hostname
		s.Host.OS = fmt.Sprintf("%s %s", hi.Platform, hi.PlatformVersion)
		s.Host.Kernel = hi.KernelArch
		s.Host.Uptime = hi.Uptime
		s.Host.Procs = int(hi.Procs)
	}
	if la, err := load.Avg(); err == nil {
		s.Host.Load1, s.Host.Load5, s.Host.Load15 = la.Load1, la.Load5, la.Load15
	}

	// Take a first reading of the cumulative I/O counters, then let the sample
	// window elapse (CPU percent blocks for it), then read again and derive
	// per-second rates. Cumulative counters on their own tell you nothing
	// actionable about what the machine is doing right now.
	win := opts.sampleWindow()
	disk0 := readDiskCounters()
	net0 := readNetCounters()
	start := time.Now()

	s.CPU.Cores, _ = cpu.Counts(true)
	if pct, err := cpu.Percent(win, false); err == nil && len(pct) > 0 {
		s.CPU.UsedPct = pct[0]
	} else {
		time.Sleep(win) // still let the I/O window elapse
	}
	dt := time.Since(start)

	if vm, err := mem.VirtualMemory(); err == nil {
		s.Memory = MemInfo{vm.Total, vm.Used, vm.UsedPercent}
	}
	if sw, err := mem.SwapMemory(); err == nil {
		s.Swap = MemInfo{sw.Total, sw.Used, sw.UsedPercent}
	}

	s.DiskIO = ioDelta(disk0, readDiskCounters(), dt)
	s.NetIO = ioDelta(net0, readNetCounters(), dt)

	s.Processes = topProcesses(opts)
	return s, nil
}

// readDiskCounters returns a cumulative I/O reading per physical device.
func readDiskCounters() []IOStat {
	io, err := disk.IOCounters()
	if err != nil {
		return nil
	}
	out := make([]IOStat, 0, len(io))
	for name, c := range io {
		out = append(out, IOStat{Name: name, ReadBytes: c.ReadBytes, WriteBytes: c.WriteBytes})
	}
	return out
}

// readNetCounters returns a cumulative I/O reading per interface, skipping
// interfaces that have never carried a byte.
func readNetCounters() []IOStat {
	io, err := net.IOCounters(true)
	if err != nil {
		return nil
	}
	out := make([]IOStat, 0, len(io))
	for _, c := range io {
		if c.BytesRecv == 0 && c.BytesSent == 0 {
			continue
		}
		out = append(out, IOStat{Name: c.Name, ReadBytes: c.BytesRecv, WriteBytes: c.BytesSent})
	}
	return out
}

func (o Options) sampleWindow() time.Duration {
	if o.Interval < time.Second {
		return o.Interval
	}
	return 500 * time.Millisecond
}

func topProcesses(opts Options) []ProcInfo {
	ps, err := process.Processes()
	if err != nil {
		return nil
	}
	// Prime CPU counters, wait, then read — gopsutil needs two samples.
	for _, p := range ps {
		_, _ = p.Percent(0)
	}
	time.Sleep(opts.sampleWindow())

	out := make([]ProcInfo, 0, len(ps))
	for _, p := range ps {
		cpuPct, _ := p.Percent(0)
		mi, err := p.MemoryInfo()
		if err != nil || mi == nil {
			continue
		}
		memPct, _ := p.MemoryPercent()
		name, _ := p.Name()
		user, _ := p.Username()
		nthreads, _ := p.NumThreads()
		cmd, _ := p.Cmdline()
		if cmd == "" {
			cmd = name
		}
		out = append(out, ProcInfo{
			PID: p.Pid, User: user, CPUPct: cpuPct, MemPct: memPct,
			RSSBytes: mi.RSS, Threads: nthreads, Name: name, Command: cmd,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if opts.SortBy == "mem" {
			return out[i].RSSBytes > out[j].RSSBytes
		}
		return out[i].CPUPct > out[j].CPUPct
	})
	if len(out) > opts.Top {
		out = out[:opts.Top]
	}
	return out
}

// --- output ----------------------------------------------------------------

func emit(s Snapshot, opts Options) error {
	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}

	ui.Header("SYSTEM MONITOR")
	fmt.Printf("  %s   %s   %s   up %s\n",
		s.Timestamp.Format("15:04:05"), s.Host.Hostname, s.Host.OS,
		(time.Duration(s.Host.Uptime) * time.Second).Round(time.Minute))
	fmt.Printf("  CPU   %s  across %d cores    load %.2f / %.2f / %.2f    %d processes\n",
		bar(s.CPU.UsedPct), s.CPU.Cores, s.Host.Load1, s.Host.Load5, s.Host.Load15, s.Host.Procs)
	fmt.Printf("  RAM   %s  %s / %s\n",
		bar(s.Memory.UsedPct), ui.HumanBytes(int64(s.Memory.UsedBytes)), ui.HumanBytes(int64(s.Memory.TotalBytes)))
	if s.Swap.TotalBytes > 0 {
		fmt.Printf("  SWAP  %s  %s / %s\n",
			bar(s.Swap.UsedPct), ui.HumanBytes(int64(s.Swap.UsedBytes)), ui.HumanBytes(int64(s.Swap.TotalBytes)))
	}

	if len(s.DiskIO) > 0 {
		fmt.Printf("\n%sDisk I/O (per second)%s\n", ui.Bold, ui.Reset)
		for i, d := range s.DiskIO {
			if i >= 4 {
				break
			}
			fmt.Printf("  %-14s read %-13s write %-13s\n", d.Name,
				rate(d.ReadPerSec), rate(d.WritePerSec))
		}
	}
	if len(s.NetIO) > 0 {
		fmt.Printf("\n%sNetwork I/O (per second)%s\n", ui.Bold, ui.Reset)
		for i, n := range s.NetIO {
			if i >= 4 {
				break
			}
			fmt.Printf("  %-14s recv %-13s sent %-13s\n", n.Name,
				rate(n.ReadPerSec), rate(n.WritePerSec))
		}
	}

	fmt.Printf("\n%sTop %d processes by %s%s\n", ui.Bold, len(s.Processes), strings.ToUpper(opts.SortBy), ui.Reset)
	fmt.Printf("  %s\n", ui.Key(fmt.Sprintf("%-7s %-12s %-7s %-7s %-11s %-4s %s", "PID", "USER", "%CPU", "%MEM", "RSS", "THR", "COMMAND")))
	ui.Rule()
	for _, p := range s.Processes {
		// Pad inside the colour wrap so ANSI codes don't skew column widths.
		cpu := ui.Grade(fmt.Sprintf("%-7.1f", p.CPUPct), p.CPUPct, 60, 100)
		memc := ui.Grade(fmt.Sprintf("%-7.1f", p.MemPct), float64(p.MemPct), 10, 25)
		fmt.Printf("  %-7d %-12s %s %s %-11s %-4d %s\n",
			p.PID, ui.Truncate(p.User, 12), cpu, memc,
			ui.HumanBytes(int64(p.RSSBytes)), p.Threads, ui.Truncate(p.Name, 40))
	}
	return nil
}

// rate formats a bytes-per-second value, e.g. "1.50 MB/s".
func rate(bytesPerSec float64) string {
	if bytesPerSec < 0 {
		bytesPerSec = 0
	}
	return ui.HumanBytes(int64(bytesPerSec)) + "/s"
}

func bar(pct float64) string {
	const width = 20
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * width)
	color := ui.Green
	switch {
	case pct >= 85:
		color = ui.Red
	case pct >= 60:
		color = ui.Yellow
	}
	return fmt.Sprintf("%s%s%s%s %5.1f%%",
		color, strings.Repeat("█", filled), strings.Repeat("░", width-filled), ui.Reset, pct)
}
