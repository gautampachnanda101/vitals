// Package memhogs reports which application families and individual processes
// are consuming the most memory, with a suggested action for each. Pure Go via
// gopsutil, so it works on macOS, Linux and Windows.
//
// Families are resolved OS-native first (appFamily: the .app bundle on macOS,
// the systemd/flatpak/snap cgroup scope on Linux, the install directory on
// Windows), which classifies an unbounded number of GUI apps for free. The
// embedded families.json is deliberately small: only cross-app groups the OS
// cannot express ("Node / web dev", "JVM / JetBrains") and headless daemons
// that have no bundle (postgres, redis, llama.cpp). Users extend it with
// <config>/vitals/families.json.
package memhogs

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"

	"vitals/internal/ui"
)

// Options configures a memhogs run.
type Options struct {
	Top      int           // individual processes to list
	Watch    bool          // refresh continuously until interrupted
	Interval time.Duration // refresh period when Watch is set
}

// stopKind is how a process family is best stopped; stopCommand turns it into
// an OS-appropriate command string.
type stopKind int

const (
	stopKill      stopKind = iota // kill the pid
	stopPattern                   // match a stable process-name pattern
	stopQuitApp                   // GUI app: quit it, or kill the pid
	stopDockerAll                 // stop every running container
	stopOllama                    // ollama stop <model>, or kill the pid
)

type family struct {
	name    string
	re      *regexp.Regexp
	kind    stopKind
	pattern string // used only by stopPattern
}

// defaultFamiliesJSON is the built-in family list. It is plain data so the set
// can grow without touching code, and users can extend or override it.
//
//go:embed families.json
var defaultFamiliesJSON []byte

type familySpec struct {
	Name        string `json:"name"`
	Pattern     string `json:"pattern"`
	Stop        string `json:"stop"`
	StopPattern string `json:"stop_pattern,omitempty"`
}

var stopKindNames = map[string]stopKind{
	"kill":       stopKill,
	"pattern":    stopPattern,
	"quit-app":   stopQuitApp,
	"docker-all": stopDockerAll,
	"ollama":     stopOllama,
}

// parseFamilies decodes a family list from JSON, compiling each pattern.
func parseFamilies(data []byte) ([]family, error) {
	var specs []familySpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, err
	}
	out := make([]family, 0, len(specs))
	for _, s := range specs {
		if s.Name == "" {
			return nil, fmt.Errorf("family with empty name")
		}
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("family %q: bad pattern: %w", s.Name, err)
		}
		kind, ok := stopKindNames[s.Stop]
		if !ok {
			return nil, fmt.Errorf("family %q: unknown stop kind %q", s.Name, s.Stop)
		}
		out = append(out, family{s.Name, re, kind, s.StopPattern})
	}
	return out, nil
}

// mergeFamilies overlays user-defined families onto the base list: an entry
// whose name matches a base entry replaces it; a new name is appended.
func mergeFamilies(base, user []family) []family {
	merged := append([]family(nil), base...)
	idx := make(map[string]int, len(merged))
	for i, f := range merged {
		idx[f.name] = i
	}
	for _, f := range user {
		if i, ok := idx[f.name]; ok {
			merged[i] = f
		} else {
			idx[f.name] = len(merged)
			merged = append(merged, f)
		}
	}
	return merged
}

// families returns the built-in list merged with the user's overrides from
// <config>/vitals/families.json, if that file exists and parses.
func families() []family {
	base, err := parseFamilies(defaultFamiliesJSON)
	if err != nil {
		// The embedded file is covered by a test; a failure here is a build bug.
		panic("vitals: embedded families.json is invalid: " + err.Error())
	}
	user := userFamilies()
	if len(user) == 0 {
		return base
	}
	return mergeFamilies(base, user)
}

func userFamilies() []family {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "vitals", "families.json"))
	if err != nil {
		return nil
	}
	fams, err := parseFamilies(data)
	if err != nil {
		ui.Warnf("ignoring %s: %v", filepath.Join(dir, "vitals", "families.json"), err)
		return nil
	}
	return fams
}

// killOne renders the single-process kill command for an OS.
func killOne(pid int32, goos string) string {
	if goos == "windows" {
		return fmt.Sprintf("Stop-Process -Id %d", pid)
	}
	return fmt.Sprintf("kill %d", pid)
}

