// Package doctor correlates readings from every resource into a single ranked
// verdict: which one thing is constraining the machine right now, and the exact
// command to fix it. Analyze is a pure function over a Snapshot so the whole
// engine is exercised from fixtures with no live system.
package doctor

import (
	"fmt"
	"sort"

	"vitals/internal/diag"
	"vitals/internal/ui"
)

// Snapshot is the cross-resource state Analyze reasons over. Zero values mean
// "not measured" and are simply skipped by the rules.
type Snapshot struct {
	CPU     CPU
	Memory  Memory
	Disks   []Disk
	GPUs    []GPU
	LLM     []LLMModel
	Thermal Thermal
	Net     []NetIface
	Power   Power
}

// NetIface is one network interface's throughput over the sample window.
type NetIface struct {
	Name          string
	RxBytesPerSec float64
	TxBytesPerSec float64
	LinkSpeedbps  float64 // 0 if unknown
	RetransPct    float64 // TCP retransmit rate, 0 if unknown
}

// Power is battery / AC state.
type Power struct {
	OnBattery       bool
	Percent         float64
	MinutesLeft     int     // OS estimate, 0 if unknown
	DesignCapacityF float64 // current full-charge / design capacity, 0 if unknown
	ChargeRateW     float64 // negative = discharging
}

type CPU struct {
	Cores     int
	UsedPct   float64
	IOWaitPct float64
	StealPct  float64
	Load1     float64
	FreqMHz   float64
	BaseMHz   float64
}

type Memory struct {
	UsedPct       float64
	SwapUsedPct   float64
	SwapTotal     uint64
	SwapInPerSec  float64
	SwapOutPerSec float64
}

type Disk struct {
	Mount             string
	UsedPct           float64
	FreeBytes         uint64
	GrowthBytesPerSec float64
	UtilPct           float64
	AwaitMS           float64
}

type GPU struct {
	Name         string
	VRAMUsed     uint64
	VRAMTotal    uint64
	UtilPct      float64
	TempC        float64
	ClockMHz     float64
	BaseClockMHz float64
}

type LLMModel struct {
	Name       string
	OffloadPct float64 // size_vram / size * 100
	HostCPUPct float64 // CPU% of the runtime process
}

type Thermal struct {
	CPUTempC   float64
	Throttling bool
}

// Analyze runs every correlation rule and returns the findings, most severe
// first. When nothing fires it returns a single OK finding.
func Analyze(s Snapshot) diag.Report {
	var r diag.Report

	analyzeMemory(&r, s)
	analyzeCPU(&r, s)
	analyzeThermal(&r, s)
	analyzeDisks(&r, s)
	analyzeGPUs(&r, s)
	analyzeLLM(&r, s)
	analyzeNet(&r, s)
	analyzePower(&r, s)

	if len(r.Findings) == 0 {
		r.Add(diag.Finding{Severity: diag.OK, Title: "No bottleneck detected",
			Detail: "CPU, memory, disk and any LLM runtimes are all within healthy limits"})
	}

	sort.SliceStable(r.Findings, func(i, j int) bool {
		return r.Findings[i].Severity > r.Findings[j].Severity
	})
	return r
}

// AnalyzeResource runs only the rules for one resource ("cpu", "mem", "disk",
// "gpu", "net", "power"), most-severe first. Used by the deep-dive commands.
func AnalyzeResource(s Snapshot, resource string) diag.Report {
	var r diag.Report
	switch resource {
	case "cpu":
		analyzeCPU(&r, s)
		analyzeThermal(&r, s)
	case "mem", "memory":
		analyzeMemory(&r, s)
	case "disk":
		analyzeDisks(&r, s)
	case "gpu":
		analyzeGPUs(&r, s)
	case "net", "network":
		analyzeNet(&r, s)
	case "power", "battery":
		analyzePower(&r, s)
	}
	sort.SliceStable(r.Findings, func(i, j int) bool {
		return r.Findings[i].Severity > r.Findings[j].Severity
	})
	return r
}

