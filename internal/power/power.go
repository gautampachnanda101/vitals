// Package power reads a real per-process energy figure on macOS.
//
// macOS's own tools (Activity Monitor's "Energy Impact") derive their
// number from OS power accounting, not CPU%. `top -l 2 -o power -stats
// pid,command,power` exposes the same per-process "power" score without
// sudo — `powermetrics` is richer but needs root, so it is out for a
// passive tool. Linux and Windows have no equivalent no-privilege
// per-process energy number; on those platforms Sample reports
// unavailable and callers keep their CPU-ranked estimate.
//
// This is presentation-only: nothing here touches the frozen
// `doctor --json` schema (roadmap item 012).
package power

import (
	"context"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"vitals/internal/ui"
)

// Proc is one process's energy reading. Power is macOS `top`'s unitless
// per-process power score — the same figure Activity Monitor shows in its
// Energy tab, not a wattage.
type Proc struct {
	PID   int32
	Name  string
	Power float64
}

// maxSampleWindow bounds the whole probe. `top -l 2` waits ~1s between
// its two samples; a slow machine can take longer, but a passive tool
// must never hang a page or a command on it.
const maxSampleWindow = 4 * time.Second

// deps is the OS surface Sample reads from, pulled out so tests drive a
// canned `top` transcript instead of the real machine.
type deps struct {
	goos string
	run  func(ctx context.Context, name string, args ...string) ([]byte, error)
}

var defaultDeps = deps{
	goos: runtime.GOOS,
	run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	},
}

// Sample returns the per-process energy readings, ranked highest first,
// and ok=false when this platform has no no-privilege per-process power
// source (everything but macOS) or the probe failed. A caller that gets
// ok=false keeps whatever estimate it already shows.
func Sample(limit int) (procs []Proc, ok bool) {
	return sample(defaultDeps, limit)
}

func sample(d deps, limit int) ([]Proc, bool) {
	if d.goos != "darwin" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxSampleWindow)
	defer cancel()
	out, err := d.run(ctx, "top", "-l", "2", "-o", "power", "-stats", "pid,command,power", "-n", "40")
	if err != nil {
		return nil, false
	}
	procs := parseTopPower(out)
	if len(procs) == 0 {
		return nil, false
	}
	sort.SliceStable(procs, func(i, j int) bool { return procs[i].Power > procs[j].Power })
	if limit > 0 && len(procs) > limit {
		procs = procs[:limit]
	}
	return procs, true
}

// parseTopPower pulls the rows out of the LAST "PID COMMAND POWER" block
// in a `top -l 2` transcript — the first sample can't compute a power
// rate yet and reports every process as 0.0, so only the second block is
// real. Each row is `<pid> <command, may contain spaces> <power>`; power
// is always the last field and pid the first, so the command is whatever
// lies between regardless of embedded spaces or `top`'s column padding.
func parseTopPower(out []byte) []Proc {
	lines := strings.Split(string(out), "\n")
	lastHeader := -1
	for i, ln := range lines {
		f := strings.Fields(ln)
		if len(f) >= 3 && f[0] == "PID" && f[len(f)-1] == "POWER" {
			lastHeader = i
		}
	}
	if lastHeader < 0 {
		return nil
	}
	var procs []Proc
	for _, ln := range lines[lastHeader+1:] {
		if strings.TrimSpace(ln) == "" {
			break // blank line ends the block
		}
		if strings.HasPrefix(ln, "Processes:") {
			break // a following sample block (defensive; -l 2 shouldn't reach here)
		}
		f := strings.Fields(ln)
		if len(f) < 3 {
			continue
		}
		pid, err := strconv.ParseInt(f[0], 10, 32)
		if err != nil {
			continue
		}
		pw, err := strconv.ParseFloat(f[len(f)-1], 64)
		if err != nil {
			continue
		}
		name := strings.Join(f[1:len(f)-1], " ")
		procs = append(procs, Proc{PID: int32(pid), Name: ui.Sanitize(name), Power: pw})
	}
	return procs
}
