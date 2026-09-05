package dashboard

import (
	"fmt"
	"html/template"
	"sort"
	"sync"
	"time"

	"vitals/internal/monitor"
	"vitals/internal/ui"
)

func init() {
	Register(Module{Slug: "processes", NavLabel: "Processes", Group: "Resources", Icon: iconProcesses, Order: 15, Available: Always, Render: renderProcesses})
}

// processCacheTTL bounds how stale the process table can be — same
// reasoning as snapshotCacheTTL: short enough nobody notices, long
// enough that a burst of requests shares one real monitor.Sample call
// (its own real cost, a blocking sample window) instead of paying for
// one each.
const processCacheTTL = 3 * time.Second

// processCache is a single-flight, TTL cache for monitor.Sample — same
// shape as snapshotCache/prepareAdviceCache (see their own doc comments
// for why this shape, not a shared generic, is this codebase's own
// convention). Deliberately its own cache, not folded into the shared
// snapshotCache: monitor.Sample's ~500ms blocking window is a real cost
// only the Processes page needs to pay, and folding it into every
// request's PageContext would slow down every other page too.
type processCache struct {
	ttl    time.Duration
	sample func() (monitor.Snapshot, error)

	mu      sync.Mutex
	value   monitor.Snapshot
	err     error
	expiry  time.Time
	loading chan struct{}
}

// processCacheCaptureTop is deliberately generous — effectively "every
// process a real machine runs" — not the Processes page's own display
// cap (see displayTop below). topProcesses (internal/monitor) sorts its
// full process list by exactly one metric (opts.SortBy) and only *then*
// truncates to opts.Top, so a memory-sorted request and a CPU-sorted
// request over the same real capture return genuinely different
// process sets: a process using 90% RAM but 2% CPU can be entirely
// absent from a CPU-sorted-and-truncated-to-20 result. Capturing wide
// once and re-sorting client-side (topProcessRows below) is what lets
// the CPU and Memory resource pages each show an accurate top-N by
// their own metric from the one real sample this cache pays for.
const processCacheCaptureTop = 1000

func newProcessCache() *processCache {
	return &processCache{ttl: processCacheTTL, sample: func() (monitor.Snapshot, error) {
		return monitor.Sample(monitor.Options{Top: processCacheCaptureTop, SortBy: "cpu"})
	}}
}

var defaultProcessCache = newProcessCache()

// Get returns the cached snapshot, sampling first if missing or stale —
// identical control flow to snapshotCache.Get/prepareAdviceCache.Get.
func (c *processCache) Get() (monitor.Snapshot, error) {
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

	v, err := c.sample()

	c.mu.Lock()
	c.value, c.err = v, err
	c.expiry = time.Now().Add(c.ttl)
	c.loading = nil
	c.mu.Unlock()
	close(ch)

	return v, err
}

// processesDisplayTop caps how many rows the Processes page itself
// shows — the cache captures far more (processCacheCaptureTop) so the
// CPU/Memory resource pages can each derive an accurate top-N by their
// own metric, but this page's own listing stays at the same visual
// density it always has.
const processesDisplayTop = 20

// renderProcesses shows the same per-process CPU/RAM table `vitals top`/
// `vitals monitor` prints in a terminal, as a page.
func renderProcesses(PageContext) string {
	snap, err := defaultProcessCache.Get()
	if err != nil {
		return mustExecute(processesErrorTmpl, err.Error())
	}
	if len(snap.Processes) == 0 {
		return `<p class="unavailable">No processes to show.</p>`
	}
	procs := snap.Processes // already CPU-sorted by the cache's own monitor.Sample call
	if len(procs) > processesDisplayTop {
		procs = procs[:processesDisplayTop]
	}
	rows := make([]processRow, len(procs))
	for i, p := range procs {
		rows[i] = processRow{
			PID: p.PID, Name: p.Name,
			CPUPct: fmt.Sprintf("%.0f%%", p.CPUPct), RSS: ui.HumanBytes(int64(p.RSSBytes)),
		}
	}
	return mustExecute(processesTmpl, rows)
}

// topProcessRows returns the n heaviest processes by CPU% (byMem false)
// or RSS (byMem true), from the same cached, widely-captured snapshot
// renderProcesses itself uses — see processCacheCaptureTop's own comment
// for why a fresh client-side sort over the full capture, not a slice of
// an already-CPU-sorted-and-truncated result, is what makes the Memory
// page's own top-N accurate. "" (not an error) when the cache itself
// failed — the CPU/Memory pages' own core fields (usage%, etc.) come
// from doctor.Snapshot regardless, so a process-table failure shouldn't
// blank the whole page, just quietly omit this one section.
func topProcessRows(n int, byMem bool) string {
	snap, err := defaultProcessCache.Get()
	if err != nil || len(snap.Processes) == 0 {
		return ""
	}
	procs := append([]monitor.ProcInfo(nil), snap.Processes...)
	sort.Slice(procs, func(i, j int) bool {
		if byMem {
			return procs[i].RSSBytes > procs[j].RSSBytes
		}
		return procs[i].CPUPct > procs[j].CPUPct
	})
	if len(procs) > n {
		procs = procs[:n]
	}
	rows := make([]processRow, len(procs))
	for i, p := range procs {
		rows[i] = processRow{PID: p.PID, Name: p.Name, CPUPct: fmt.Sprintf("%.0f%%", p.CPUPct), RSS: ui.HumanBytes(int64(p.RSSBytes))}
	}
	return mustExecute(processesTmpl, rows)
}

var processesErrorTmpl = template.Must(template.New("processesError").Parse(
	`<p class="unavailable">Could not read the process table: {{.}}</p>`))

type processRow struct {
	PID    int32
	Name   string
	CPUPct string
	RSS    string
}

var processesTmpl = template.Must(template.New("processes").Parse(`<table style="width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--line);border-radius:10px;overflow:hidden;font-size:.86rem">` +
	`<tr style="background:var(--surface-2)"><th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Process</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">CPU</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">RAM</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">PID</th></tr>` +
	`{{range .}}<tr>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);font-weight:600">{{.Name}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.CPUPct}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.RSS}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.PID}}</td>` +
	`</tr>{{end}}</table>`))
