// Package clean implements a cross-platform disk cleanup routine in pure Go.
//
// All file removal is done with the Go standard library so the binary is fully
// self-contained. Optional package-manager cleanups (brew, docker, pip, apt...)
// are only attempted when the corresponding executable is already on PATH; if it
// is missing the step is silently skipped.
package clean

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/shirou/gopsutil/v4/disk"

	"vitals/internal/ui"
)

// Options controls a cleanup run.
type Options struct {
	DryRun bool // report what would be deleted, delete nothing
	Assume bool // skip the interactive confirmation prompt
}

type runner struct {
	opts      Options
	home      string
	freedByRM atomic.Int64 // bytes accounted for by our own deletions
}

// Run executes the cleanup and prints a report.
func Run(opts Options) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	r := &runner{opts: opts, home: home}

	ui.Header("CROSS-PLATFORM DISK CLEANER")
	fmt.Printf("  OS: %s/%s   Home: %s\n", runtime.GOOS, runtime.GOARCH, home)
	if opts.DryRun {
		ui.Warnf("dry-run: nothing will be deleted")
	} else if !opts.Assume && !confirm() {
		ui.Infof("aborted")
		return nil
	}

	freeBefore := freeSpace()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.cleanDevCaches() }()
	go func() {
		defer wg.Done()
		switch runtime.GOOS {
		case "linux":
			r.cleanLinux()
		case "darwin":
			r.cleanMacOS()
		case "windows":
			r.cleanWindows()
		default:
			ui.Warnf("no OS-specific cleanup for %s", runtime.GOOS)
		}
	}()
	wg.Wait()

	freeAfter := freeSpace()

	ui.Header("SUMMARY")
	measured := ui.HumanBytes(r.freedByRM.Load())
	if opts.DryRun {
		fmt.Printf("  Would remove (measured)      : %s\n", measured)
	} else {
		fmt.Printf("  Removed (measured)           : %s\n", measured)
	}
	// Filesystem free space is shown as context only, never as a "reclaimed"
	// figure: other processes write to the disk concurrently, so the delta is
	// not attributable to this run. The measured byte count above is the
	// honest number.
	if !opts.DryRun && freeBefore > 0 && freeAfter > 0 {
		fmt.Printf("  Root filesystem free space   : %s -> %s\n",
			ui.HumanBytes(freeBefore), ui.HumanBytes(freeAfter))
	}
	if opts.DryRun {
		ui.Okf("dry-run complete — re-run without --dry-run to apply")
	} else {
		ui.Okf("cleanup complete")
	}
	return nil
}

func confirm() bool {
	fmt.Print(ui.Yellow + "This permanently deletes cache/log/temp files. Continue? [y/N] " + ui.Reset)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}

func freeSpace() int64 {
	root := "/"
	if runtime.GOOS == "windows" {
		root = filepath.VolumeName(os.Getenv("SystemRoot")) + `\`
		if root == `\` {
			root = `C:\`
		}
	}
	u, err := disk.Usage(root)
	if err != nil {
		return 0
	}
	return int64(u.Free)
}

// --- cache groups ---------------------------------------------------------------

func (r *runner) cleanDevCaches() {
	ui.Infof("developer & runtime caches")
	dirs := []string{
		filepath.Join(r.home, ".cache"),
		filepath.Join(r.home, ".npm", "_cacache"),
		filepath.Join(r.home, ".yarn", "berry", "cache"),
		filepath.Join(r.home, "Library", "Caches", "pip"),
		filepath.Join(r.home, ".gradle", "caches"),
		filepath.Join(r.home, ".m2", "repository", ".cache"),
	}
	for _, d := range dirs {
		r.purgeContents(d)
	}
	r.optional("docker", "system", "prune", "-f")
	r.optional("pip", "cache", "purge")
	r.optional("npm", "cache", "clean", "--force")
}

func (r *runner) cleanLinux() {
	ui.Infof("linux package caches & temp")
	r.optional("apt-get", "clean")
	r.optional("dnf", "clean", "all")
	r.optional("yum", "clean", "all")
	r.optional("pacman", "-Scc", "--noconfirm")
	r.optional("journalctl", "--vacuum-time=7d")
	r.purgeContents("/var/tmp")
	r.purgeContents("/tmp")
}

func (r *runner) cleanMacOS() {
	ui.Infof("macOS caches, logs & trash")
	lib := filepath.Join(r.home, "Library")
	r.purgeContents(filepath.Join(lib, "Caches"))
	r.purgeContents(filepath.Join(lib, "Logs"))
	r.purgeContents(filepath.Join(r.home, ".Trash"))
	r.optional("brew", "cleanup", "-s")
}

func (r *runner) cleanWindows() {
	ui.Infof("windows temp & component store")
	for _, env := range []string{"TEMP", "TMP"} {
		r.purgeContents(os.Getenv(env))
	}
	if sr := os.Getenv("SystemRoot"); sr != "" {
		r.purgeContents(filepath.Join(sr, "Temp"))
		r.purgeContents(filepath.Join(sr, "Prefetch"))
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		r.purgeContents(filepath.Join(la, "Temp"))
	}
	if !r.opts.DryRun {
		r.optional("powershell", "-NoProfile", "-Command",
			"Clear-RecycleBin -Force -ErrorAction SilentlyContinue")
	}
}

// --- helpers ------------------------------------------------------------------

// purgeContents removes every entry inside dir (but keeps dir itself).
func (r *runner) purgeContents(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var size int64
	var removed int
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		sz, _ := removeTree(full, r.opts.DryRun)
		if r.opts.DryRun {
			size += sz
			removed++
			continue
		}
		if _, statErr := os.Lstat(full); os.IsNotExist(statErr) {
			size += sz
			removed++
		}
	}
	if removed > 0 {
		r.freedByRM.Add(size)
		verb := "removed"
		if r.opts.DryRun {
			verb = "would remove"
		}
		fmt.Printf("    %s %-3d entries  %-10s  %s\n", verb, removed, ui.HumanBytes(size), dir)
	}
}

// treeStat walks path once and returns the count and total size of the regular
// files under it.
func treeStat(path string) (files int, bytes int64) {
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e == nil && info.Mode().IsRegular() {
			files++
			bytes += info.Size()
		}
		return nil
	})
	return
}

// removeTree deletes path and everything under it in a single post-order pass,
// returning the total size of the regular files it accounted for. With dryRun
// it measures but deletes nothing. Unreadable entries are skipped, not fatal.
func removeTree(path string, dryRun bool) (int64, error) {
	var total int64
	var dirs []string
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return nil
		}
		if info, e := d.Info(); e == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		if !dryRun {
			_ = os.Remove(p)
		}
		return nil
	})
	if !dryRun {
		for i := len(dirs) - 1; i >= 0; i-- { // deepest first
			_ = os.Remove(dirs[i])
		}
	}
	return total, err
}

func (r *runner) optional(name string, args ...string) {
	if _, err := exec.LookPath(name); err != nil {
		return
	}
	if r.opts.DryRun {
		fmt.Printf("    would run: %s %s\n", name, strings.Join(args, " "))
		return
	}
	fmt.Printf("    run: %s %s\n", name, strings.Join(args, " "))
	_ = exec.Command(name, args...).Run()
}