func analyzeMemory(r *diag.Report, s Snapshot) {
	m := s.Memory
	swapPressure := m.SwapOutPerSec > 0 || m.SwapUsedPct >= 50

	switch {
	case m.SwapUsedPct >= 50 && m.SwapOutPerSec > 0:
		r.Add(diag.Finding{
			Severity: diag.Critical,
			Title:    "Swap thrashing",
			Detail: fmt.Sprintf("swap %.0f%% full and paging out at %s/s — the machine is stalling on disk, not working",
				m.SwapUsedPct, ui.HumanBytes(int64(m.SwapOutPerSec))),
			Fixes: []string{
				"free RAM now: `vitals memhogs` then quit the top consumers",
				"macOS: `sudo purge`",
				"reboot if swap stays pinned after freeing RAM",
			},
		})
	case m.SwapUsedPct >= 60:
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "Swap heavily used",
			Detail: fmt.Sprintf("swap %.0f%% full but not actively paging — little headroom left before it starts to stall",
				m.SwapUsedPct),
			Fixes: []string{"free RAM with `vitals memhogs`", "a reboot clears accumulated swap"},
		})
	}

	switch {
	case m.UsedPct >= 90 && swapPressure:
		r.Add(diag.Finding{
			Severity: diag.Critical,
			Title:    "RAM exhausted",
			Detail:   fmt.Sprintf("%.0f%% of physical RAM in use with active swap pressure", m.UsedPct),
			Fixes:    []string{"quit the biggest apps (`vitals memhogs`)", "add RAM or swap headroom"},
		})
	case m.UsedPct >= 90:
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "RAM usage high (likely reclaimable)",
			Detail: fmt.Sprintf("%.0f%% in use but no swap-out is happening — much of this is probably file cache the kernel will drop on demand",
				m.UsedPct),
			Fixes: []string{"no action needed unless apps slow down", "confirm with `vitals memcheck`"},
		})
	case m.UsedPct >= 78:
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "RAM elevated",
			Detail:   fmt.Sprintf("%.0f%% of physical RAM in use", m.UsedPct),
			Fixes:    []string{"close idle apps before it becomes pressure"},
		})
	}
}

func analyzeCPU(r *diag.Report, s Snapshot) {
	c := s.CPU

	if c.UsedPct >= 80 && c.IOWaitPct >= 20 {
		sev := diag.Warn
		if c.IOWaitPct >= 35 {
			sev = diag.Critical
		}
		fix := "find the busy disk with `vitals disk` and the process driving it"
		if d, ok := busiestDisk(s.Disks); ok {
			fix = fmt.Sprintf("disk %s is the likely culprit (util %.0f%%, await %.0fms) — find its top writer", d.Mount, d.UtilPct, d.AwaitMS)
		}
		r.Add(diag.Finding{
			Severity: sev,
			Title:    "CPU looks busy but it is mostly I/O wait",
			Detail: fmt.Sprintf("CPU %.0f%% used, of which %.0f points is waiting on disk — the bottleneck is storage, not compute",
				c.UsedPct, c.IOWaitPct),
			Fixes: []string{fix, "moving hot data to a faster disk will help; a faster CPU will not"},
		})
	}

	if c.StealPct >= 10 {
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "CPU time is being stolen by the hypervisor",
			Detail:   fmt.Sprintf("%.0f%% steal — a noisy neighbour on the host is taking cycles from this VM", c.StealPct),
			Fixes:    []string{"resize to a dedicated-CPU instance", "raise it with your provider if it persists"},
		})
	}

	if c.Cores > 0 && c.Load1 >= float64(2*c.Cores) && c.IOWaitPct < 20 {
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "CPU run queue is oversubscribed",
			Detail: fmt.Sprintf("load %.1f on %d cores — roughly %.1fx more runnable work than cores",
				c.Load1, c.Cores, c.Load1/float64(c.Cores)),
			Fixes: []string{"reduce parallelism (lower -j / worker count)", "`vitals cpu` for the top consumers"},
		})
	}
}

