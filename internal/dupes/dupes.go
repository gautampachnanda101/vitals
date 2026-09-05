// Package dupes finds byte-identical files under a directory tree — a space
// recovery axis vitals' cache-focused `clean` doesn't touch at all, since
// duplicate photos, videos and documents are a user's own data, not a
// regenerable cache. By default this package only ever reports; deletion is
// never automated. The one opt-in action it offers, --hardlink, destroys no
// data even if it's wrong — every path keeps working and keeps reading the
// same bytes, just sharing one inode instead of two.
package dupes

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vitals/internal/ui"
)

// deps is the live OS surface Run/applyHardlinksWithConfirmation read
// from, pulled out so a test can substitute fakes — same shape as
// internal/tools' deps struct (lookPath/runCmd/confirmReader/goos).
// defaultDeps wires the real calls; production code always goes through
// it via Run.
type deps struct {
	homeDir       func() (string, error)
	confirmReader io.Reader
}

var defaultDeps = deps{homeDir: os.UserHomeDir, confirmReader: os.Stdin}

// Options configures a scan.
type Options struct {
	Root     string // directory to scan; defaults to the home directory
	MinSize  int64  // ignore files smaller than this; defaults to 1 MiB
	Top      int    // duplicate groups to print; defaults to 20
	JSON     bool
	Output   string // if set, also write the full JSON result here
	Hardlink bool   // after reporting, replace duplicates with hardlinks to reclaim space
	Yes      bool   // skip the confirmation prompt before applying --hardlink
}

// Group is one set of byte-identical files.
type Group struct {
	SizeBytes int64    `json:"size_bytes"`
	Hash      string   `json:"sha256"`
	Paths     []string `json:"paths"`
}

// WastedBytes is what keeping only one copy of the group would reclaim.
func (g Group) WastedBytes() int64 { return g.SizeBytes * int64(len(g.Paths)-1) }

// Result is one scan's full findings.
type Result struct {
	Root         string  `json:"root"`
	ScannedFiles int64   `json:"scanned_files"`
	ScannedBytes int64   `json:"scanned_bytes"`
	Groups       []Group `json:"groups"`
	WastedBytes  int64   `json:"wasted_bytes"`
	// Truncated is true when the walk stopped early — its context was
	// cancelled, or a caller-supplied file budget (ScanContext) was hit —
	// before the whole tree was seen. Groups/WastedBytes above still
	// reflect everything found up to that point; this just says there
	// may have been more.
	Truncated bool `json:"truncated,omitempty"`
}

// skipDirNames are directories not worth walking into: version control,
// dependency/build trees regenerated from a manifest, and trash cans — none
// of these hold files a person would think of as "mine".
var skipDirNames = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "__pycache__": true,
	".cache": true, ".Trash": true, ".Trashes": true,
	"$RECYCLE.BIN": true, "System Volume Information": true,
}

const partialHashBytes = 64 * 1024

// Run scans Options.Root and prints (or emits as JSON) every group of
// byte-identical files at or above Options.MinSize. It never deletes
// anything — duplicates are a user's own data, unlike the regenerable caches
// `vitals clean` removes, so the right "fix" is left to the person reviewing
// the list, not an automated action.
func Run(opts Options) error { return run(defaultDeps, opts) }

func run(d deps, opts Options) error {
	if opts.Root == "" {
		home, err := d.homeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		opts.Root = home
	}
	if opts.MinSize <= 0 {
		opts.MinSize = 1 << 20
	}
	if opts.Top <= 0 {
		opts.Top = 20
	}

	result, err := Scan(opts.Root, opts.MinSize)
	if err != nil {
		return err
	}

	if opts.Output != "" {
		if err := writeResultToFile(opts.Output, result); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write --output file: %v\n", err)
		}
	}

	if !opts.JSON {
		render(result, opts.Top)
	}

	if opts.Hardlink && len(result.Groups) > 0 {
		if err := applyHardlinksWithConfirmation(d, result, opts.Yes); err != nil {
			return err
		}
	}

	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	return nil
}

// applyHardlinksWithConfirmation confirms (unless yes) then runs
// ApplyHardlinks, printing a summary — the same UX bar `vitals clean` holds
// its own destructive action to, even though hardlinking destroys nothing.
func applyHardlinksWithConfirmation(d deps, result Result, yes bool) error {
	fmt.Println()
	fmt.Printf("  %s hardlink %d duplicate group(s), reclaiming up to %s\n",
		ui.Bold+"about to"+ui.Reset, len(result.Groups), ui.HumanBytes(result.WastedBytes))
	if !yes && !confirmHardlink(d.confirmReader) {
		ui.Infof("aborted")
		return nil
	}
	linked, reclaimed, failures := ApplyHardlinks(result.Groups)
	fmt.Println()
	ui.Okf("hardlinked %d file(s), reclaiming %s", linked, ui.HumanBytes(reclaimed))
	for _, f := range failures {
		ui.Warnf("%s", f)
	}
	return nil
}

func confirmHardlink(r io.Reader) bool {
	fmt.Print(ui.Yellow + "This replaces duplicate files with hardlinks (no data is deleted). Continue? [y/N] " + ui.Reset)
	sc := bufio.NewScanner(r)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}

