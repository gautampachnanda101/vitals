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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"vitals/internal/ui"
)

// Options controls a cleanup run.
type Options struct {
	DryRun      bool // report what would be deleted, delete nothing
	Assume      bool // skip the interactive confirmation prompt
	ShowHistory bool // print past runs from the audit log instead of cleaning
}

type runner struct {
	opts      Options
	home      string
	freedByRM atomic.Int64 // bytes accounted for by our own deletions

	recordsMu sync.Mutex
	records   []PurgeRecord // one entry per non-empty purgeContents call, for the audit log
}

// Result is what a cleanup run actually did — the structured form Run
// prints, and what a future dashboard write action can return as JSON
// instead of vitals clean's own stdout. FreeBefore/FreeAfter are 0 when
// unavailable (freeSpace can't determine it), not a sentinel error.
type Result struct {
	FreedBytes int64
	FreeBefore int64
	FreeAfter  int64
	Records    []PurgeRecord
	DryRun     bool
}

// Apply runs the real (or, with opts.DryRun, a measure-only) cleanup and
// returns what happened — no printing, no confirmation prompt, both of
// which are Run's own CLI-only concerns below. This is what Run's own
// internals do after its prompt already passed; a future dashboard
// write action calls this directly instead of duplicating the cleanup
// logic, the same relationship doctor.Assess has to doctor.Collect+
// Analyze.
func Apply(home string, opts Options) Result {
	r := &runner{opts: opts, home: home}

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

	if !opts.DryRun {
		recordRun(RunRecord{Time: time.Now(), TotalBytes: r.freedByRM.Load(), Purges: r.records})
	}

	return Result{
		FreedBytes: r.freedByRM.Load(),
		FreeBefore: freeBefore,
		FreeAfter:  freeAfter,
		Records:    r.records,
		DryRun:     opts.DryRun,
	}
}