// stopCommand renders an OS-appropriate remediation command for a family.
func stopCommand(kind stopKind, pattern string, pid int32, goos string) string {
	switch kind {
	case stopPattern:
		if goos == "windows" {
			return killOne(pid, goos)
		}
		return fmt.Sprintf("pkill -f '%s'", pattern)
	case stopQuitApp:
		if goos == "windows" {
			return "close the app, or " + killOne(pid, goos)
		}
		return "quit the app, or " + killOne(pid, goos)
	case stopDockerAll:
		if goos == "windows" {
			return "docker ps -q | ForEach-Object { docker stop $_ }"
		}
		return "docker stop $(docker ps -q)"
	case stopOllama:
		return "ollama stop <model>, or " + killOne(pid, goos)
	default: // stopKill
		return killOne(pid, goos)
	}
}

type procInfo struct {
	pid    int32
	rss    uint64
	name   string
	cmd    string
	exe    string // executable path, for OS-native app grouping
	cgroup string // /proc/<pid>/cgroup body (Linux only)
}

type familyAgg struct {
	name     string
	procs    int
	totalRSS uint64
	topPID   int32
	topRSS   uint64
	kind     stopKind
	pattern  string
}

// readCgroup returns /proc/<pid>/cgroup for OS-native app grouping on Linux;
// empty on every other platform.
func readCgroup(pid int32) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	return string(b)
}

// bucketFamilies groups processes into application families: the OS-native app
// identity (.app bundle / cgroup scope / install dir) first, and the cross-app
// regex families as the fallback and as the source of a more specific stop
// action (docker prune, ollama stop). Returned busiest-first by total RSS.
func bucketFamilies(all []procInfo, goos string, fams []family) []familyAgg {
	buckets := map[string]*familyAgg{}
	var order []string
	for _, pi := range all {
		name := appFamily(goos, pi.exe, pi.cgroup)
		kind := stopQuitApp
		pattern := ""
		hay := pi.name + " " + pi.cmd
		for _, f := range fams {
			if f.re.MatchString(hay) {
				if name == "" {
					name = f.name
				}
				kind, pattern = f.kind, f.pattern
				break
			}
		}
		if name == "" {
			continue
		}
		b := buckets[name]
		if b == nil {
			b = &familyAgg{name: name, kind: kind, pattern: pattern}
			buckets[name] = b
			order = append(order, name)
		} else if b.kind == stopQuitApp && kind != stopQuitApp {
			b.kind, b.pattern = kind, pattern // a later process gave us a better action
		}
		b.procs++
		b.totalRSS += pi.rss
		if pi.rss > b.topRSS {
			b.topRSS, b.topPID = pi.rss, pi.pid
		}
	}
	out := make([]familyAgg, 0, len(order))
	for _, n := range order {
		out = append(out, *buckets[n])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].totalRSS > out[j].totalRSS })
	return out
}