// writeResultToFile writes r as JSON to path, creating any missing parent
// directories — the same --output convention as `vitals doctor`.
func writeResultToFile(path string, r Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Scan walks root and returns every group of files sharing a full SHA-256
// hash, largest wasted-space group first. Only regular files at or above
// minSize are considered; a size-then-partial-hash prefilter keeps it from
// fully hashing files that can't possibly match. Unbounded — see
// ScanContext for a cancellable, budget-capped walk.
func Scan(root string, minSize int64) (Result, error) {
	return ScanContext(context.Background(), root, minSize, 0)
}

// ScanContext is Scan's context/budget-aware core. ctx is checked during
// the walk so a caller can bound how long a scan runs — an in-process,
// cancellable walk, unlike internal/clean's untimed subprocess calls (see
// docs/roadmap/items/005-dashboard-write-actions/design.md §7 finding 2,
// accepted there specifically because clean's calls are NOT cancellable
// the way this one is designed to be from the start). maxFiles caps how
// many regular files at-or-above minSize are considered before the walk
// stops early; <=0 means unlimited, Scan's own existing behavior. Either
// limit hit sets Result.Truncated, so a caller can tell "no duplicates"
// apart from "didn't get to see everything."
func ScanContext(ctx context.Context, root string, minSize int64, maxFiles int) (Result, error) {
	sizeGroups := map[int64][]string{}
	var scannedFiles, scannedBytes int64
	truncated := false

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			truncated = true
			return fs.SkipAll
		default:
		}
		if err != nil {
			return nil // unreadable entry (permissions, race) — skip, don't abort the scan
		}
		if d.IsDir() {
			if path != root && skipDirNames[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // never follow — avoids double-counting a link's target
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < minSize {
			return nil
		}
		if maxFiles > 0 && scannedFiles >= int64(maxFiles) {
			truncated = true
			return fs.SkipAll
		}
		scannedFiles++
		scannedBytes += info.Size()
		sizeGroups[info.Size()] = append(sizeGroups[info.Size()], path)
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	var groups []Group
	var wasted int64
	for size, paths := range sizeGroups {
		if len(paths) < 2 {
			continue // a unique size can't have a duplicate
		}
		for hash, confirmed := range confirmDuplicates(paths) {
			if len(confirmed) < 2 {
				continue
			}
			g := Group{SizeBytes: size, Hash: hash, Paths: confirmed}
			groups = append(groups, g)
			wasted += g.WastedBytes()
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].WastedBytes() > groups[j].WastedBytes() })

	return Result{Root: root, ScannedFiles: scannedFiles, ScannedBytes: scannedBytes, Groups: groups, WastedBytes: wasted, Truncated: truncated}, nil
}

// confirmDuplicates takes same-size candidates and returns them grouped by
// full-content hash. It first buckets by a cheap partial hash (the first 64
// KiB) so a full read — and the I/O that comes with it — only happens for
// files that already agree on both size and their leading bytes.
func confirmDuplicates(paths []string) map[string][]string {
	byPartial := map[string][]string{}
	for _, p := range paths {
		if h, err := hashPrefix(p, partialHashBytes); err == nil {
			byPartial[h] = append(byPartial[h], p)
		}
	}
	byFull := map[string][]string{}
	for _, candidates := range byPartial {
		if len(candidates) < 2 {
			continue
		}
		for _, p := range candidates {
			if h, err := hashFile(p); err == nil {
				byFull[h] = append(byFull[h], p)
			}
		}
	}
	return byFull
}

func hashPrefix(path string, n int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyN(h, f, n); err != nil && err != io.EOF {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func render(r Result, top int) {
	ui.Header("DUPLICATE FILES")
	fmt.Printf("  scanned %d files (%s) under %s\n", r.ScannedFiles, ui.HumanBytes(r.ScannedBytes), r.Root)

	if len(r.Groups) == 0 {
		fmt.Println()
		ui.Okf("no duplicate files found above the size threshold")
		return
	}

	fmt.Println()
	fmt.Printf("  %s duplicate group(s) — %s reclaimable by keeping one copy of each\n",
		ui.Actionf("%d", len(r.Groups)), ui.Actionf("%s", ui.HumanBytes(r.WastedBytes)))

	shown := 0
	for _, g := range r.Groups {
		if shown >= top {
			break
		}
		fmt.Println()
		fmt.Printf("  %s wasted — %d copies of a %s file\n",
			ui.Actionf("%s", ui.HumanBytes(g.WastedBytes())), len(g.Paths), ui.HumanBytes(g.SizeBytes))
		for _, p := range g.Paths {
			fmt.Printf("    %s\n", p)
		}
		shown++
	}
	if rest := len(r.Groups) - shown; rest > 0 {
		fmt.Println()
		fmt.Println(ui.Key(fmt.Sprintf("  ...and %d more group(s) — raise --top or use --json for the full list", rest)))
	}

	fmt.Println()
	ui.Warnf("nothing was deleted — vitals never removes your own files automatically")
	fmt.Println(ui.Key("  review the paths above and remove the copies you don't want"))
}
