package dashboard

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vitals/internal/diskusage"
	"vitals/internal/ui"
)

const (
	diskScanCacheTTL        = 10 * time.Minute
	diskScanDrainGrace      = 2 * time.Second
	diskScanCacheMaxEntries = 64
)

var (
	errDiskScanBusy    = errors.New("another disk scan is still running")
	errDiskScanTimeout = errors.New("scan budget elapsed; filesystem operations may still be returning")
)

type diskScanKey struct {
	root    string
	options diskusage.Options
}

type diskCachedResult struct {
	value  diskusage.Result
	err    error
	expiry time.Time
	used   uint64
}

type diskFlight struct {
	key     diskScanKey
	done    chan struct{}
	started time.Time
	value   diskusage.Result
	err     error
}

type diskUsageCache struct {
	drainGrace time.Duration
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
	home       func() (string, error)
	scan       func(context.Context, string, diskusage.Options) (diskusage.Result, error)

	mu     sync.Mutex
	values map[diskScanKey]diskCachedResult
	active *diskFlight
	serial uint64
}

func (c *diskUsageCache) maxCap() int {
	if c.maxEntries > 0 {
		return c.maxEntries
	}
	return diskScanCacheMaxEntries
}

func newDiskUsageCache() *diskUsageCache {
	return &diskUsageCache{
		ttl: diskScanCacheTTL, now: time.Now, home: os.UserHomeDir,
		drainGrace: diskScanDrainGrace, maxEntries: diskScanCacheMaxEntries,
		scan: scanHomeDirectory, values: make(map[diskScanKey]diskCachedResult),
	}
}

var defaultDiskUsageCache = newDiskUsageCache()

type diskRootOps struct {
	resolve func(string) (string, error)
	open    func(string) (*os.Root, error)
	lstat   func(*os.Root, string) (os.FileInfo, error)
	stat    func(*os.Root) (os.FileInfo, error)
	child   func(*os.Root, string) (*os.Root, error)
}

var liveDiskRootOps = diskRootOps{
	resolve: func(home string) (string, error) {
		resolved, err := filepath.EvalSymlinks(home)
		if err != nil {
			return "", err
		}
		return filepath.Abs(resolved)
	},
	open:  os.OpenRoot,
	lstat: func(root *os.Root, name string) (os.FileInfo, error) { return root.Lstat(name) },
	stat:  func(root *os.Root) (os.FileInfo, error) { return root.Stat(".") },
	child: func(root *os.Root, name string) (*os.Root, error) { return root.OpenRoot(name) },
}

// A request cancels only its own wait. Shared work owns its budget and the
// admission slot until Scan actually returns, even if a syscall ignores it.
func (c *diskUsageCache) Get(ctx context.Context, root string, opts diskusage.Options, refresh bool) (diskusage.Result, error) {
	if err := ctx.Err(); err != nil {
		return diskusage.Result{}, err
	}
	key := diskScanKey{root, opts}
	c.mu.Lock()
	now := c.now()
	for k, v := range c.values {
		if !now.Before(v.expiry) {
			delete(c.values, k)
		}
	}
	if v, ok := c.values[key]; ok && !refresh {
		c.serial++
		v.used = c.serial
		c.values[key] = v
		c.mu.Unlock()
		return v.value, v.err
	}
	f := c.active
	if f != nil && f.key != key {
		c.mu.Unlock()
		return diskusage.Result{}, errDiskScanBusy
	}
	if f == nil {
		f = &diskFlight{key: key, done: make(chan struct{}), started: now}
		c.active = f
		// Capture the seam under the lock; an active scan never gets replaced.
		scan := c.scan
		go c.run(f, scan)
	}
	// Cancellation starts worker shutdown; aggregation finishes shortly after
	// the budget. Do not race the scanner's own deadline and discard its rows.
	remaining := opts.Budget + c.drainGrace - now.Sub(f.started)
	c.mu.Unlock()

	timer := time.NewTimer(max(remaining, 0))
	defer timer.Stop()
	select {
	case <-f.done:
		return f.value, f.err
	case <-ctx.Done():
		return diskusage.Result{}, ctx.Err()
	case <-timer.C:
		// Prefer a result already published at the wait deadline.
		select {
		case <-f.done:
			return f.value, f.err
		default:
			return diskusage.Result{}, errDiskScanTimeout
		}
	}
}

