package memhogs

import (
	"context"
	"os"
	"os/signal"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// procSource is the subset of *process.Process once() needs. procHandle makes
// the real gopsutil type satisfy it; tests substitute a fake with canned
// per-process data instead of scanning the real process table. Same pattern as
// internal/monitor's procSource.
type procSource interface {
	PID() int32
	MemoryInfo() (*process.MemoryInfoStat, error)
	Name() (string, error)
	Cmdline() (string, error)
	Exe() (string, error)
}

// procHandle adapts a real *process.Process (whose PID is a struct field, not
// a method) to procSource.
type procHandle struct{ p *process.Process }

func (h procHandle) PID() int32                                   { return h.p.Pid }
func (h procHandle) MemoryInfo() (*process.MemoryInfoStat, error) { return h.p.MemoryInfo() }
func (h procHandle) Name() (string, error)                        { return h.p.Name() }
func (h procHandle) Cmdline() (string, error)                     { return h.p.Cmdline() }
func (h procHandle) Exe() (string, error)                         { return h.p.Exe() }

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

// source is the live gopsutil/OS surface Run reads from, pulled out so a test
// can substitute fakes and exercise once()/watch()'s bucketing, formatting and
// branching without touching the real host. defaultSource wires the real
// calls; production always goes through it via Run.
type source struct {
	processes        func() ([]procSource, error)
	readCgroup       func(pid int32) string
	virtualMemory    func() (*mem.VirtualMemoryStat, error)
	swapMemory       func() (*mem.SwapMemoryStat, error)
	newSignalContext func() (context.Context, context.CancelFunc)
}

var defaultSource = source{
	processes:     realProcesses,
	readCgroup:    realReadCgroup,
	virtualMemory: mem.VirtualMemory,
	swapMemory:    mem.SwapMemory,
	newSignalContext: func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt)
	},
}
