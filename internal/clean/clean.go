// Package clean plans and applies selective, bounded cross-platform cleanup.
package clean

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// deps is the live OS surface Run/confirm read from, pulled out so a
// test can substitute fakes — same shape as internal/tools'/internal/
// dupes'/internal/metrics'/internal/guide's/internal/llm's deps
// structs. defaultDeps wires the real calls; production always goes
// through it via Run.
type deps struct {
	homeDir       func() (string, error)
	confirmReader io.Reader
}

var defaultDeps = deps{homeDir: os.UserHomeDir, confirmReader: os.Stdin}

// Options controls a cleanup run.
type Options struct {
	DryRun      bool // report what would be deleted, delete nothing
	Assume      bool // skip the interactive confirmation prompt
	ShowHistory bool // print past runs from the audit log instead of cleaning
	Categories  string
	MinAge      time.Duration
	MinAgeSet   bool
	JSON        bool
	Tools       string // optional comma-separated subset of tool actions
}

type runner struct {
	opts      Options
	freedByRM atomic.Int64 // bytes accounted for by our own deletions

	recordsMu sync.Mutex
	records   []PurgeRecord // one entry per non-empty purgeContents call, for the audit log

	// lookPath/runCmd back optional()'s package-manager cleanup calls
	// (brew/docker/npm/...) — injected so a test can prove the exact
	// argv a call site builds (a recordingRunCmd, same shape internal/
	// tools' does) without ever actually shelling out to a real package
	// manager (a hung/networked optional() call was flagged as a real,
	// accepted risk by this item's own dashboard write-action security
	// review — see docs/roadmap/items/005-dashboard-write-actions/
	// design.md §7 finding 2).
	lookPath func(name string) (string, error)
	runCmd   func(name string, args ...string) error
}

// Result is what a cleanup run actually did — the structured form Run
// prints, and what a future dashboard write action can return as JSON
// instead of vitals clean's own stdout. FreeBefore/FreeAfter are 0 when
// unavailable (freeSpace can't determine it), not a sentinel error.
type Result struct {
	FreedBytes int64         `json:"freed_bytes"`
	FreeBefore int64         `json:"free_before"`
	FreeAfter  int64         `json:"free_after"`
	Records    []PurgeRecord `json:"records,omitempty"`
	DryRun     bool          `json:"dry_run"`
	Actions    []Action      `json:"actions"`
	Complete   bool          `json:"complete"`
	Error      string        `json:"error,omitempty"`
	Warning    string        `json:"warning"`
}

// Apply runs the real (or, with opts.DryRun, a measure-only) cleanup and
// returns what happened — no printing, no confirmation prompt, both of
// which are Run's own CLI-only concerns below. This is what Run's own
// internals do after its prompt already passed; a future dashboard
// write action calls this directly instead of duplicating the cleanup
// logic, the same relationship doctor.Assess has to doctor.Collect+
// Analyze.
func Apply(home string, opts Options) Result {
	opts, err := normalizedOptions(opts)
	if err != nil {
		return Result{DryRun: opts.DryRun, Error: err.Error(), Warning: RiskWarning}
	}
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	p := buildPlan(ctx, cleanupTargets(runtime.GOOS, home, opts), time.Now().Add(-opts.MinAge), planFileLimit)
	markToolAvailability(&p, exec.LookPath)
	discoverToolCaches(ctx, &p, probeToolCache)
	cancel()
	if opts.DryRun {
		return previewResult(p)
	}
	before := freeSpace()
	result := executePlan(context.Background(), p, runCleanupCommand, nil)
	result.FreeBefore, result.FreeAfter = before, freeSpace()
	if path, ok := cleanHistoryPath(); ok {
		if err := appendCleanHistoryTo(path, RunRecord{Time: time.Now(), TotalBytes: result.FreedBytes, Purges: result.Records, Actions: result.Actions}); err != nil {
			result.Error = fmt.Sprintf("cleanup finished but audit history could not be saved: %v", err)
		}
	} else {
		result.Error = "cleanup finished but config directory is unavailable; audit history could not be saved"
	}
	return result
}

// Run executes the cleanup and prints a report.
func Run(opts Options) error { return run(defaultDeps, opts) }