func (c *diskUsageCache) run(f *diskFlight, scan func(context.Context, string, diskusage.Options) (diskusage.Result, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), f.key.options.Budget)
	defer cancel()
	value, err := scan(ctx, f.key.root, f.key.options)
	c.mu.Lock()
	defer c.mu.Unlock()
	f.value, f.err = value, err
	if _, exists := c.values[f.key]; !exists && len(c.values) >= c.maxCap() {
		var oldest diskScanKey
		var used uint64 = ^uint64(0)
		for k, v := range c.values {
			if v.used < used {
				oldest, used = k, v.used
			}
		}
		delete(c.values, oldest)
	}
	c.serial++
	c.values[f.key] = diskCachedResult{value: value, err: err, expiry: c.now().Add(c.ttl), used: c.serial}
	c.active = nil
	close(f.done)
}

// Latest returns the most recently used successful cached disk scan result,
// or nil if none is currently valid.
func (c *diskUsageCache) Latest() *diskusage.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	var best *diskusage.Result
	var bestUsed uint64
	for _, v := range c.values {
		if now.Before(v.expiry) && v.err == nil && v.value.Entries > 0 {
			if best == nil || v.used > bestUsed {
				val := v.value
				best = &val
				bestUsed = v.used
			}
		}
	}
	return best
}

// Put records a completed disk scan result in the cache.
func (c *diskUsageCache) Put(root string, opts diskusage.Options, res diskusage.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := diskScanKey{root, opts}
	c.serial++
	c.values[key] = diskCachedResult{
		value:  res,
		expiry: c.now().Add(c.ttl),
		used:   c.serial,
	}
}

// Only the trusted home is canonicalized. Requested child paths are never
// EvalSymlinks'ed: every descent is rooted, non-symlink and identity checked.
func confinedDiskRoot(home, requested string) (string, *os.Root, os.FileInfo, error) {
	return confinedDiskRootWith(home, requested, liveDiskRootOps)
}

func confinedDiskRootWith(home, requested string, ops diskRootOps) (string, *os.Root, os.FileInfo, error) {
	canonicalHome, err := ops.resolve(home)
	if err != nil {
		return "", nil, nil, err
	}
	selected := requested
	if selected == "" {
		selected = canonicalHome
	} else if !filepath.IsAbs(selected) {
		selected = filepath.Join(canonicalHome, selected)
	}
	selected = filepath.Clean(selected)
	rel, err := filepath.Rel(canonicalHome, selected)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", nil, nil, errors.New("choose a real directory inside your home folder")
	}
	anchor := filepath.VolumeName(canonicalHome) + string(filepath.Separator)
	handle, err := ops.open(anchor)
	if err != nil {
		return "", nil, nil, err
	}
	homeParts := strings.Split(strings.TrimPrefix(canonicalHome, anchor), string(filepath.Separator))
	parts := homeParts
	if rel != "." {
		parts = append(parts, strings.Split(rel, string(filepath.Separator))...)
	}
	var opened os.FileInfo
	for _, part := range parts {
		if part == "" {
			continue
		}
		info, statErr := ops.lstat(handle, part)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			handle.Close()
			return "", nil, nil, errors.New("choose an accessible real directory; symlink drill-down is not supported")
		}
		child, openErr := ops.child(handle, part)
		if openErr != nil {
			handle.Close()
			return "", nil, nil, openErr
		}
		opened, err = ops.stat(child)
		after, afterErr := ops.lstat(handle, part)
		handle.Close()
		if err != nil || afterErr != nil || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, opened) || !os.SameFile(info, after) {
			child.Close()
			return "", nil, nil, errors.New("directory changed while opening; retry with a real directory")
		}
		handle = child
	}
	if opened == nil {
		opened, err = ops.stat(handle)
		if err != nil {
			handle.Close()
			return "", nil, nil, err
		}
	}
	return selected, handle, opened, nil
}

