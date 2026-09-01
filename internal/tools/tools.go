// Package tools turns vitals into a launcher/installer for the established
// tools it has always deferred to (ncdu, gdu, dust, btop, htop, nvtop,
// jdupes, smartmontools) instead of reimplementing their specialties. vitals
// detects what's already on PATH, offers to install what's missing via the
// host's own package manager (brew/apt/dnf/pacman/winget/scoop — never a
// bespoke installer), and can hand off to the best available one directly.
package tools

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"vitals/internal/ui"
)

// Manager is a system package manager vitals knows how to drive.
type Manager string

const (
	Brew   Manager = "brew"
	Apt    Manager = "apt-get"
	Dnf    Manager = "dnf"
	Pacman Manager = "pacman"
	Winget Manager = "winget"
	Scoop  Manager = "scoop"
)

// Tool is one companion tool vitals can detect, install, or launch.
type Tool struct {
	Name        string // display name and, unless Binary is set, the PATH binary
	Binary      string // PATH binary name, if different from Name (e.g. smartctl ships in the smartmontools package)
	Category    string // "disk explorer", "live monitor", "GPU monitor", "duplicate finder", "disk health"
	Description string
	Packages    map[Manager]string // package name per manager; a manager absent here has no known package
}

func (t Tool) binary() string {
	if t.Binary != "" {
		return t.Binary
	}
	return t.Name
}

// Registry is every companion tool vitals knows about, in a stable display
// order. Package names are listed only where they're confidently correct —
// an absent manager means Install reports "install it manually" rather than
// guessing at a package name that might not exist.
var Registry = []Tool{
	{Name: "gdu", Category: "disk explorer", Description: "much faster ncdu-alike disk usage browser, good on large SSDs",
		Packages: map[Manager]string{Brew: "gdu", Apt: "gdu", Dnf: "gdu", Pacman: "gdu", Scoop: "gdu"}},
	{Name: "ncdu", Category: "disk explorer", Description: "the classic interactive ncurses disk usage browser",
		Packages: map[Manager]string{Brew: "ncdu", Apt: "ncdu", Dnf: "ncdu", Pacman: "ncdu"}},
	{Name: "dust", Category: "disk explorer", Description: "du replacement with an inline size tree (view-only, no delete)",
		Packages: map[Manager]string{Brew: "dust", Pacman: "dust", Scoop: "dust"}},
	{Name: "btop", Category: "live monitor", Description: "fancier htop: per-core graphs, memory history, disk/net I/O",
		Packages: map[Manager]string{Brew: "btop", Apt: "btop", Dnf: "btop", Pacman: "btop"}},
	{Name: "htop", Category: "live monitor", Description: "the classic interactive process viewer",
		Packages: map[Manager]string{Brew: "htop", Apt: "htop", Dnf: "htop", Pacman: "htop"}},
	{Name: "nvtop", Category: "GPU monitor", Description: "live per-process GPU utilisation and VRAM (NVIDIA/AMD/Intel)",
		Packages: map[Manager]string{Apt: "nvtop", Dnf: "nvtop", Pacman: "nvtop"}},
	{Name: "jdupes", Category: "duplicate finder", Description: "much faster duplicate finder; can hardlink or reflink matches",
		Packages: map[Manager]string{Brew: "jdupes", Apt: "jdupes", Dnf: "jdupes", Pacman: "jdupes", Winget: "jbruchon.jdupes"}},
	{Name: "smartctl", Binary: "smartctl", Category: "disk health", Description: "S.M.A.R.T. health and wear reporting for physical disks",
		Packages: map[Manager]string{Brew: "smartmontools", Apt: "smartmontools", Dnf: "smartmontools", Pacman: "smartmontools", Winget: "smartmontools"}},
}

