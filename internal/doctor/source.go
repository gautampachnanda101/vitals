package doctor

import (
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/shirou/gopsutil/v4/sensors"

	"vitals/internal/gpu"
	"vitals/internal/llm"
)

// procSource is the subset of *process.Process's method set topProcs
// needs. procHandle adapts the real gopsutil type; tests substitute a
// fake with canned per-process data instead of scanning the real process
// table. Same pattern as internal/monitor's procSource/procHandle.
type procSource interface {
	PID() int32
	Percent(interval time.Duration) (float64, error)
	Name() (string, error)
	MemoryInfo() (*process.MemoryInfoStat, error)
}

// procHandle adapts a real *process.Process (whose PID is a struct field,
// not a method) to procSource.
type procHandle struct{ p *process.Process }

func (h procHandle) PID() int32                                   { return h.p.Pid }
func (h procHandle) Percent(d time.Duration) (float64, error)     { return h.p.Percent(d) }
func (h procHandle) Name() (string, error)                        { return h.p.Name() }
func (h procHandle) MemoryInfo() (*process.MemoryInfoStat, error) { return h.p.MemoryInfo() }

func realProcesses() ([]procSource, error) {
	ps, err := process.Processes()
	if err != nil {
		return nil, err
	}
	out := make([]procSource, len(ps))
	for i, p := range ps {
		out[i] = procHandle{p}
	}
	return out, nil
}

// source is the live gopsutil/subprocess surface Collect's named
// per-signal collectors (collectCPU/collectMemory/collectGPUs/
// collectThermal/collectLLM below) read from, pulled out so a test can
// substitute fakes — same shape as internal/monitor's source struct.
// defaultSource wires the real calls; production always goes through it
// via Collect.
//
// Deliberately scoped to the signals decomposed into named collectors so
// far (CPU, Memory, Net's raw counters, GPU, Thermal, LLM) — Disk and
// Power stay on their existing collectDisks/collectPower call sites for
// now: both are their own bigger subsystems within this package
// (per-mount filtering/cooldown state via markMountBad, OS-specific
// battery/pmset reads across three platforms) that deserve their own
// decomposition pass rather than being folded in here, per this item's
// own "don't improvise mid-implementation" ground rule. Tracked as this
// decomposition's own next step in docs/roadmap/items/
// 009-raw-coverage-95/implementation-plan.md.
type source struct {
	cpuTimes      func(percpu bool) ([]cpu.TimesStat, error)
	cpuCounts     func(logical bool) (int, error)
	cpuInfo       func() ([]cpu.InfoStat, error)
	loadAvg       func() (*load.AvgStat, error)
	processes     func() ([]procSource, error)
	virtualMemory func() (*mem.VirtualMemoryStat, error)
	swapMemory    func() (*mem.SwapMemoryStat, error)
	netIOCounters func(pernic bool) ([]gnet.IOCountersStat, error)
	gpuProbe      func() []gpu.Device
	sensorsTemps  func() ([]sensors.TemperatureStat, error)
	ollamaModels  func(ollamaURL string) []llm.ModelState
	scanProcesses func() []llm.ProcSnapshot
}

var defaultSource = source{
	cpuTimes:      cpu.Times,
	cpuCounts:     cpu.Counts,
	cpuInfo:       cpu.Info,
	loadAvg:       load.Avg,
	processes:     realProcesses,
	virtualMemory: mem.VirtualMemory,
	swapMemory:    mem.SwapMemory,
	netIOCounters: gnet.IOCounters,
	gpuProbe:      gpu.Probe,
	sensorsTemps:  sensors.SensorsTemperatures,
	ollamaModels:  llm.OllamaModels,
	scanProcesses: llm.ScanProcesses,
}