func scanHomeDirectory(ctx context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
	return scanHomeDirectoryWith(ctx, root, opts, os.UserHomeDir, os.Lstat, diskusage.ScanRoot)
}

func scanHomeDirectoryWith(ctx context.Context, root string, opts diskusage.Options,
	homeDir func() (string, error), stat func(string) (os.FileInfo, error),
	scan func(context.Context, *os.Root, string, diskusage.Options) (diskusage.Result, error)) (diskusage.Result, error) {
	home, err := homeDir()
	if err != nil {
		return diskusage.Result{}, err
	}
	selected, handle, identity, err := confinedDiskRoot(home, root)
	if err != nil {
		return diskusage.Result{}, err
	}
	defer handle.Close()
	current, err := stat(selected)
	if err != nil || !os.SameFile(identity, current) {
		return diskusage.Result{}, errors.New("scan directory changed; select a real directory and retry")
	}
	result, err := scan(ctx, handle, selected, opts)
	current, statErr := stat(selected)
	if statErr != nil || !os.SameFile(identity, current) {
		return diskusage.Result{}, errors.New("scan directory changed; select a real directory and retry")
	}
	return result, err
}

func init() {
	RegisterAsync(AsyncFragment{Path: "/disk/usage", Handler: renderDiskUsage, SameOrigin: true})
}

func renderDiskUsage(ctx PageContext) (int, string) {
	if ctx.Request != nil {
		if status := protectedFragmentStatus(ctx.Request); status != http.StatusOK {
			return status, diskRecovery("Cross-origin or unsupported disk scan request rejected. Open the Disk page on this dashboard.", "")
		}
	}
	query := url.Values{}
	requestCtx := context.Background()
	if ctx.Request != nil {
		query = ctx.Request.URL.Query()
		requestCtx = ctx.Request.Context()
	}
	opts := diskusage.DefaultOptions()
	for _, key := range []string{"root", "top", "budget", "rescan"} {
		if len(query[key]) > 1 {
			return http.StatusBadRequest, diskRecovery("Provide each disk scan option only once.", "")
		}
	}
	var err error
	if text := query.Get("top"); text != "" {
		opts.Top, err = strconv.Atoi(text)
		if err != nil {
			return http.StatusBadRequest, diskRecovery("Top must be a number from 1 to 100.", "")
		}
	}
	if text := query.Get("budget"); text != "" {
		opts.Budget, err = time.ParseDuration(text)
		if err != nil {
			return http.StatusBadRequest, diskRecovery("Choose a scan budget such as 30s or 1m, no more than 5m.", "")
		}
	}
	if err = diskusage.Validate(opts); err != nil {
		return http.StatusBadRequest, diskRecovery(err.Error(), "")
	}
	refresh := query.Get("rescan")
	if refresh != "" && refresh != "1" {
		return http.StatusBadRequest, diskRecovery("Rescan must be 1 when requested.", "")
	}
	home, err := defaultDiskUsageCache.home()
	if err != nil {
		return http.StatusInternalServerError, diskRecovery("Cannot locate your home folder: "+err.Error(), "")
	}
	root, handle, _, err := confinedDiskRoot(home, query.Get("root"))
	if err != nil {
		return http.StatusBadRequest, diskRecovery("Choose a real directory inside your home folder. "+err.Error(), "")
	}
	handle.Close()
	home, _ = filepath.EvalSymlinks(home)
	home, _ = filepath.Abs(home)
	rel, _ := filepath.Rel(home, root)
	retry := diskURL(rel, opts, true)
	res, err := defaultDiskUsageCache.Get(requestCtx, root, opts, refresh == "1")
	if err != nil {
		switch {
		case errors.Is(err, errDiskScanBusy), errors.Is(err, errDiskScanTimeout):
			return http.StatusServiceUnavailable, diskRecovery(err.Error()+". Wait for the active scan to return, then retry; narrow the scope for faster results. Budgets are cooperative, not a hard syscall timeout.", retry)
		case errors.Is(err, context.Canceled):
			return http.StatusRequestTimeout, diskRecovery("This request stopped waiting. The shared scan retains its slot until it returns; retry shortly.", retry)
		default:
			return http.StatusInternalServerError, diskRecovery("Disk scan failed: "+err.Error()+". Review directory access or choose a narrower real directory; do not run the dashboard as root.", retry)
		}
	}
	return http.StatusOK, renderDiskResult(res, home, root, opts)
}

