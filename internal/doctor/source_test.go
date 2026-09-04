package doctor

import (
	"errors"
	"testing"
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

// fakeProc is a canned procSource for topProcs tests, mirroring
// internal/monitor's own fakeProc.
type fakeProc struct {
	pid    int32
	name   string
	cpuPct float64
	mem    *process.MemoryInfoStat
	memErr error
}

func (f fakeProc) PID() int32                                   { return f.pid }
func (f fakeProc) Percent(time.Duration) (float64, error)       { return f.cpuPct, nil }
func (f fakeProc) Name() (string, error)                        { return f.name, nil }
func (f fakeProc) MemoryInfo() (*process.MemoryInfoStat, error) { return f.mem, f.memErr }

// baseSource returns a source with every field wired to a harmless zero
// value, so a test only has to override the fields it cares about.
func baseSource() source {
	return source{
		cpuTimes:      func(bool) ([]cpu.TimesStat, error) { return nil, nil },
		cpuCounts:     func(bool) (int, error) { return 0, nil },
		cpuInfo:       func() ([]cpu.InfoStat, error) { return nil, nil },
		loadAvg:       func() (*load.AvgStat, error) { return nil, errors.New("no load") },
		processes:     func() ([]procSource, error) { return nil, nil },
		virtualMemory: func() (*mem.VirtualMemoryStat, error) { return nil, errors.New("no vm") },
		swapMemory:    func() (*mem.SwapMemoryStat, error) { return nil, errors.New("no swap") },
		netIOCounters: func(bool) ([]gnet.IOCountersStat, error) { return nil, nil },
		gpuProbe:      func() []gpu.Device { return nil },
		sensorsTemps:  func() ([]sensors.TemperatureStat, error) { return nil, errors.New("no sensors") },
		ollamaModels:  func(string) []llm.ModelState { return nil },
		scanProcesses: func() []llm.ProcSnapshot { return nil },
	}
}

func TestCollectCPUReadsCoresLoadFreqAndTopProc(t *testing.T) {
	src := baseSource()
	src.cpuCounts = func(logical bool) (int, error) {
		if !logical {
			t.Errorf("collectCPU should ask for logical core count")
		}
		return 8, nil
	}
	src.loadAvg = func() (*load.AvgStat, error) { return &load.AvgStat{Load1: 2.5}, nil }
	src.cpuInfo = func() ([]cpu.InfoStat, error) { return []cpu.InfoStat{{Mhz: 3200}}, nil }

	t0 := cpu.TimesStat{User: 100, Idle: 800}
	t1 := cpu.TimesStat{User: 140, Idle: 900}
	top := ProcRef{PID: 42, Name: "hog", CPUPct: 99}

	c := collectCPU(src, t0, t1, nil, nil, top)
	if c.Cores != 8 {
		t.Errorf("Cores = %d, want 8", c.Cores)
	}
	if c.Load1 != 2.5 {
		t.Errorf("Load1 = %v, want 2.5", c.Load1)
	}
	if c.FreqMHz != 3200 {
		t.Errorf("FreqMHz = %v, want 3200", c.FreqMHz)
	}
	if c.TopProc != top {
		t.Errorf("TopProc = %+v, want %+v", c.TopProc, top)
	}
}

func TestCollectCPUIgnoresBogusTinyFreqAndFailedLoad(t *testing.T) {
	// Apple silicon: gopsutil reports a near-zero Mhz that isn't real.
	src := baseSource()
	src.cpuInfo = func() ([]cpu.InfoStat, error) { return []cpu.InfoStat{{Mhz: 24}}, nil }

	c := collectCPU(src, cpu.TimesStat{}, cpu.TimesStat{}, nil, nil, ProcRef{})
	if c.FreqMHz != 0 {
		t.Errorf("FreqMHz = %v, want 0 (bogus tiny value should be dropped)", c.FreqMHz)
	}
	if c.Load1 != 0 {
		t.Errorf("Load1 = %v, want 0 when loadAvg fails", c.Load1)
	}
}

func TestCollectMemoryComputesUsedAvailableAndSwapRates(t *testing.T) {
	src := baseSource()
	src.virtualMemory = func() (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{UsedPercent: 55, Total: 1000, Available: 400}, nil
	}
	src.swapMemory = func() (*mem.SwapMemoryStat, error) {
		return &mem.SwapMemoryStat{UsedPercent: 10, Total: 2048}, nil
	}
	top := ProcRef{PID: 7, Name: "leaky", RSSBytes: 12345}

	m := collectMemory(src, swapIO{in: 100, out: 50}, swapIO{in: 300, out: 60}, 2*time.Second, top)
	if m.UsedPct != 55 {
		t.Errorf("UsedPct = %v, want 55", m.UsedPct)
	}
	if !approx(m.AvailablePct, 40) {
		t.Errorf("AvailablePct = %v, want 40", m.AvailablePct)
	}
	if m.SwapUsedPct != 10 || m.SwapTotal != 2048 {
		t.Errorf("swap = %+v", m)
	}
	if m.SwapInPerSec != 100 { // (300-100)/2s
		t.Errorf("SwapInPerSec = %v, want 100", m.SwapInPerSec)
	}
	if m.SwapOutPerSec != 5 { // (60-50)/2s
		t.Errorf("SwapOutPerSec = %v, want 5", m.SwapOutPerSec)
	}
	if m.TopProc != top {
		t.Errorf("TopProc = %+v, want %+v", m.TopProc, top)
	}
}

func TestCollectMemorySwapCounterResetYieldsNoRate(t *testing.T) {
	src := baseSource()
	src.virtualMemory = func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{Total: 0}, nil }
	src.swapMemory = func() (*mem.SwapMemoryStat, error) { return &mem.SwapMemoryStat{}, nil }

	m := collectMemory(src, swapIO{in: 500, out: 500}, swapIO{in: 10, out: 10}, time.Second, ProcRef{})
	if m.SwapInPerSec != 0 || m.SwapOutPerSec != 0 {
		t.Errorf("counter reset should yield zero rates, got %+v", m)
	}
	if m.AvailablePct != 0 {
		t.Errorf("AvailablePct = %v, want 0 when Total is 0", m.AvailablePct)
	}
}