// Run executes the cleanup and prints a report.
func Run(opts Options) error {
	if opts.ShowHistory {
		ui.Header("CLEAN HISTORY")
		fmt.Print(renderCleanHistory(History()))
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}

	ui.Header("CROSS-PLATFORM DISK CLEANER")
	fmt.Printf("  OS: %s/%s   Home: %s\n", runtime.GOOS, runtime.GOARCH, home)
	if opts.DryRun {
		ui.Warnf("dry-run: nothing will be deleted")
	} else if !opts.Assume && !confirm() {
		ui.Infof("aborted")
		return nil
	}

	result := Apply(home, opts)

	ui.Header("SUMMARY")
	measured := ui.HumanBytes(result.FreedBytes)
	if opts.DryRun {
		fmt.Printf("  Would remove (measured)      : %s\n", measured)
	} else {
		fmt.Printf("  Removed (measured)           : %s\n", measured)
	}
	// Filesystem free space is shown as context only, never as a "reclaimed"
	// figure: other processes write to the disk concurrently, so the delta is
	// not attributable to this run. The measured byte count above is the
	// honest number.
	if !opts.DryRun && result.FreeBefore > 0 && result.FreeAfter > 0 {
		fmt.Printf("  Root filesystem free space   : %s -> %s\n",
			ui.HumanBytes(result.FreeBefore), ui.HumanBytes(result.FreeAfter))
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

// freeSpaceRoot picks which volume/mount freeSpace measures — pulled out
// as a pure function of (goos, systemRoot) so both branches are testable
// without depending on which OS actually runs the test (the coverage
// gate only ever runs on Linux CI, so a Windows-only branch would
// otherwise never be exercised there regardless of the test matrix).
func freeSpaceRoot(goos, systemRoot string) string {
	if goos != "windows" {
		return "/"
	}
	root := filepath.VolumeName(systemRoot) + `\`
	if root == `\` {
		root = `C:\`
	}
	return root
}

func freeSpace() int64 {
	u, err := disk.Usage(freeSpaceRoot(runtime.GOOS, os.Getenv("SystemRoot")))
	if err != nil {
		return 0
	}
	return int64(u.Free)
}

// --- cache groups ---------------------------------------------------------------

// devCacheDirs are the language/tool caches cleaned on every OS. Shared with
// ReclaimableSummary so `vitals disk` reports the same directories `clean`
// would actually touch.
func devCacheDirs(home string) []string {
	return []string{
		filepath.Join(home, ".cache"),
		filepath.Join(home, ".npm", "_cacache"),
		filepath.Join(home, ".yarn", "berry", "cache"),
		filepath.Join(home, "Library", "Caches", "pip"),
		filepath.Join(home, ".gradle", "caches"),
		filepath.Join(home, ".m2", "repository", ".cache"),
		filepath.Join(home, ".cargo", "registry", "cache"), // downloaded .crate archives — re-fetched on demand
	}
}

// osCacheDirs are the OS-specific cache/log/temp locations cleaned by
// cleanLinux/cleanMacOS/cleanWindows. Shared with ReclaimableSummary.
// goos is a parameter (not read from runtime.GOOS directly) for the same
// testability reason as freeSpaceRoot/withSudo — see their comments.
func osCacheDirs(goos, home string) []string {
	switch goos {
	case "linux":
		return []string{"/var/tmp", "/tmp"}
	case "darwin":
		lib := filepath.Join(home, "Library")
		return []string{
			filepath.Join(lib, "Caches"),
			filepath.Join(lib, "Logs"),
			filepath.Join(home, ".Trash"),
			filepath.Join(lib, "Developer", "Xcode", "DerivedData"), // rebuilt on the next Xcode build
		}
	case "windows":
		var dirs []string
		for _, env := range []string{"TEMP", "TMP"} {
			if v := os.Getenv(env); v != "" {
				dirs = append(dirs, v)
			}
		}
		// Windows has no single umbrella cache directory the way Linux has
		// ~/.cache and macOS has ~/Library/Caches (both swept wholesale
		// above) — so app and browser caches are named individually here.
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			dirs = append(dirs,
				filepath.Join(la, "Temp"),
				filepath.Join(la, "Google", "Chrome", "User Data", "Default", "Cache"),
				filepath.Join(la, "Microsoft", "Edge", "User Data", "Default", "Cache"),
				filepath.Join(la, "Microsoft", "Windows", "INetCache"),
				filepath.Join(la, "Microsoft", "Windows", "Explorer"), // thumbcache_*.db
			)
			if matches, _ := filepath.Glob(filepath.Join(la, "Mozilla", "Firefox", "Profiles", "*", "cache2")); len(matches) > 0 {
				dirs = append(dirs, matches...)
			}
		}
		return dirs
	default:
		return nil
	}
}

func (r *runner) cleanDevCaches() {
	ui.Infof("developer & runtime caches")
	for _, d := range devCacheDirs(r.home) {
		r.purgeContents(d)
	}
	r.optional("docker", "system", "prune", "-f")
	r.optional("pip", "cache", "purge")
	r.optional("npm", "cache", "clean", "--force")
	r.optional("pnpm", "store", "prune")
}

func (r *runner) cleanLinux() {
	ui.Infof("linux package caches & temp")
	r.optional("apt-get", "clean")
	r.optional("dnf", "clean", "all")
	r.optional("yum", "clean", "all")
	r.optional("pacman", "-Scc", "--noconfirm")
	r.optional("journalctl", "--vacuum-time=7d")
	r.optional("flatpak", "uninstall", "--unused", "-y")
	for _, d := range osCacheDirs(runtime.GOOS, r.home) {
		r.purgeContents(d)
	}
}

func (r *runner) cleanMacOS() {
	ui.Infof("macOS caches, logs & trash")
	for _, d := range osCacheDirs(runtime.GOOS, r.home) {
		r.purgeContents(d)
	}
	r.optional("brew", "cleanup", "-s")
	r.optional("xcrun", "simctl", "delete", "unavailable")
}

func (r *runner) cleanWindows() {
	ui.Infof("windows temp & component store")
	for _, d := range osCacheDirs(runtime.GOOS, r.home) {
		r.purgeContents(d)
	}
	if sr := os.Getenv("SystemRoot"); sr != "" {
		r.purgeContents(filepath.Join(sr, "Temp"))
		r.purgeContents(filepath.Join(sr, "Prefetch"))
	}
	if !r.opts.DryRun {
		r.optional("powershell", "-NoProfile", "-Command",
			"Clear-RecycleBin -Force -ErrorAction SilentlyContinue")
	}
	// Component-store cleanup: superseded WinSxS versions of updated
	// components, the closest Windows equivalent of `brew cleanup` /
	// `apt-get clean`. Needs elevation; silently no-ops without it.
	r.optional("dism.exe", "/online", "/cleanup-image", "/startcomponentcleanup")
}

// CacheEntry is one candidate directory's measured size, for reporting what a
// `clean` run would actually reclaim without deleting anything.
type CacheEntry struct {
	Path  string
	Bytes int64
}

// ReclaimableSummary measures the same cache/log/trash directories `clean`
// targets, largest first, without deleting anything. It stops as soon as
// budget elapses — everything measured up to that point is still returned,
// complete just reports whether every candidate directory was reached, so a
// huge cache can't stall an interactive command like `vitals disk`.
func ReclaimableSummary(budget time.Duration) (entries []CacheEntry, complete bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	dirs := append(devCacheDirs(home), osCacheDirs(runtime.GOOS, home)...)
	return measureDirs(dirs, budget)
}

// measureDirs is ReclaimableSummary's testable core: given an explicit
// directory list (so tests don't depend on the real home directory), measure
// each one, largest first, stopping once budget elapses.
func measureDirs(dirs []string, budget time.Duration) (entries []CacheEntry, complete bool) {
	deadline := time.Now().Add(budget)
	complete = true
	for _, d := range dirs {
		if time.Now().After(deadline) {
			complete = false
			break
		}
		if _, err := os.Stat(d); err != nil {
			continue
		}
		_, sz := treeStat(d)
		if sz > 0 {
			entries = append(entries, CacheEntry{Path: d, Bytes: sz})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Bytes > entries[j].Bytes })
	return entries, complete
}

// --- helpers ------------------------------------------------------------------

// purgeContents removes every entry inside dir (but keeps dir itself). dir
// is always one of a fixed set of cache/log/trash paths this package
// hardcodes — never a config- or user-supplied path — but that path could
// still have been replaced with a symlink before this ran (by malware, or
// by anything else that already has write access to $HOME). os.Lstat here,
// checked before ReadDir, refuses to purge through a symlink: only a real
// directory found sitting at dir is ever eligible.
func (r *runner) purgeContents(dir string) {
	if dir == "" {
		return
	}
	if fi, err := os.Lstat(dir); err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
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
		if !r.opts.DryRun {
			r.recordsMu.Lock()
			r.records = append(r.records, PurgeRecord{Dir: dir, Bytes: size, Entries: removed})
			r.recordsMu.Unlock()
		}
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