func diskURL(rel string, opts diskusage.Options, refresh bool) string {
	q := url.Values{"root": {filepath.ToSlash(rel)}, "top": {strconv.Itoa(opts.Top)}, "budget": {opts.Budget.String()}}
	if refresh {
		q.Set("rescan", "1")
	}
	return "/disk/usage?" + q.Encode()
}

func diskRecovery(message, retry string) string {
	if retry == "" {
		retry = diskURL(".", diskusage.DefaultOptions(), true)
	}
	link, _ := url.Parse(retry)
	query := link.Query()
	return mustExecute(diskRecoveryTmpl, struct{ Message, Retry, Root, Top, Budget string }{
		diskusage.DisplayPath(message), retry, query.Get("root"), query.Get("top"), query.Get("budget"),
	})
}

type diskLink struct{ Label, URL, Root string }
type pathRow struct {
	Path, Size, URL, Root, Advice string
	Partial                       bool
}

func pathAdvice(p string) string {
	lower := strings.ToLower(filepath.ToSlash(p))
	switch {
	case strings.Contains(lower, "docker.raw") || strings.Contains(lower, "com.docker.docker") || strings.Contains(lower, ".docker"):
		return "Docker virtual disk or data. Sparse file allocated size is often smaller than logical ceiling. Reclaim space using `docker system df` to check allocations, `docker system prune` for stopped containers/unused images, and `docker volume prune` for orphaned volumes."
	case strings.Contains(lower, ".ollama") || strings.Contains(lower, "huggingface") || strings.Contains(lower, "lm-studio") || (strings.Contains(lower, "models") && !strings.Contains(lower, "cache")):
		return "Local LLM model weights. Managed by the runtime or model manager; prune unused models using `ollama rm <model>` or your model UI rather than deleting files directly."
	case strings.Contains(lower, ".cache") || strings.Contains(lower, "caches") || strings.Contains(lower, "library/caches"):
		return "Application cache. Usually safe to clean; inspect with `vitals clean --dry-run` or the Clean page."
	case strings.Contains(lower, ".trash") || strings.Contains(lower, "$recycle.bin"):
		return "Recoverable deleted files in Trash/Recycle Bin; clean via the Clean page or system trash."
	default:
		return "Manual review only; use the owning application's storage management for personal, model or VM data. Large does not mean disposable."
	}
}

func diskRows(rows []diskusage.Row, home string, opts diskusage.Options, dirs bool) []pathRow {
	ordered := append([]diskusage.Row(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Bytes == ordered[j].Bytes {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Bytes > ordered[j].Bytes
	})
	out := make([]pathRow, 0, len(ordered))
	for _, row := range ordered {
		rel, err := filepath.Rel(home, row.Path)
		display := filepath.ToSlash(row.Path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			display = "~/" + filepath.ToSlash(rel)
		}
		link := ""
		if dirs && err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			link = diskURL(rel, opts, false)
		}
		advice := pathAdvice(row.Path)
		out = append(out, pathRow{Path: diskusage.DisplayPath(display), Size: ui.HumanBytes(row.Bytes), URL: link, Root: filepath.ToSlash(rel), Advice: advice, Partial: !row.Complete})
	}
	return out
}

type donutSlice struct {
	Label      string
	Size       string
	PercentStr string
	Color      string
	Dash       string
	Gap        string
	Offset     string
}

type donutChart struct {
	Title     string
	TotalSize string
	Slices    []donutSlice
	HasData   bool
}

const donutCircumference = 339.292

var chartPalette = []string{
	"#3b82f6", // blue
	"#10b981", // emerald
	"#f59e0b", // amber
	"#8b5cf6", // purple
	"#ec4899", // pink
	"#06b6d4", // cyan
}