func TestCollectMemoryZeroWindowYieldsNoSwapRate(t *testing.T) {
	src := baseSource()
	m := collectMemory(src, swapIO{}, swapIO{in: 10, out: 10}, 0, ProcRef{})
	if m.SwapInPerSec != 0 || m.SwapOutPerSec != 0 {
		t.Errorf("zero window should yield zero rates, got %+v", m)
	}
}

func TestCollectGPUsMapsProbeResultsAndIsEmptyWhenNone(t *testing.T) {
	src := baseSource()
	if got := collectGPUs(src); got != nil {
		t.Errorf("collectGPUs with no devices = %+v, want nil", got)
	}

	src.gpuProbe = func() []gpu.Device {
		return []gpu.Device{{
			Name: "Apple M3 Max", MemUsedB: 1 << 30, MemTotalB: 32 << 30,
			UtilPct: 42, TempC: 55, ClockMHz: 1200, ClockMaxMHz: 1500,
		}}
	}
	got := collectGPUs(src)
	if len(got) != 1 {
		t.Fatalf("want 1 GPU, got %d", len(got))
	}
	want := GPU{Name: "Apple M3 Max", VRAMUsed: 1 << 30, VRAMTotal: 32 << 30, UtilPct: 42, TempC: 55, ClockMHz: 1200, BaseClockMHz: 1500}
	if got[0] != want {
		t.Errorf("collectGPUs = %+v, want %+v", got[0], want)
	}
}

func TestCollectThermalReportsHottestSensor(t *testing.T) {
	src := baseSource()
	src.sensorsTemps = func() ([]sensors.TemperatureStat, error) {
		return []sensors.TemperatureStat{{Temperature: 45}, {Temperature: 72}, {Temperature: 60}}, nil
	}
	if got := collectThermal(src).CPUTempC; got != 72 {
		t.Errorf("CPUTempC = %v, want 72 (the hottest reading)", got)
	}
}

