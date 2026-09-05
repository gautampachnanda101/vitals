package dashboard

import (
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"vitals/internal/ui"
)

// The Disk page's "what's using space" detail. There's no per-process
// disk-I/O rate available cross-platform (zero on macOS via gopsutil —
// roadmap 012), so the disk-appropriate answer is by *path*: the
// biggest directories and files under the user's home, where a person's
// reclaimable space almost always is.
//
// This is a real filesystem walk, not a metric read, so it is: bounded
// hard by a wall-clock budget and a visited-entry cap (partial results
// beat a page that hangs); confined to $HOME (never a walk of system
// volumes); and cached for a good while because it is expensive.

const (
	diskScanBudget   = 2500 * time.Millisecond
	diskScanMaxEntry = 1_500_000
	diskScanWorkers  = 6
	diskScanCacheTTL = 90 * time.Second
	diskUsageTopN    = 10
)

type pathSize struct {
	Path string
	Size int64
}

type diskScanResult struct {
	Root      string
	Dirs      []pathSize // biggest immediate children of Root, by recursive size
	Files     []pathSize // biggest individual files anywhere under Root
	Entries   int
	Truncated bool // hit the budget or the entry cap before finishing
}

type diskUsageCache struct {
	ttl  time.Duration
	scan func() (diskScanResult, error)

	mu      sync.Mutex
	value   diskScanResult
	err     error
	expiry  time.Time
	loading chan struct{}
}

func newDiskUsageCache() *diskUsageCache {
	return &diskUsageCache{ttl: diskScanCacheTTL, scan: func() (diskScanResult, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return diskScanResult{}, err
		}
		return scanBiggestPaths(home), nil
	}}
}

var defaultDiskUsageCache = newDiskUsageCache()

func (c *diskUsageCache) Get() (diskScanResult, error) {
	c.mu.Lock()
	if time.Now().Before(c.expiry) {
		v, err := c.value, c.err
		c.mu.Unlock()
		return v, err
	}
	if c.loading != nil {
		ch := c.loading
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		v, err := c.value, c.err
		c.mu.Unlock()
		return v, err
	}
	ch := make(chan struct{})
	c.loading = ch
	c.mu.Unlock()

	v, err := c.scan()

	c.mu.Lock()
	c.value, c.err = v, err
	c.expiry = time.Now().Add(c.ttl)
	c.loading = nil
	c.mu.Unlock()
	close(ch)

	return v, err
}

// scanBiggestPaths sizes each immediate child of root by walking the
// children concurrently under one shared wall-clock budget — so a large
// early-alphabetical directory can't starve the rest, which a single
// lexical WalkDir would do. It also tracks the largest individual files
// seen. Results are partial (Truncated) if the budget or the global
// entry cap is hit first.
func scanBiggestPaths(root string) diskScanResult {
	deadline := time.Now().Add(diskScanBudget)
	res := diskScanResult{Root: root}

	kids, err := os.ReadDir(root)
	if err != nil {
		return res
	}

	var (
		mu        sync.Mutex
		entries   atomic.Int64
		truncated atomic.Bool
		files     []pathSize // ascending by size, capped at diskUsageTopN
	)
	consider := func(p string, sz int64) {
		if len(files) < diskUsageTopN {
			files = append(files, pathSize{p, sz})
		} else if sz > files[0].Size {
			files[0] = pathSize{p, sz}
		} else {
			return
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Size < files[j].Size })
	}

	sem := make(chan struct{}, diskScanWorkers)
	var wg sync.WaitGroup
	dirTotals := make([]pathSize, len(kids))

	for i, k := range kids {
		full := filepath.Join(root, k.Name())
		dirTotals[i].Path = full
		if !k.IsDir() {
			if info, err := k.Info(); err == nil {
				mu.Lock()
				dirTotals[i].Size = info.Size()
				consider(full, info.Size())
				mu.Unlock()
			}
			continue
		}
		wg.Add(1)
		go func(idx int, dir string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var sum int64
			_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					if d != nil && d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				if entries.Add(1) > diskScanMaxEntry || time.Now().After(deadline) {
					truncated.Store(true)
					return filepath.SkipAll
				}
				if d.IsDir() {
					return nil
				}
				if info, err := d.Info(); err == nil {
					sum += info.Size()
					mu.Lock()
					consider(p, info.Size())
					mu.Unlock()
				}
				return nil
			})
			mu.Lock()
			dirTotals[idx].Size = sum
			mu.Unlock()
		}(i, full)
	}
	wg.Wait()

	res.Entries = int(entries.Load())
	res.Truncated = truncated.Load()

	sort.Slice(dirTotals, func(i, j int) bool { return dirTotals[i].Size > dirTotals[j].Size })
	for _, d := range dirTotals {
		if d.Size == 0 {
			break
		}
		res.Dirs = append(res.Dirs, d)
		if len(res.Dirs) >= diskUsageTopN {
			break
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	res.Files = files
	return res
}

// biggestPathsSection renders the Disk page's "Biggest directories" /
// "Biggest files" tables. "" when the scan couldn't run (no home dir,
// etc.) so the section just doesn't appear.
func biggestPathsSection() string {
	res, err := defaultDiskUsageCache.Get()
	if err != nil || (len(res.Dirs) == 0 && len(res.Files) == 0) {
		return ""
	}

	out := `<div class="sectiontitle">Biggest directories in your home folder</div>`
	if res.Truncated {
		out += fmt.Sprintf(`<p class="caption">Partial — scan stopped after %s entries. Largest of what was scanned.</p>`,
			formatCount(res.Entries))
	}
	out += card(mustExecute(pathTableTmpl, toPathRows(res.Dirs, res.Root)))

	if len(res.Files) > 0 {
		out += `<div class="sectiontitle">Biggest single files</div>` +
			card(mustExecute(pathTableTmpl, toPathRows(res.Files, res.Root)))
	}
	return out
}

type pathRow struct {
	Path string
	Size string
}

func toPathRows(ps []pathSize, root string) []pathRow {
	rows := make([]pathRow, len(ps))
	for i, p := range ps {
		disp := p.Path
		if rel, err := filepath.Rel(root, p.Path); err == nil && rel != "." {
			disp = "~" + string(os.PathSeparator) + rel
		}
		rows[i] = pathRow{Path: disp, Size: ui.HumanBytes(p.Size)}
	}
	return rows
}

func formatCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

var pathTableTmpl = template.Must(template.New("paths").Parse(`<table style="width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--line);border-radius:10px;overflow:hidden;font-size:.86rem">` +
	`<tr style="background:var(--surface-2)">` +
	`<th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Path</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Size</th></tr>` +
	`{{range .}}<tr>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line)" class="mono">{{.Path}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.Size}}</td>` +
	`</tr>{{end}}</table>`))