func buildDonutChart(title string, rows []diskusage.Row, denominator int64) donutChart {
	if len(rows) == 0 || denominator <= 0 {
		return donutChart{Title: title, HasData: false}
	}
	ordered := append([]diskusage.Row(nil), rows...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Bytes == ordered[j].Bytes {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Bytes > ordered[j].Bytes
	})

	maxItems := 5
	if len(ordered) < maxItems {
		maxItems = len(ordered)
	}

	var topSum int64
	for i := 0; i < maxItems; i++ {
		topSum += ordered[i].Bytes
	}
	if topSum <= 0 {
		return donutChart{Title: title, HasData: false}
	}

	effectiveTotal := denominator
	if topSum > effectiveTotal {
		effectiveTotal = topSum
	}

	var slices []donutSlice
	var cumLength float64
	for i := 0; i < maxItems; i++ {
		row := ordered[i]
		if row.Bytes <= 0 {
			continue
		}
		pct := float64(row.Bytes) / float64(effectiveTotal)
		dash := pct * donutCircumference
		gap := donutCircumference - dash
		offset := -cumLength
		cumLength += dash

		base := filepath.Base(row.Path)
		if base == "" || base == "." || base == string(filepath.Separator) {
			base = row.Path
		}
		clean := diskusage.DisplayPath(base)
		r := []rune(clean)
		if len(r) > 22 {
			clean = string(r[:19]) + "…"
		}
		label := clean

		color := chartPalette[i%len(chartPalette)]
		slices = append(slices, donutSlice{
			Label:      label,
			Size:       ui.HumanBytes(row.Bytes),
			PercentStr: fmt.Sprintf("%.1f%%", pct*100),
			Color:      color,
			Dash:       fmt.Sprintf("%.1f", dash),
			Gap:        fmt.Sprintf("%.1f", gap),
			Offset:     fmt.Sprintf("%.1f", offset),
		})
	}

	remBytes := effectiveTotal - topSum
	if remBytes > 0 && float64(remBytes)/float64(effectiveTotal) >= 0.01 {
		pct := float64(remBytes) / float64(effectiveTotal)
		dash := pct * donutCircumference
		gap := donutCircumference - dash
		offset := -cumLength

		slices = append(slices, donutSlice{
			Label:      "Other",
			Size:       ui.HumanBytes(remBytes),
			PercentStr: fmt.Sprintf("%.1f%%", pct*100),
			Color:      "#64748b",
			Dash:       fmt.Sprintf("%.1f", dash),
			Gap:        fmt.Sprintf("%.1f", gap),
			Offset:     fmt.Sprintf("%.1f", offset),
		})
	}

	return donutChart{
		Title:     title,
		TotalSize: ui.HumanBytes(effectiveTotal),
		Slices:    slices,
		HasData:   len(slices) > 0,
	}
}