func TestCollectThermalErrorYieldsZero(t *testing.T) {
	src := baseSource() // sensorsTemps errors by default
	if got := collectThermal(src).CPUTempC; got != 0 {
		t.Errorf("CPUTempC = %v, want 0 when sensors fail", got)
	}
}

func TestCollectLLMCorrelatesOffloadWithOllamaHostCPU(t *testing.T) {
	src := baseSource()
	src.ollamaModels = func(url string) []llm.ModelState {
		if url != "http://x" {
			t.Errorf("ollamaModels called with %q, want http://x", url)
		}
		return []llm.ModelState{{Name: "llama3", GPUOffload: 80}}
	}
	src.scanProcesses = func() []llm.ProcSnapshot {
		return []llm.ProcSnapshot{
			{Runtime: "other", CPUPct: 99},
			{Runtime: "ollama", CPUPct: 12},
			{Runtime: "ollama", CPUPct: 30},
		}
	}
	got := collectLLM(src, Options{OllamaURL: "http://x"})
	if len(got) != 1 {
		t.Fatalf("want 1 model, got %+v", got)
	}
	want := LLMModel{Name: "llama3", OffloadPct: 80, HostCPUPct: 30}
	if got[0] != want {
		t.Errorf("collectLLM = %+v, want %+v", got[0], want)
	}
}

func TestCollectLLMNoModelsIsEmpty(t *testing.T) {
	src := baseSource()
	if got := collectLLM(src, Options{}); got != nil {
		t.Errorf("collectLLM with no models = %+v, want nil", got)
	}
}

func TestFirstTimesAndPercoreTimesWithSourceFailure(t *testing.T) {
	src := baseSource()
	src.cpuTimes = func(percpu bool) ([]cpu.TimesStat, error) { return nil, errors.New("boom") }
	if got := firstTimes(src); got != (cpu.TimesStat{}) {
		t.Errorf("firstTimes on error = %+v, want zero value", got)
	}
	if got := percoreTimes(src); got != nil {
		t.Errorf("percoreTimes on error = %+v, want nil", got)
	}
}

func TestFirstTimesAndPercoreTimesReadThroughSource(t *testing.T) {
	src := baseSource()
	src.cpuTimes = func(percpu bool) ([]cpu.TimesStat, error) {
		if percpu {
			return []cpu.TimesStat{{User: 1}, {User: 2}}, nil
		}
		return []cpu.TimesStat{{User: 99}}, nil
	}
	if got := firstTimes(src); got.User != 99 {
		t.Errorf("firstTimes = %+v, want User=99", got)
	}
	if got := percoreTimes(src); len(got) != 2 {
		t.Errorf("percoreTimes = %+v, want 2 cores", got)
	}
}

func TestSwapCountersWithSourceFailureAndSuccess(t *testing.T) {
	src := baseSource()
	if got := swapCounters(src); got != (swapIO{}) {
		t.Errorf("swapCounters on error = %+v, want zero value", got)
	}
	src.swapMemory = func() (*mem.SwapMemoryStat, error) { return &mem.SwapMemoryStat{Sin: 5, Sout: 6}, nil }
	if got := swapCounters(src); got != (swapIO{in: 5, out: 6}) {
		t.Errorf("swapCounters = %+v, want {5 6}", got)
	}
}

func TestTopProcsPicksHighestCPUAndHighestRSSIndependently(t *testing.T) {
	procs := []procSource{
		fakeProc{pid: 1, name: "alpha", cpuPct: 90, mem: &process.MemoryInfoStat{RSS: 100}},
		fakeProc{pid: 2, name: "beta", cpuPct: 10, mem: &process.MemoryInfoStat{RSS: 900}},
		fakeProc{pid: 3, name: "gamma", cpuPct: 50, mem: nil, memErr: errors.New("no access")},
	}
	topCPU, topMem := topProcs(procs)
	if topCPU.PID != 1 || topCPU.Name != "alpha" {
		t.Errorf("topCPU = %+v, want pid 1 (alpha)", topCPU)
	}
	if topMem.PID != 2 || topMem.RSSBytes != 900 {
		t.Errorf("topMem = %+v, want pid 2, RSS 900", topMem)
	}
}

