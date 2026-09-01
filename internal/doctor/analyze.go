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
// "not measured" and are simply skipped by the rules. The JSON shape is a
// frozen contract — see SchemaVersion.
type Snapshot struct {
	CPU     CPU        `json:"cpu"`
	Memory  Memory     `json:"memory"`
	Disks   []Disk     `json:"disks"`
	GPUs    []GPU      `json:"gpus"`
	LLM     []LLMModel `json:"llm"`
	Thermal Thermal    `json:"thermal"`
	Net     []NetIface `json:"net"`
	Power   Power      `json:"power"`
}

// NetIface is one network interface's throughput over the sample window.
type NetIface struct {
	Name          string  `json:"name"`
	RxBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TxBytesPerSec float64 `json:"tx_bytes_per_sec"`
	LinkSpeedbps  float64 `json:"link_speed_bps"` // 0 if unknown
	RetransPct    float64 `json:"retransmit_percent"`
}

// Power is battery / AC state.
type Power struct {
	OnBattery       bool    `json:"on_battery"`
	Percent         float64 `json:"percent"`
	MinutesLeft     int     `json:"minutes_left"`             // OS estimate, 0 if unknown
	DesignCapacityF float64 `json:"design_capacity_fraction"` // full-charge / design, 0 if unknown
	ChargeRateW     float64 `json:"charge_rate_watts"`        // negative = discharging
	LowPowerMode    bool    `json:"low_power_mode"`           // macOS only; always false elsewhere
}

type CPU struct {
	Cores      int       `json:"cores"`
	UsedPct    float64   `json:"used_percent"`
	IOWaitPct  float64   `json:"iowait_percent"`
	StealPct   float64   `json:"steal_percent"`
	Load1      float64   `json:"load1"`
	FreqMHz    float64   `json:"freq_mhz"`
	BaseMHz    float64   `json:"base_mhz"`
	PerCorePct []float64 `json:"per_core_percent"`
	TopProc    ProcRef   `json:"top_process"`
}

type Memory struct {
	UsedPct       float64 `json:"used_percent"`
	AvailablePct  float64 `json:"available_percent"`
	SwapUsedPct   float64 `json:"swap_used_percent"`
	SwapTotal     uint64  `json:"swap_total_bytes"`
	SwapInPerSec  float64 `json:"swap_in_bytes_per_sec"`
	SwapOutPerSec float64 `json:"swap_out_bytes_per_sec"`
	TopProc       ProcRef `json:"top_process"`
}

// ProcRef names the process behind a resource reading — the "who" that turns
// a percentage into something actionable. A zero value (empty Name) means no
// process could be attributed.
type ProcRef struct {
	PID      int32   `json:"pid"`
	Name     string  `json:"name"`
	CPUPct   float64 `json:"cpu_percent"`
	RSSBytes uint64  `json:"rss_bytes"`
}

type Disk struct {
	Mount             string  `json:"mount"`
	UsedPct           float64 `json:"used_percent"`
	FreeBytes         uint64  `json:"free_bytes"`
	GrowthBytesPerSec float64 `json:"growth_bytes_per_sec"`
	UtilPct           float64 `json:"util_percent"`
	AwaitMS           float64 `json:"await_ms"`
	IOPS              float64 `json:"iops"`
	InodesUsedPct     float64 `json:"inodes_used_percent"`
}

type GPU struct {
	Name         string  `json:"name"`
	VRAMUsed     uint64  `json:"vram_used_bytes"`
	VRAMTotal    uint64  `json:"vram_total_bytes"`
	UtilPct      float64 `json:"util_percent"`
	TempC        float64 `json:"temp_c"`
	ClockMHz     float64 `json:"clock_mhz"`
	BaseClockMHz float64 `json:"base_clock_mhz"`
}

type LLMModel struct {
	Name       string  `json:"name"`
	OffloadPct float64 `json:"gpu_offload_percent"`
	HostCPUPct float64 `json:"host_cpu_percent"`
}

type Thermal struct {
	CPUTempC   float64 `json:"cpu_temp_c"`
	Throttling bool    `json:"throttling"`
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
	topProc := procSuffix(m.TopProc, false)

	switch {
	case m.SwapUsedPct >= 50 && m.SwapOutPerSec > 0:
		r.Add(diag.Finding{
			Severity: diag.Critical,
			Title:    "Swap thrashing",
			Detail: fmt.Sprintf("swap %.0f%% full and paging out at %s/s — the machine is stalling on disk, not working%s",
				m.SwapUsedPct, ui.HumanBytes(int64(m.SwapOutPerSec)), topProc),
			Fixes: []string{
				quitFix(m.TopProc),
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
			Fixes: []string{quitFix(m.TopProc), "a reboot clears accumulated swap"},
		})
	}

	// AvailablePct (kernel's own "could allocate this without swapping" number)
	// is the honest read on pressure — UsedPct alone conflates committed memory
	// with cache the kernel will drop on demand.
	lowAvailable := m.AvailablePct > 0 && m.AvailablePct < 10

	switch {
	case m.UsedPct >= thresholds.RAMHighPercent && (swapPressure || lowAvailable):
		r.Add(diag.Finding{
			Severity: diag.Critical,
			Title:    "RAM exhausted",
			Detail: fmt.Sprintf("%.0f%% of physical RAM in use with only %.0f%% available without swapping%s",
				m.UsedPct, m.AvailablePct, topProc),
			Fixes: []string{quitFix(m.TopProc), "add RAM or swap headroom"},
		})
	case m.UsedPct >= thresholds.RAMHighPercent:
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "RAM usage high (likely reclaimable)",
			Detail: fmt.Sprintf("%.0f%% in use but %.0f%% is still available and no swap-out is happening — much of this is probably file cache the kernel will drop on demand",
				m.UsedPct, m.AvailablePct),
			Fixes: []string{"no action needed unless apps slow down", "confirm with `vitals memcheck`"},
		})
	case m.UsedPct >= thresholds.RAMWarnPercent:
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "RAM elevated",
			Detail:   fmt.Sprintf("%.0f%% of physical RAM in use%s", m.UsedPct, topProc),
			Fixes:    []string{"close idle apps before it becomes pressure"},
		})
	}
}