func renderDiskResult(res diskusage.Result, home, root string, opts diskusage.Options) string {
	rel, _ := filepath.Rel(home, root)
	crumbs := []diskLink{{"Home", diskURL(".", opts, false), "."}}
	up := ""
	if rel != "." {
		parent := filepath.Dir(rel)
		up = diskURL(parent, opts, false)
		parts := strings.Split(rel, string(filepath.Separator))
		for i, part := range parts {
			crumbRoot := filepath.Join(parts[:i+1]...)
			crumbs = append(crumbs, diskLink{diskusage.DisplayPath(part), diskURL(crumbRoot, opts, false), filepath.ToSlash(crumbRoot)})
		}
	}
	state := "Complete — measured all accessible regular files in this scope."
	if !res.Complete {
		state = "Partial — largest observed paths only. Incomplete rows are lower bounds; narrow the root or increase the budget and rescan."
	}
	if res.ErrorCount > 0 {
		state += " Review the access errors below; do not run the dashboard as root."
	}
	empty := ""
	if res.ObservedBytes == 0 {
		empty = "No regular-file bytes observed yet — this is not proof the scope has no files. Review errors, narrow the scope or increase the budget."
		if res.Complete && res.ErrorCount == 0 {
			empty = "No regular-file bytes found in this scope. Empty directories and zero-byte files consume no measured logical file bytes."
		}
	}
	issues := append([]diskusage.Issue(nil), res.Errors...)
	for i := range issues {
		issues[i].Path = diskusage.DisplayPath(issues[i].Path)
		issues[i].Message = diskusage.DisplayPath(issues[i].Message)
	}
	reasons := make([]string, len(res.StopReasons))
	for i, reason := range res.StopReasons {
		reasons[i] = diskusage.DisplayPath(reason)
	}
	title := "Biggest directories"
	if !res.Complete {
		title = "Largest observed directories"
	}

	var dirTotal int64
	for _, d := range res.Directories {
		dirTotal += d.Bytes
	}
	if res.ObservedBytes > dirTotal {
		dirTotal = res.ObservedBytes
	}
	dirChart := buildDonutChart("Folders", res.Directories, dirTotal)

	var fileTotal int64
	for _, f := range res.Files {
		fileTotal += f.Bytes
	}
	fileChart := buildDonutChart("Files", res.Files, fileTotal)

	return mustExecute(diskResultTmpl, struct {
		Root, RelativeRoot, State, Empty, Entries, Observed, Title, Rescan, Up, Budget string
		Crumbs                                                                         []diskLink
		Dirs, Files, RootFiles                                                         []pathRow
		Errors                                                                         []diskusage.Issue
		Reasons                                                                        []string
		Result                                                                         diskusage.Result
		Top                                                                            int
		DirChart                                                                       donutChart
		FileChart                                                                      donutChart
	}{
		Root: diskusage.DisplayPath(root), RelativeRoot: filepath.ToSlash(rel), State: state, Empty: empty, Entries: formatCount(res.Entries),
		Observed: ui.HumanBytes(res.ObservedBytes), Title: title, Rescan: diskURL(rel, opts, true), Up: up,
		Budget: opts.Budget.String(), Top: opts.Top, Crumbs: crumbs,
		Dirs: diskRows(res.Directories, home, opts, true), Files: diskRows(res.Files, home, opts, false),
		RootFiles: diskRows(res.RootFiles, home, opts, false), Errors: issues, Reasons: reasons, Result: res,
		DirChart: dirChart, FileChart: fileChart,
	})
}