func TestTopProcsEmptyYieldsZeroValues(t *testing.T) {
	topCPU, topMem := topProcs(nil)
	if topCPU != (ProcRef{}) || topMem != (ProcRef{}) {
		t.Errorf("topProcs(nil) = %+v / %+v, want zero values", topCPU, topMem)
	}
}

func TestNetCountersFiltersLoopbackAndIdleInterfaces(t *testing.T) {
	src := baseSource()
	src.netIOCounters = func(pernic bool) ([]gnet.IOCountersStat, error) {
		if !pernic {
			t.Error("netCounters should ask for per-interface counters")
		}
		return []gnet.IOCountersStat{
			{Name: "lo0", BytesRecv: 1000, BytesSent: 1000},
			{Name: "en0", BytesRecv: 0, BytesSent: 0},
			{Name: "en1", BytesRecv: 500, BytesSent: 200},
		}, nil
	}
	got := netCounters(src)
	if len(got) != 1 || got[0].name != "en1" {
		t.Errorf("netCounters = %+v, want only en1", got)
	}
}

func TestNetCountersSourceFailureYieldsNil(t *testing.T) {
	src := baseSource()
	src.netIOCounters = func(bool) ([]gnet.IOCountersStat, error) { return nil, errors.New("boom") }
	if got := netCounters(src); got != nil {
		t.Errorf("netCounters on error = %+v, want nil", got)
	}
}

func TestCollectRunsEndToEndOverAFakeSource(t *testing.T) {
	src := baseSource()
	src.cpuTimes = func(percpu bool) ([]cpu.TimesStat, error) {
		if percpu {
			return []cpu.TimesStat{{User: 1, Idle: 10}}, nil
		}
		return []cpu.TimesStat{{User: 1, Idle: 10}}, nil
	}
	src.processes = func() ([]procSource, error) {
		return []procSource{fakeProc{pid: 9, name: "solo", cpuPct: 5, mem: &process.MemoryInfoStat{RSS: 42}}}, nil
	}
	src.gpuProbe = func() []gpu.Device { return []gpu.Device{{Name: "fake-gpu"}} }

	s := collect(src, Options{Window: time.Millisecond})
	if len(s.GPUs) != 1 || s.GPUs[0].Name != "fake-gpu" {
		t.Errorf("Snapshot.GPUs = %+v, want 1 fake-gpu entry", s.GPUs)
	}
	if s.CPU.TopProc.PID != 9 {
		t.Errorf("Snapshot.CPU.TopProc = %+v, want pid 9", s.CPU.TopProc)
	}
	if s.Memory.TopProc.RSSBytes != 42 {
		t.Errorf("Snapshot.Memory.TopProc = %+v, want RSS 42", s.Memory.TopProc)
	}
}

func TestRealProcessesReturnsSomeOfTheLiveProcessTable(t *testing.T) {
	// One real end-to-end call through the actual process table, matching
	// this package's dns/netpeers style of exercising the live wiring
	// once without asserting on machine-specific content.
	procs, err := realProcesses()
	if err != nil {
		t.Fatalf("realProcesses: %v", err)
	}
	if len(procs) == 0 {
		t.Skip("no processes visible (unusual, but not this test's concern)")
	}
	if procs[0].PID() == 0 {
		t.Error("expected a real, nonzero PID from the live process table")
	}
}

func TestDefaultSourceFieldsAreAllWired(t *testing.T) {
	// A cheap guard against a field silently left nil after a future
	// rebase/merge — every field should be callable without panicking.
	if defaultSource.cpuTimes == nil || defaultSource.cpuCounts == nil || defaultSource.cpuInfo == nil ||
		defaultSource.loadAvg == nil || defaultSource.processes == nil || defaultSource.virtualMemory == nil ||
		defaultSource.swapMemory == nil || defaultSource.netIOCounters == nil || defaultSource.gpuProbe == nil ||
		defaultSource.sensorsTemps == nil || defaultSource.ollamaModels == nil || defaultSource.scanProcesses == nil {
		t.Error("defaultSource has a nil field")
	}
}
