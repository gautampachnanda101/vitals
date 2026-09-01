// Package memhogs reports which application families and individual processes
// are consuming the most memory, with a suggested action for each. Pure Go via
// gopsutil, so it works on macOS, Linux and Windows.
package memhogs

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"

	"vitals/internal/ui"
)

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

func families() []family {
	def := []struct {
		name, pat string
		kind      stopKind
		pattern   string
	}{
		{"Google Chrome", `(?i)google chrome|chrome helper|chromium`, stopQuitApp, ""},
		{"VS Code", `(?i)\bcode\b|vscode|code helper`, stopQuitApp, ""},
		{"Node / Web dev", `(?i)\bnode\b|next-server|vite|webpack|esbuild`, stopKill, ""},
		{"Docker", `(?i)com\.docker|dockerd|docker desktop`, stopDockerAll, ""},
		{"Safari", `(?i)safari`, stopPattern, "Safari"},
		{"Firefox", `(?i)firefox`, stopPattern, "firefox"},
		{"Slack", `(?i)slack`, stopQuitApp, ""},
		{"Ollama", `(?i)ollama`, stopOllama, ""},
		{"LM Studio", `(?i)lm studio|lmstudio|lms\b`, stopQuitApp, ""},
		{"llama.cpp", `(?i)llama-server|llama\.cpp|llamacpp`, stopKill, ""},
		{"Java / JetBrains", `(?i)\bjava\b|idea|goland|pycharm|webstorm|rubymine`, stopKill, ""},
		{"Electron (generic)", `(?i)electron`, stopQuitApp, ""},
	}
	out := make([]family, 0, len(def))
	for _, d := range def {
		out = append(out, family{d.name, regexp.MustCompile(d.pat), d.kind, d.pattern})
	}
	return out
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
	pid  int32
	rss  uint64
	name string
	cmd  string
}

// Run prints the three report sections.
func Run(topN int) error {
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
		all = append(all, procInfo{pid: p.Pid, rss: mi.RSS, name: name, cmd: cmd})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].rss > all[j].rss })

	// Section 1: application family footprints
	ui.Header("1. APPLICATION FAMILY FOOTPRINTS")
	fmt.Printf("%-20s %-7s %-14s %-12s %s\n", "FAMILY", "PROCS", "TOTAL RAM", "TOP PID", "SUGGESTED ACTION")
	ui.Rule()
	for _, f := range families() {
		var total uint64
		var count int
		var topPID int32
		var topRSS uint64
		for _, pi := range all {
			hay := pi.name + " " + pi.cmd
			if !f.re.MatchString(hay) {
				continue
			}
			count++
			total += pi.rss
			if pi.rss > topRSS {
				topRSS, topPID = pi.rss, pi.pid
			}
		}
		if count == 0 {
			continue
		}
		action := stopCommand(f.kind, f.pattern, topPID, runtime.GOOS)
		fmt.Printf("%-20s %-7d %-14s %-12s %s\n",
			f.name, count, ui.HumanBytes(int64(total)),
			fmt.Sprintf("PID %d", topPID), ui.Actionf("%s", action))
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