// Find looks up a registry entry by name, case-insensitively.
func Find(name string) (Tool, bool) {
	for _, t := range Registry {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Tool{}, false
}

// Installed reports whether t's binary is on PATH.
func Installed(t Tool) bool {
	_, err := exec.LookPath(t.binary())
	return err == nil
}

// byCategory returns every registry entry in category, in the order they
// should be preferred (Registry's own order already reflects that — e.g.
// gdu before ncdu before dust for "disk explorer").
func byCategory(category string) []Tool {
	var out []Tool
	for _, t := range Registry {
		if strings.EqualFold(t.Category, category) {
			out = append(out, t)
		}
	}
	return out
}

// detectManager returns the first package manager available on this host,
// trying the platform's usual options in order.
func detectManager() (Manager, bool) {
	var candidates []Manager
	switch runtime.GOOS {
	case "darwin":
		candidates = []Manager{Brew}
	case "linux":
		candidates = []Manager{Apt, Dnf, Pacman}
	case "windows":
		candidates = []Manager{Winget, Scoop}
	}
	for _, m := range candidates {
		if _, err := exec.LookPath(string(m)); err == nil {
			return m, true
		}
	}
	return "", false
}

// installCommand builds the exact command line for installing pkg via mgr,
// prefixing sudo for the Linux system package managers when not already
// root (irrelevant, and skipped, on Windows and for brew, which refuses to
// run as root at all).
func installCommand(mgr Manager, pkg string) (string, []string) {
	switch mgr {
	case Brew:
		return "brew", []string{"install", pkg}
	case Apt:
		return withSudo([]string{"apt-get", "install", "-y", pkg})
	case Dnf:
		return withSudo([]string{"dnf", "install", "-y", pkg})
	case Pacman:
		return withSudo([]string{"pacman", "-S", "--noconfirm", pkg})
	case Winget:
		return "winget", []string{"install", "-e", "--id", pkg}
	case Scoop:
		return "scoop", []string{"install", pkg}
	default:
		return "", nil
	}
}

func withSudo(args []string) (string, []string) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		return "sudo", args
	}
	return args[0], args[1:]
}

// Options configures Run.
type Options struct {
	Install string // tool name to install; empty just lists everything
	Yes     bool   // skip the confirmation prompt before installing
}

// Run lists every known companion tool with its install status, or installs
// one when Options.Install is set.
func Run(opts Options) error {
	if opts.Install != "" {
		return Install(opts.Install, opts.Yes)
	}
	List()
	return nil
}

// List prints every registry entry with its install status.
func List() {
	ui.Header("COMPANION TOOLS")
	fmt.Println(ui.Key("  vitals complements these rather than reimplementing them — see `vitals tools install <name>`"))
	fmt.Println()
	for _, t := range Registry {
		status := ui.Key("not installed")
		if Installed(t) {
			status = ui.Green + "installed" + ui.Reset
		}
		fmt.Printf("  %-10s %-18s %-16s %s\n", t.Name, t.Category, status, t.Description)
	}
}

// Install installs the named tool via the host's package manager, printing
// the exact command before running it and confirming first unless yes is
// set — installing software is a real, system-wide change, the same bar
// `vitals clean` holds destructive cleanup to.
func Install(name string, yes bool) error {
	t, ok := Find(name)
	if !ok {
		return fmt.Errorf("unknown tool %q — run `vitals tools` to see the list", name)
	}
	if Installed(t) {
		ui.Okf("%s is already installed", t.Name)
		return nil
	}
	mgr, ok := detectManager()
	if !ok {
		return fmt.Errorf("no supported package manager found for %s — install %s manually", runtime.GOOS, t.Name)
	}
	pkg, ok := t.Packages[mgr]
	if !ok {
		return fmt.Errorf("no known %s package for %s on %s — install it manually", t.Name, mgr, runtime.GOOS)
	}
	cmdName, args := installCommand(mgr, pkg)

	fmt.Printf("  this will run: %s %s %s\n", ui.Bold, strings.Join(append([]string{cmdName}, args...), " "), ui.Reset)
	if !yes && !confirm() {
		ui.Infof("aborted")
		return nil
	}
	cmd := exec.Command(cmdName, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Launch hands off to the best installed tool in category, replacing
// vitals' own stdio with the child's so its TUI (ncdu, btop, ...) behaves
// exactly as it would run directly.
func Launch(category string, args []string) error {
	candidates := byCategory(category)
	for _, t := range candidates {
		if !Installed(t) {
			continue
		}
		cmd := exec.Command(t.binary(), args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	names := make([]string, len(candidates))
	for i, t := range candidates {
		names[i] = t.Name
	}
	return fmt.Errorf("none of %s is installed — run `vitals tools install %s`",
		strings.Join(names, "/"), firstOrEmpty(names))
}

func firstOrEmpty(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func confirm() bool {
	fmt.Print(ui.Yellow + "Continue? [y/N] " + ui.Reset)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}
