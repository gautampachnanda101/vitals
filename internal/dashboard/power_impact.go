package dashboard

import (
	"fmt"
	"html/template"
	"sync"
	"time"

	"vitals/internal/power"
)

// powerImpactCacheTTL bounds how stale the per-process energy reading
// can be. power.Sample shells out to `top -l 2` (~2s), so — like
// processCache — a burst of Power-page loads shares one probe.
const powerImpactCacheTTL = 10 * time.Second

// powerImpactCache is a single-flight TTL cache for power.Sample, the
// same shape as processCache (see its doc comment). Its own cache
// because the `top` probe is a cost only the Power page pays.
type powerImpactCache struct {
	ttl    time.Duration
	sample func() ([]power.Proc, bool)

	mu      sync.Mutex
	procs   []power.Proc
	ok      bool
	expiry  time.Time
	loading chan struct{}
}

func newPowerImpactCache() *powerImpactCache {
	return &powerImpactCache{ttl: powerImpactCacheTTL, sample: func() ([]power.Proc, bool) {
		return power.Sample(topProcessesSectionN)
	}}
}

var defaultPowerImpactCache = newPowerImpactCache()

func (c *powerImpactCache) Get() ([]power.Proc, bool) {
	c.mu.Lock()
	if time.Now().Before(c.expiry) {
		p, ok := c.procs, c.ok
		c.mu.Unlock()
		return p, ok
	}
	if c.loading != nil {
		ch := c.loading
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		p, ok := c.procs, c.ok
		c.mu.Unlock()
		return p, ok
	}
	ch := make(chan struct{})
	c.loading = ch
	c.mu.Unlock()

	p, ok := c.sample()

	c.mu.Lock()
	c.procs, c.ok = p, ok
	c.expiry = time.Now().Add(c.ttl)
	c.loading = nil
	c.mu.Unlock()
	close(ch)

	return p, ok
}

// powerImpactSection renders the Power page's "Energy impact by process"
// table from a real macOS power reading. "" when there's no
// no-privilege per-process energy source (Linux/Windows) or the probe
// failed — the caller then falls back to its CPU-ranked estimate
// (roadmap item 012).
func powerImpactSection() string {
	procs, ok := defaultPowerImpactCache.Get()
	if !ok || len(procs) == 0 {
		return ""
	}
	rows := make([]powerImpactRow, len(procs))
	for i, p := range procs {
		rows[i] = powerImpactRow{PID: p.PID, Name: p.Name, Impact: fmt.Sprintf("%.1f", p.Power)}
	}
	return `<div class="sectiontitle">Energy impact by process</div>` +
		`<p class="caption">Real macOS power score (the figure Activity Monitor's Energy tab shows), not a CPU estimate and not watts.</p>` +
		card(mustExecute(powerImpactTmpl, rows))
}

type powerImpactRow struct {
	PID    int32
	Name   string
	Impact string
}

var powerImpactTmpl = template.Must(template.New("powerImpact").Parse(`<table style="width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--line);border-radius:10px;overflow:hidden;font-size:.86rem">` +
	`<tr style="background:var(--surface-2)"><th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Process</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Impact</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">PID</th></tr>` +
	`{{range .}}<tr>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);font-weight:600">{{.Name}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.Impact}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.PID}}</td>` +
	`</tr>{{end}}</table>`))