// Run prints the three report sections, once or (with Watch) continuously
// until interrupted.
func Run(opts Options) error {
	if opts.Top <= 0 {
		opts.Top = 15
	}
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	if !opts.Watch {
		return once(opts.Top)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ticker := time.NewTicker(opts.Interval)
	defer ticker.Stop()
	for {
		fmt.Print("\033[H\033[2J") // clear screen
		if err := once(opts.Top); err != nil {
			ui.Errf("%v", err)
		}
		select {
		case <-ctx.Done():
			fmt.Println()
			return nil
		case <-ticker.C:
		}
	}
}

// once prints the three report sections a single time.
func once(topN int) error {
	if topN <= 0 {
		topN = 15
	}

	ps, err := process.Processes()
	if err != nil {
		return fmt.Errorf("enumerate processes: %w", err)
	}

	all := make([]procInfo, 0, len(ps))
	for _, p := range ps {
		mi, err := p.MemoryInfo()
		if err != nil || mi == nil || mi.RSS == 0 {
			continue
		}
		name, _ := p.Name()
		cmd, _ := p.Cmdline()
		if cmd == "" {
			cmd = name
		}
		exe, _ := p.Exe()
		all = append(all, procInfo{
			pid: p.Pid, rss: mi.RSS, name: name, cmd: cmd,
			exe: exe, cgroup: readCgroup(p.Pid),
		})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].rss > all[j].rss })

	// Section 1: application family footprints
	ui.Header("1. APPLICATION FAMILY FOOTPRINTS")
	fmt.Printf("%-24s %-7s %-14s %-12s %s\n", "FAMILY", "PROCS", "TOTAL RAM", "TOP PID", "SUGGESTED ACTION")
	ui.Rule()
	aggs := bucketFamilies(all, runtime.GOOS, families())
	if len(aggs) > topN {
		aggs = aggs[:topN]
	}
	for _, b := range aggs {
		action := stopCommand(b.kind, b.pattern, b.topPID, runtime.GOOS)
		fmt.Printf("%-24s %-7d %-14s %-12s %s\n",
			ui.Truncate(b.name, 24), b.procs, ui.HumanBytes(int64(b.totalRSS)),
			fmt.Sprintf("PID %d", b.topPID), ui.Actionf("%s", action))
	}

	// Section 2: heaviest individual processes
	ui.Header(fmt.Sprintf("2. TOP %d PROCESSES BY RESIDENT MEMORY", topN))
	fmt.Printf("%-8s %-14s %-34s %s\n", "PID", "RSS", "PROCESS", "SUGGESTED ACTION")
	ui.Rule()
	for i, pi := range all {
		if i >= topN {
			break
		}
		label := describe(pi)
		action := ui.Actionf("%s", killOne(pi.pid, runtime.GOOS))
		switch {
		case strings.Contains(pi.name, "WindowServer"):
			action = ui.Actionf("do NOT kill (display server)")
		case strings.Contains(pi.name, "kernel_task"):
			action = ui.Actionf("do NOT kill (kernel thermal control)")
		case strings.Contains(pi.name, "System") && runtime.GOOS == "windows":
			action = ui.Actionf("do NOT kill (Windows kernel)")
		}
		fmt.Printf("%-8d %-14s %-34s %s\n", pi.pid, ui.HumanBytes(int64(pi.rss)), label, action)
	}

	// Section 3: system memory pressure + remedies
	ui.Header("3. SYSTEM MEMORY & REMEDIES")
	if vm, err := mem.VirtualMemory(); err == nil {
		fmt.Printf("  Total RAM        : %s\n", ui.HumanBytes(int64(vm.Total)))
		fmt.Printf("  Used             : %s (%.1f%%)\n", ui.HumanBytes(int64(vm.Used)), vm.UsedPercent)
		fmt.Printf("  Available        : %s\n", ui.HumanBytes(int64(vm.Available)))
	}
	if sw, err := mem.SwapMemory(); err == nil && sw.Total > 0 {
		fmt.Printf("  Swap used        : %s / %s (%.1f%%)\n",
			ui.HumanBytes(int64(sw.Used)), ui.HumanBytes(int64(sw.Total)), sw.UsedPercent)
	}
	fmt.Println()
	switch runtime.GOOS {
	case "darwin":
		fmt.Println("  • Free inactive/purgeable RAM:      " + ui.Actionf("sudo purge"))
		fmt.Println("  • Drop Chrome background tabs:      " + ui.Actionf("pkill -f 'Chrome Helper (Renderer)'"))
		fmt.Println("  • Restart VS Code extension host:   " + ui.Actionf("pkill -f 'Code Helper (Plugin)'"))
	case "linux":
		fmt.Println("  • Drop page cache (root):           " + ui.Actionf("sync; echo 1 > /proc/sys/vm/drop_caches"))
		fmt.Println("  • Inspect a process map:            " + ui.Actionf("pmap -x <pid>"))
	case "windows":
		fmt.Println("  • Empty working sets:               " + ui.Actionf("EmptyStandbyList.exe workingsets"))
		fmt.Println("  • Inspect with:                     " + ui.Actionf("Get-Process | Sort-Object WS -Descending"))
	}
	return nil
}

func describe(pi procInfo) string {
	switch {
	case strings.Contains(pi.cmd, "Chrome Helper (Renderer)"):
		return "Chrome tab (renderer)"
	case strings.Contains(pi.cmd, "Chrome Helper (GPU)"):
		return "Chrome GPU process"
	case strings.Contains(pi.cmd, "Code Helper (Plugin)"):
		return "VS Code extension host"
	case strings.Contains(pi.cmd, "Code Helper"):
		return "VS Code helper"
	}
	n := pi.name
	if n == "" {
		n = "(unknown)"
	}
	return n
}