func analyzeThermal(r *diag.Report, s Snapshot) {
	t := s.Thermal
	c := s.CPU
	freqCapped := c.BaseMHz > 0 && c.FreqMHz > 0 && c.FreqMHz < 0.9*c.BaseMHz && t.CPUTempC >= 85
	if !t.Throttling && !freqCapped {
		return
	}
	detail := "CPU is thermally throttling — sustained performance is capped below its rated clock"
	if c.BaseMHz > 0 && c.FreqMHz > 0 {
		detail = fmt.Sprintf("CPU clocked to %.0f MHz against a %.0f MHz base at %.0f°C — sustained performance is capped ~%.0f%%",
			c.FreqMHz, c.BaseMHz, t.CPUTempC, (1-c.FreqMHz/c.BaseMHz)*100)
	}
	r.Add(diag.Finding{
		Severity: diag.Warn,
		Title:    "CPU throttling on heat",
		Detail:   detail,
		Fixes:    []string{"improve airflow / clear dust", "reduce sustained parallel load", "lower the ambient temperature"},
	})
}

func analyzeDisks(r *diag.Report, s Snapshot) {
	for _, d := range s.Disks {
		if d.UsedPct >= 90 {
			sev := diag.Warn
			if d.UsedPct >= 97 {
				sev = diag.Critical
			}
			detail := fmt.Sprintf("%s is %.0f%% full (%s free)", d.Mount, d.UsedPct, ui.HumanBytes(int64(d.FreeBytes)))
			if d.GrowthBytesPerSec > 0 {
				secs := float64(d.FreeBytes) / d.GrowthBytesPerSec
				detail += " and filling — " + timeToFull(secs)
			}
			r.Add(diag.Finding{
				Severity: sev,
				Title:    fmt.Sprintf("Disk %s nearly full", d.Mount),
				Detail:   detail,
				Fixes:    []string{"`vitals clean --dry-run` then apply", "explore the biggest dirs with ncdu / gdu"},
			})
		}
		if d.UtilPct >= 90 && d.AwaitMS >= 20 {
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("Disk %s is saturated", d.Mount),
				Detail: fmt.Sprintf("%.0f%% utilised with %.0fms average latency — I/O is queueing",
					d.UtilPct, d.AwaitMS),
				Fixes: []string{"identify the top reader/writer with `vitals disk`", "throttle or reschedule the heavy job"},
			})
		}
	}
}

func analyzeGPUs(r *diag.Report, s Snapshot) {
	for _, g := range s.GPUs {
		if g.VRAMTotal == 0 {
			continue
		}
		usedPct := float64(g.VRAMUsed) / float64(g.VRAMTotal) * 100
		if usedPct >= 95 {
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("GPU %s VRAM is full", nz(g.Name)),
				Detail: fmt.Sprintf("%s of %s VRAM in use (%.0f%%) — further allocations will spill to system RAM",
					ui.HumanBytes(int64(g.VRAMUsed)), ui.HumanBytes(int64(g.VRAMTotal)), usedPct),
				Fixes: []string{"unload an idle model", "use a smaller quant", "`nvidia-smi` to see what holds VRAM"},
			})
		}
		if g.BaseClockMHz > 0 && g.ClockMHz > 0 && g.ClockMHz < 0.9*g.BaseClockMHz && g.TempC >= 80 {
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("GPU %s throttling on heat", nz(g.Name)),
				Detail:   fmt.Sprintf("%.0f MHz against a %.0f MHz base at %.0f°C", g.ClockMHz, g.BaseClockMHz, g.TempC),
				Fixes:    []string{"improve case airflow", "lower the power limit for steadier clocks"},
			})
		}
	}
}

func analyzeLLM(r *diag.Report, s Snapshot) {
	for _, m := range s.LLM {
		switch {
		case m.OffloadPct < 5:
			detail := fmt.Sprintf("model %s is running entirely on the CPU (0%% offloaded to GPU)", m.Name)
			if m.HostCPUPct >= 100 {
				detail += fmt.Sprintf("; the runtime is pinned at %.0f%% CPU waiting on memory bandwidth", m.HostCPUPct)
			}
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("LLM %s is on CPU only", m.Name),
				Detail:   detail,
				Fixes: []string{
					"check the runtime actually has GPU access (CUDA / --gpus / Metal)",
					"pick a model that fits VRAM, or a smaller quant",
				},
			})
		case m.OffloadPct <= 95:
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("LLM %s is only %.0f%% offloaded", m.Name, m.OffloadPct),
				Detail:   "the CPU-GPU layer split forces context shuffling every token, which sharply slows generation",
				Fixes: []string{
					"drop to a smaller quant (e.g. Q8_0 -> Q4_K_M)",
					"lower the layer/`num_gpu` count so it fits fully",
					"unload other models competing for VRAM",
				},
			})
		}
	}
}

