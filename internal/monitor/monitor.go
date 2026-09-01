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
	DiskIO    []IOStat   `json:"disk_io"`
	NetIO     []IOStat   `json:"net_io"`
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

type IOStat struct {
	Name       string `json:"name"`
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
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

	s.CPU.Cores, _ = cpu.Counts(true)
	if pct, err := cpu.Percent(opts.sampleWindow(), false); err == nil && len(pct) > 0 {
		s.CPU.UsedPct = pct[0]
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		s.Memory = MemInfo{vm.Total, vm.Used, vm.UsedPercent}
	}
	if sw, err := mem.SwapMemory(); err == nil {
		s.Swap = MemInfo{sw.Total, sw.Used, sw.UsedPercent}
	}

	if io, err := disk.IOCounters(); err == nil {
		for name, c := range io {
			s.DiskIO = append(s.DiskIO, IOStat{name, c.ReadBytes, c.WriteBytes})
		}
		sort.Slice(s.DiskIO, func(i, j int) bool {
			return s.DiskIO[i].ReadBytes+s.DiskIO[i].WriteBytes > s.DiskIO[j].ReadBytes+s.DiskIO[j].WriteBytes
		})
	}
	if io, err := net.IOCounters(true); err == nil {
		for _, c := range io {
			if c.BytesRecv == 0 && c.BytesSent == 0 {
				continue
			}
			s.NetIO = append(s.NetIO, IOStat{c.Name, c.BytesRecv, c.BytesSent})
		}
		sort.Slice(s.NetIO, func(i, j int) bool {
			return s.NetIO[i].ReadBytes+s.NetIO[i].WriteBytes > s.NetIO[j].ReadBytes+s.NetIO[j].WriteBytes
		})
	}

	s.Processes = topProcesses(opts)
	return s, nil
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
		fmt.Printf("\n%sDisk I/O (cumulative)%s\n", ui.Bold, ui.Reset)
		for i, d := range s.DiskIO {
			if i >= 4 {
				break
			}
			fmt.Printf("  %-14s read %-11s write %-11s\n", d.Name,
				ui.HumanBytes(int64(d.ReadBytes)), ui.HumanBytes(int64(d.WriteBytes)))
		}
	}
	if len(s.NetIO) > 0 {
		fmt.Printf("\n%sNetwork I/O (cumulative)%s\n", ui.Bold, ui.Reset)
		for i, n := range s.NetIO {
			if i >= 4 {
				break
			}
			fmt.Printf("  %-14s recv %-11s sent %-11s\n", n.Name,
				ui.HumanBytes(int64(n.ReadBytes)), ui.HumanBytes(int64(n.WriteBytes)))
		}
	}

	fmt.Printf("\n%sTop %d processes by %s%s\n", ui.Bold, len(s.Processes), strings.ToUpper(opts.SortBy), ui.Reset)
	fmt.Printf("  %-7s %-12s %-7s %-7s %-11s %-4s %s\n", "PID", "USER", "%CPU", "%MEM", "RSS", "THR", "COMMAND")
	ui.Rule()
	for _, p := range s.Processes {
		fmt.Printf("  %-7d %-12s %-7.1f %-7.1f %-11s %-4d %s\n",
			p.PID, ui.Truncate(p.User, 12), p.CPUPct, p.MemPct,
			ui.HumanBytes(int64(p.RSSBytes)), p.Threads, ui.Truncate(p.Name, 40))
	}
	return nil
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