func biggestPathsSection() string {
	return `<div class="sectiontitle">What's using space in your home folder</div>
<div class="card" id="disk-usage" aria-live="polite"><p>Loading read-only disk analysis… Capacity information above is ready now.</p></div>` + diskUsageScript
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

var diskRecoveryTmpl = template.Must(template.New("disk-recovery").Parse(
	`<p class="unavailable">{{.Message}}</p><a data-disk-scan href="{{.Retry}}">Retry disk analysis</a> · <a data-disk-scan href="/disk/usage">Return to home scope</a> · <span>Run <code>vitals guide</code> for more detail.</span>
<form id="disk-scope-options"><label>Scope inside home <input name="root" value="{{.Root}}" placeholder="e.g. Downloads" aria-label="Scope inside home"></label>
<label>Top <input name="top" type="number" min="1" max="100" value="{{.Top}}"></label>
<label>Scan budget <input name="budget" value="{{.Budget}}" aria-label="Scan budget (up to 5m)"></label>
<button type="submit">Scan narrower scope</button><p class="caption">Use a relative real directory inside your home; symlinks are not supported. No deletion is performed.</p></form>`))

var diskResultTmpl = template.Must(template.New("disk-result").Parse(`
<nav aria-label="Disk scan breadcrumbs">{{range .Crumbs}}<a data-disk-scan data-disk-root="{{.Root}}" href="{{.URL}}">{{.Label}}</a> / {{end}}
{{if .Up}}<a data-disk-scan href="{{.Up}}">Up</a> · {{end}}<button id="disk-rescan" type="button" data-disk-scan data-disk-url="{{.Rescan}}">Rescan this scope</button></nav>
<p class="mono">Scope: {{.Root}}</p><p>{{.State}}</p>
<p class="caption">{{.Observed}} observed · {{.Entries}} entries · {{.Result.ElapsedMS}} ms · {{.Result.ErrorCount}} access errors ·
{{.Result.SkippedSymlinks}} symlinks skipped · {{.Result.SkippedSpecial}} special files skipped · {{.Result.SkippedMounts}} mounts skipped.
Top {{.Top}} · budget {{.Budget}} · cached up to 10m. Navigating the folder tree uses cached results without rescanning.</p>
{{if not .Result.BoundaryDetectionAvailable}}<p class="caption">Boundary detection unavailable — filesystem-exclusive accounting cannot be confirmed.</p>{{end}}
{{if .Reasons}}<p>Stopped: {{range .Reasons}}{{.}}; {{end}}</p>{{end}}
<form id="disk-scan-options"><label>Scope inside home <input name="root" value="{{.RelativeRoot}}" placeholder="e.g. Downloads" aria-label="Scope inside home"></label>
<label>Top <input name="top" type="number" min="1" max="100" value="{{.Top}}"></label>
<label>Scan budget <input name="budget" value="{{.Budget}}" aria-label="Scan budget (up to 5m)"></label><button type="submit">Rescan with these limits</button></form>
<p class="caption">Sizes are logical bytes per path, not guaranteed reclaim. Hardlinks, sparse files, APFS clones/compression and snapshots can differ from allocated disk use.
Never sum rows from different levels. Home scans exclude system data, other users and snapshots.</p>
<p>Supported caches: <a href="/clean">open cleanup preview</a> and review candidates before taking action.
For personal files, models and VM/application data, use the owning application's storage management; never delete blobs based on this ranking.</p>
{{if .Empty}}<p>{{.Empty}}</p>{{end}}
{{if or .DirChart.HasData .FileChart.HasData}}
<div style="display:grid;grid-template-columns:repeat(auto-fit, minmax(280px, 1fr));gap:1rem;margin:1.25rem 0">
  {{if .DirChart.HasData}}
  <div class="card" style="display:flex;align-items:center;gap:1.25rem;padding:.75rem 1rem">
    <div style="flex:0 0 130px;display:flex;flex-direction:column;align-items:center">
      {{template "donut" .DirChart}}
      <span class="caption" style="margin-top:.25rem;font-weight:600">{{.DirChart.Title}}</span>
    </div>
    <div style="flex:1;min-width:140px">
      {{template "chartLegend" .DirChart}}
    </div>
  </div>
  {{end}}
  {{if .FileChart.HasData}}
  <div class="card" style="display:flex;align-items:center;gap:1.25rem;padding:.75rem 1rem">
    <div style="flex:0 0 130px;display:flex;flex-direction:column;align-items:center">
      {{template "donut" .FileChart}}
      <span class="caption" style="margin-top:.25rem;font-weight:600">{{.FileChart.Title}}</span>
    </div>
    <div style="flex:1;min-width:140px">
      {{template "chartLegend" .FileChart}}
    </div>
  </div>
  {{end}}
</div>
{{end}}
{{if .Dirs}}<div class="sectiontitle">{{.Title}}</div>{{template "rows" .Dirs}}{{end}}
{{if .RootFiles}}<div class="sectiontitle">Files directly under this scope</div>{{template "rows" .RootFiles}}{{end}}
{{if .Files}}<div class="sectiontitle">Biggest single files — largest observed when partial</div>{{template "rows" .Files}}{{end}}
{{if .Errors}}<div class="sectiontitle">Access errors (bounded samples)</div><ul>{{range .Errors}}<li class="mono">{{.Path}}: {{.Message}}</li>{{end}}</ul>{{end}}
{{define "donut"}}<svg viewBox="0 0 160 160" width="130" height="130" role="img" aria-label="{{.Title}} chart">
  <circle cx="80" cy="80" r="54" fill="none" stroke="var(--line)" stroke-width="20" opacity="0.4" />
  {{range .Slices}}
  <circle cx="80" cy="80" r="54" fill="none" stroke="{{.Color}}" stroke-width="20"
    stroke-dasharray="{{.Dash}} {{.Gap}}" stroke-dashoffset="{{.Offset}}"
    transform="rotate(-90 80 80)">
    <title>{{.Label}}: {{.Size}} ({{.PercentStr}})</title>
  </circle>
  {{end}}
  <text x="80" y="76" text-anchor="middle" font-size="10" fill="var(--text-dim)">Total</text>
  <text x="80" y="92" text-anchor="middle" font-size="12" font-weight="bold" fill="var(--text)">{{.TotalSize}}</text>
</svg>{{end}}
{{define "chartLegend"}}<table style="width:100%;border-collapse:collapse;font-size:.78rem">
  <tbody>
    {{range .Slices}}
    <tr>
      <td style="padding:2px 4px;width:12px;vertical-align:middle">
        <span style="display:inline-block;width:10px;height:10px;border-radius:2px;background:{{.Color}}"></span>
      </td>
      <td style="padding:2px 4px;max-width:140px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;vertical-align:middle" title="{{.Label}}">
        <span class="mono">{{.Label}}</span>
      </td>
      <td style="padding:2px 4px;text-align:right;white-space:nowrap;vertical-align:middle" class="mono">{{.Size}}</td>
      <td style="padding:2px 4px;text-align:right;white-space:nowrap;color:var(--text-dim);vertical-align:middle" class="mono">{{.PercentStr}}</td>
    </tr>
    {{end}}
  </tbody>
</table>{{end}}
{{define "rows"}}<table style="width:100%;border-collapse:collapse;font-size:.86rem"><thead><tr><th style="text-align:left">Path / next step</th><th style="text-align:right">Logical size</th></tr></thead>
<tbody>{{range .}}<tr><td style="padding:.5rem;border-top:1px solid var(--line)"><span class="mono">{{.Path}}</span>
{{if .URL}} · <a data-disk-scan data-disk-root="{{.Root}}" href="{{.URL}}">Drill down</a>{{else}}<p class="caption">{{.Advice}}</p>{{end}}</td>
<td class="mono" style="text-align:right;white-space:nowrap">{{.Size}}{{if .Partial}} <span class="caption">lower bound (partial)</span>{{end}}</td></tr>{{end}}</tbody></table>{{end}}
`))

const diskUsageScript = `<script>
(function(){
  var el = document.getElementById('disk-usage');
  var controller, current = '/disk/usage', sequence = 0;
  function load(href) {
    var url = new URL(href, location.href);
    if (url.origin !== location.origin || url.pathname !== '/disk/usage') return;
    if (controller) controller.abort();
    controller = new AbortController();
    var mine = ++sequence;
    current = url.pathname + url.search;
    el.innerHTML = '<p>Loading read-only disk analysis… Other dashboard pages remain available.</p>';
    fetch(current, {signal: controller.signal, credentials: 'same-origin'}).then(function(r) {
      return r.text();
    }).then(function(html) {
      if (mine === sequence) el.innerHTML = html;
    }).catch(function(err) {
      if (err.name === 'AbortError' || mine !== sequence) return;
      el.innerHTML = '<p class="unavailable">Could not reach the dashboard. Retry the current scope or return home.</p>';
      var retry = document.createElement('a');
      retry.href = current; retry.textContent = 'Retry disk analysis'; retry.setAttribute('data-disk-scan', '');
      el.appendChild(retry);
      var home = document.createElement('a');
      home.href = '/disk/usage'; home.textContent = ' Return to home scope'; home.setAttribute('data-disk-scan', '');
      el.appendChild(home);
    });
  }
  el.addEventListener('click', function(e) {
    var a = e.target.closest('[data-disk-scan]');
    if (!a || !el.contains(a) || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
    e.preventDefault(); load(a.href || a.getAttribute('data-disk-url'));
  });
  el.addEventListener('submit', function(e) {
    if (e.target.id !== 'disk-scan-options' && e.target.id !== 'disk-scope-options') return;
    e.preventDefault();
    var q = new URLSearchParams(new URL(current, location.href).search);
    var data = new FormData(e.target);
    q.set('root', data.get('root')); q.set('top', data.get('top')); q.set('budget', data.get('budget')); q.set('rescan', '1');
    load('/disk/usage?' + q.toString());
  });
  load('/disk/usage');
})();
</script>`