// procSuffix renders " — largest consumer: name (pid N), 1.2 GB" (or the CPU
// equivalent) for appending to a Detail line, empty when nothing was attributed.
func procSuffix(p ProcRef, byCPU bool) string {
	if p.Name == "" {
		return ""
	}
	if byCPU {
		return fmt.Sprintf(" — top consumer: %s (pid %d) at %.0f%% CPU", p.Name, p.PID, p.CPUPct)
	}
	return fmt.Sprintf(" — largest consumer: %s (pid %d), %s RSS", p.Name, p.PID, ui.HumanBytes(int64(p.RSSBytes)))
}

// quitFix names the actual top RAM consumer when known, falling back to the
// generic pointer at `vitals memhogs` otherwise.
func quitFix(p ProcRef) string {
	if p.Name == "" {
		return "free RAM with `vitals memhogs`"
	}
	return fmt.Sprintf("quit or restart %s (pid %d) — the largest consumer, or run `vitals memhogs` for the full list", p.Name, p.PID)
}

func analyzeCPU(r *diag.Report, s Snapshot) {
	c := s.CPU
	topProc := procSuffix(c.TopProc, true)

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

	if c.Cores > 0 && c.Load1 >= thresholds.CPUOversubscribeMult*float64(c.Cores) && c.IOWaitPct < 20 {
		fix := "reduce parallelism (lower -j / worker count)"
		if c.TopProc.Name != "" {
			fix = fmt.Sprintf("%s (pid %d) is the top consumer at %.0f%% CPU — reduce its parallelism or stop it", c.TopProc.Name, c.TopProc.PID, c.TopProc.CPUPct)
		}
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "CPU run queue is oversubscribed",
			Detail: fmt.Sprintf("load %.1f on %d cores — roughly %.1fx more runnable work than cores%s",
				c.Load1, c.Cores, c.Load1/float64(c.Cores), topProc),
			Fixes: []string{fix, "`vitals top` for the full process list"},
		})
	}

	if lo, hi, ok := coreSpread(c.PerCorePct); ok && hi >= 90 && c.UsedPct <= 60 {
		fix := "the workload isn't parallelized — more cores won't help"
		if c.TopProc.Name != "" {
			fix = fmt.Sprintf("%s (pid %d) is pinning a single core — it isn't parallelized, so more cores won't help", c.TopProc.Name, c.TopProc.PID)
		}
		r.Add(diag.Finding{
			Severity: diag.Warn,
			Title:    "Single-core bottleneck",
			Detail: fmt.Sprintf("one core is at %.0f%% while the busiest others sit near %.0f%%, yet overall CPU use is only %.0f%%%s",
				hi, lo, c.UsedPct, topProc),
			Fixes: []string{fix, "a faster single-thread clock helps here; adding cores does not"},
		})
	}
}

// coreSpread returns the second-busiest and busiest per-core percentages —
// "second-busiest" so one runaway core doesn't get compared against itself,
// which is what actually signals a single-thread bottleneck versus a
// generally busy machine. ok is false with fewer than 2 cores measured.
func coreSpread(cores []float64) (secondHi, hi float64, ok bool) {
	if len(cores) < 2 {
		return 0, 0, false
	}
	sorted := append([]float64(nil), cores...)
	sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
	return sorted[1], sorted[0], true
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
		if d.UsedPct >= thresholds.DiskWarnPercent {
			sev := diag.Warn
			if d.UsedPct >= thresholds.DiskCriticalPercent {
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
				Detail: fmt.Sprintf("%.0f%% utilised with %.0fms average latency (%.0f IOPS) — I/O is queueing",
					d.UtilPct, d.AwaitMS, d.IOPS),
				Fixes: []string{"identify the top reader/writer with `vitals top --sort mem`", "throttle or reschedule the heavy job"},
			})
		}
		if d.InodesUsedPct >= 90 {
			sev := diag.Warn
			if d.InodesUsedPct >= 97 {
				sev = diag.Critical
			}
			r.Add(diag.Finding{
				Severity: sev,
				Title:    fmt.Sprintf("Disk %s is running out of inodes", d.Mount),
				Detail: fmt.Sprintf("%.0f%% of inodes used — free space can look fine while new files still fail to create; usually huge counts of tiny files (node_modules, mail spools, cache trees)",
					d.InodesUsedPct),
				Fixes: []string{"find the directory with the most files, e.g. `find <dir> -xdev | wc -l` per subtree", "`vitals clean --dry-run` then apply"},
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