func run(d deps, opts Options) error {
	if opts.ShowHistory {
		if opts.JSON {
			return fmt.Errorf("clean --history cannot be combined with --json; use --history for readable audit output")
		}
		ui.Header("CLEAN HISTORY")
		fmt.Print(renderCleanHistory(History()))
		return nil
	}

	home, err := d.homeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	opts, err = normalizedOptions(opts)
	if err != nil {
		return err
	}
	if opts.JSON && !opts.DryRun && !opts.Assume {
		return fmt.Errorf("clean --json requires --dry-run or --yes; preview before applying")
	}
	if opts.JSON {
		result := Apply(home, opts)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
		if result.Error != "" {
			return fmt.Errorf("%s", result.Error)
		}
		return nil
	}

	ui.Header("CROSS-PLATFORM DISK CLEANER")
	fmt.Printf("  OS: %s/%s   Home: %s\n", runtime.GOOS, runtime.GOARCH, home)
	fmt.Printf("  Selected: %s; temp min-age: %s\n", opts.Categories, opts.MinAge)
	ui.Warnf("%s", RiskWarning)
	fmt.Println("  Logs, Trash and tool commands are opt-in. Close affected applications; see vitals guide.")
	if opts.DryRun {
		ui.Warnf("dry-run: nothing will be deleted")
	} else if !opts.Assume {
		ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
		preview := Preview(ctx, home, opts)
		cancel()
		renderActions(preview)
		if preview.Error != "" {
			return fmt.Errorf("%s", preview.Error)
		}
		if !confirm(d.confirmReader) {
			ui.Infof("aborted")
			return nil
		}
	}

	result := Apply(home, opts)
	renderActions(result)

	ui.Header("SUMMARY")
	measured := ui.HumanBytes(result.FreedBytes)
	fmt.Println("  Logical file bytes only; not guaranteed physical space recovery. Empty directories are retained.")
	if opts.DryRun {
		fmt.Printf("  Would remove (measured)      : %s\n", measured)
	} else {
		fmt.Printf("  Removed (measured)           : %s\n", measured)
	}
	if !opts.DryRun && result.FreeBefore > 0 && result.FreeAfter > 0 {
		fmt.Printf("  Root filesystem free space   : %s -> %s\n",
			ui.HumanBytes(result.FreeBefore), ui.HumanBytes(result.FreeAfter))
	}
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}
	if opts.DryRun {
		ui.Okf("dry-run complete — re-run without --dry-run to apply")
	} else {
		ui.Okf("cleanup complete")
	}
	return nil
}

func renderActions(result Result) {
	for _, a := range result.Actions {
		size := ui.HumanBytes(a.Bytes)
		if !result.DryRun {
			size = ui.HumanBytes(a.Removed)
		}
		if a.Kind == "command" {
			size = "reclaim unknown"
		}
		fmt.Printf("  [%s] %s  %s  %s\n    %s\n", a.Category, a.Status, size, a.Target, a.Reason)
		if a.Preserved > 0 {
			fmt.Printf("    Preserved %d recent or non-regular files\n", a.Preserved)
		}
		for _, issue := range a.Issues {
			ui.Warnf("%s", issue)
		}
		for _, path := range a.Protected {
			fmt.Printf("    Protected: %s (models/data or manager-owned cache); use the owning tool or vitals explore to review.\n", path)
		}
		if a.Discovery != "" {
			fmt.Printf("    Discovery: %s\n", a.Discovery)
		}
		for _, path := range a.Locations {
			fmt.Printf("    Discovered cache: %s\n", path)
		}
	}

}

func confirm(r io.Reader) bool {
	fmt.Print(ui.Yellow + "Proceed at your own risk? No undo: selected files are deleted and selected tool commands run. Apply rescans, not the earlier preview. Continue? [y/N] " + ui.Reset)
	sc := bufio.NewScanner(r)
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
	cacheHome := filepath.Join(home, ".cache")
	if path := os.Getenv("XDG_CACHE_HOME"); filepath.IsAbs(path) {
		cacheHome = path
	}
	gradleHome := os.Getenv("GRADLE_USER_HOME")
	if !filepath.IsAbs(gradleHome) {
		gradleHome = filepath.Join(home, ".gradle")
	}
	cargoHome := os.Getenv("CARGO_HOME")
	if !filepath.IsAbs(cargoHome) {
		cargoHome = filepath.Join(home, ".cargo")
	}
	return []string{
		cacheHome,
		filepath.Join(gradleHome, "caches"),
		filepath.Join(cargoHome, "registry", "cache"),
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
			for _, browser := range []string{"Google", "Microsoft"} {
				product := "Chrome"
				if browser == "Microsoft" {
					product = "Edge"
				}
				matches, _ := filepath.Glob(filepath.Join(la, browser, product, "User Data", "*", "Cache"))
				dirs = append(dirs, matches...)
			}
		}
		return dirs
	default:
		return nil
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	result := Preview(ctx, home, Options{})
	for _, a := range result.Actions {
		if a.Bytes > 0 {
			entries = append(entries, CacheEntry{Path: a.Target, Bytes: a.Bytes})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Bytes > entries[j].Bytes })
	return entries, result.Complete
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
	if _, err := r.lookPath(name); err != nil {
		return
	}
	if r.opts.DryRun {
		fmt.Printf("    would run: %s %s\n", name, strings.Join(args, " "))
		return
	}
	fmt.Printf("    run: %s %s\n", name, strings.Join(args, " "))
	_ = r.runCmd(name, args...)
}
