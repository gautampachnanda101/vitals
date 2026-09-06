package dashboard

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"sync"
	"time"

	"vitals/internal/containers"
	"vitals/internal/doctor"
	"vitals/internal/ui"
)

func init() {
	Register(Module{
		Slug: "containers", NavLabel: "Containers", Group: "Resources",
		Icon: iconContainers, Order: 55,
		Available: HasContainers, UnavailableReason: "no local container runtime detected",
		Render: resourcePage("containers", renderContainers),
	})
}

// containerStatsCacheTTL bounds how stale the per-container CPU/mem
// sample can be. containers.Sample makes a ~1s server-side call per
// running container (concurrently), so — like powerImpactCache — a burst
// of Containers-page loads shares one sample.
const containerStatsCacheTTL = 12 * time.Second

type containerStatsCache struct {
	ttl    time.Duration
	sample func(containers.Report) containers.Report

	mu      sync.Mutex
	value   containers.Report
	forKey  string
	expiry  time.Time
	loading chan struct{}
}

func newContainerStatsCache() *containerStatsCache {
	return &containerStatsCache{ttl: containerStatsCacheTTL, sample: func(r containers.Report) containers.Report {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		return containers.Sample(ctx, r)
	}}
}

var defaultContainerStatsCache = newContainerStatsCache()

// Enrich returns base with per-container stats filled, sampling at most
// once per TTL. Keyed by the runtime endpoint so a runtime restart
// invalidates a stale sample.
func (c *containerStatsCache) Enrich(base containers.Report) containers.Report {
	if !base.Reachable || len(base.Containers) == 0 {
		return base
	}
	c.mu.Lock()
	if c.forKey == base.Endpoint && time.Now().Before(c.expiry) {
		v := c.value
		c.mu.Unlock()
		return v
	}
	if c.loading != nil {
		ch := c.loading
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		v := c.value
		c.mu.Unlock()
		return v
	}
	ch := make(chan struct{})
	c.loading = ch
	c.mu.Unlock()

	v := c.sample(base)

	c.mu.Lock()
	c.value, c.forKey = v, base.Endpoint
	c.expiry = time.Now().Add(c.ttl)
	c.loading = nil
	c.mu.Unlock()
	close(ch)
	return v
}

func renderContainers(s doctor.Snapshot) string {
	rep := defaultContainerStatsCache.Enrich(s.Containers)
	if rep.Runtime == "" {
		return card(`<p class="unavailable">No local container runtime or Kubernetes context detected.</p>`)
	}
	if !rep.Reachable {
		note := rep.Note
		if note == "" {
			note = "the runtime is not responding"
		}
		return card(`<p class="unavailable">` + template.HTMLEscapeString(rep.Runtime) + `: ` + template.HTMLEscapeString(note) + `</p>`)
	}

	run, stopped, unhealthy := rep.Counts()
	head := row("Runtime", fmt.Sprintf("%s — %s", rep.Runtime, template.HTMLEscapeString(nzStr(rep.Endpoint, "local"))))
	head += row("Containers", fmt.Sprintf("%d running, %d stopped, %d unhealthy", run, stopped, unhealthy))
	if rep.VMTotalBytes > 0 {
		head += row("Runtime VM RAM", ui.HumanBytes(int64(rep.VMTotalBytes)))
	}
	out := card(head)

	if len(rep.Containers) == 0 {
		return out
	}
	rows := make([]containerRow, 0, len(rep.Containers))
	for _, c := range rep.Containers {
		name := c.Name
		if c.Namespace != "" {
			name = c.Namespace + "/" + name
		}
		status := c.Status
		if c.WaitingOn != "" {
			status = c.WaitingOn
		}
		if c.OOMKilled {
			status += " · OOMKilled"
		}
		cpu, mem := "—", "—"
		if c.CPUPct > 0 {
			cpu = fmt.Sprintf("%.0f%%", c.CPUPct)
		}
		if c.MemBytes > 0 {
			mem = ui.HumanBytes(int64(c.MemBytes))
			if c.MemLimitBytes > 0 {
				mem += " / " + ui.HumanBytes(int64(c.MemLimitBytes))
			}
		}
		rows = append(rows, containerRow{
			Name: name, Image: c.Image, State: strings.ToLower(c.State),
			CPU: cpu, Mem: mem, Status: status,
			Bad: c.OOMKilled || c.WaitingOn != "" || c.Health == "unhealthy",
		})
	}
	out += `<div class="sectiontitle">Containers</div>` + card(mustExecute(containersTmpl, rows))
	return out
}

type containerRow struct {
	Name, Image, State, CPU, Mem, Status string
	Bad                                  bool
}

func nzStr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

var containersTmpl = template.Must(template.New("containers").Parse(`<table style="width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--line);border-radius:10px;overflow:hidden;font-size:.86rem">` +
	`<tr style="background:var(--surface-2)">` +
	`<th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Container</th>` +
	`<th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">State</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">CPU</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Memory</th>` +
	`<th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Status</th></tr>` +
	`{{range .}}<tr>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line)"><span style="font-weight:600">{{.Name}}</span><br><span style="color:var(--muted);font-size:.78rem">{{.Image}}</span></td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line)">{{.State}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.CPU}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.Mem}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line){{if .Bad}};color:var(--bad,#c0392b);font-weight:600{{end}}">{{.Status}}</td>` +
	`</tr>{{end}}</table>`))