func analyzeNet(r *diag.Report, s Snapshot) {
	for _, n := range s.Net {
		if n.LinkSpeedbps > 0 {
			usedbps := (n.RxBytesPerSec + n.TxBytesPerSec) * 8
			if pct := usedbps / n.LinkSpeedbps * 100; pct >= 85 {
				r.Add(diag.Finding{
					Severity: diag.Warn,
					Title:    fmt.Sprintf("%s near link capacity", n.Name),
					Detail: fmt.Sprintf("%.0f%% of the %.0f Mbps link in use (%s/s rx, %s/s tx)",
						pct, n.LinkSpeedbps/1e6, ui.HumanBytes(int64(n.RxBytesPerSec)), ui.HumanBytes(int64(n.TxBytesPerSec))),
					Fixes: []string{"find the talker with `vitals net`", "throttle the large transfer"},
				})
			}
		}
		if n.RetransPct >= 5 {
			r.Add(diag.Finding{
				Severity: diag.Warn,
				Title:    fmt.Sprintf("%s is losing packets", n.Name),
				Detail:   fmt.Sprintf("TCP retransmit rate %.1f%% — a lossy or congested link, not a bandwidth ceiling", n.RetransPct),
				Fixes:    []string{"check cabling / Wi-Fi signal", "test against another host to isolate it"},
			})
		}
	}
}

func analyzePower(r *diag.Report, s Snapshot) {
	p := s.Power
	if p.OnBattery && p.Percent > 0 && p.Percent <= 20 {
		sev := diag.Warn
		if p.Percent <= 8 {
			sev = diag.Critical
		}
		detail := fmt.Sprintf("battery at %.0f%% on battery power", p.Percent)
		if p.MinutesLeft > 0 {
			detail += fmt.Sprintf(", ~%d min left at the current draw", p.MinutesLeft)
		}
		r.Add(diag.Finding{
			Severity: sev, Title: "Battery low", Detail: detail,
			Fixes: []string{"connect a charger", "quit high-energy apps (`vitals power`)"},
		})
	}
	if !p.OnBattery && p.ChargeRateW < 0 {
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "Draining while plugged in",
			Detail:   "the charger cannot keep up with the current load — battery is discharging on AC power",
			Fixes:    []string{"use a higher-wattage charger", "reduce sustained CPU/GPU load"},
		})
	}
	if p.DesignCapacityF > 0 && p.DesignCapacityF < 0.8 {
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "Battery health degraded",
			Detail:   fmt.Sprintf("full charge holds %.0f%% of design capacity — runtime estimates will be optimistic", p.DesignCapacityF*100),
			Fixes:    []string{"plan for a battery replacement", "keep a charger handy for long sessions"},
		})
	}
}

// --- helpers ---------------------------------------------------------------

func busiestDisk(ds []Disk) (Disk, bool) {
	var best Disk
	found := false
	for _, d := range ds {
		if !found || d.UtilPct+d.AwaitMS > best.UtilPct+best.AwaitMS {
			best, found = d, true
		}
	}
	return best, found
}

func timeToFull(secs float64) string {
	switch {
	case secs < 3600:
		return fmt.Sprintf("about %.0f minutes to full at the current rate", secs/60)
	case secs < 48*3600:
		return fmt.Sprintf("about %.0f hours to full at the current rate", secs/3600)
	default:
		return fmt.Sprintf("about %.0f days to full at the current rate", secs/86400)
	}
}

func nz(s string) string {
	if s == "" {
		return "(unnamed)"
	}
	return s
}
