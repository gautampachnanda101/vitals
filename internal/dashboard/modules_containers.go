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
		Render: renderContainersPage,
	})
}

// renderContainersPage is the Containers page. A kind/k3s host runs a
// Docker daemon AND a Kubernetes API at once, so the snapshot carries
// every reachable runtime — each gets its own titled section here, no
// "pick one".
func renderContainersPage(ctx PageContext) string {
	report := doctor.AnalyzeResource(ctx.Snapshot, "containers")
	out := verdictBanner(reportHeadline(report, "No issues found"), "", report.Worst())

	reps := ctx.Snapshot.Containers
	if len(reps) == 0 {
		out += card(`<p class="unavailable">No local container runtime or Kubernetes context detected.</p>`)
	}
	for _, rep := range reps {
		out += renderRuntimeSection(rep)
	}
	out += findingsCard(report.SortedBySeverity())
	return out
}

// containerStatsCacheTTL bounds how stale the per-container CPU/mem
// sample can be. containers.Sample makes a ~1s server-side call per
// running container (concurrently), so — like powerImpactCache — a burst
// of Containers-page loads shares one sample.
const containerStatsCacheTTL = 12 * time.Second

type statsEntry struct {
	value  containers.Report
	expiry time.Time
}

// containerStatsCache caches one enriched Report per runtime endpoint, so
// a page that shows Docker *and* Kubernetes at once doesn't evict one to
// sample the other, and every 10s auto-refresh reuses both within the TTL.
type containerStatsCache struct {
	ttl    time.Duration
	sample func(containers.Report) containers.Report

	mu sync.Mutex
	by map[string]statsEntry
}

func newContainerStatsCache() *containerStatsCache {
	return &containerStatsCache{
		ttl: containerStatsCacheTTL,
		by:  map[string]statsEntry{},
		sample: func(r containers.Report) containers.Report {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			return containers.Sample(ctx, r)
		},
	}
}

var defaultContainerStatsCache = newContainerStatsCache()

// Enrich returns base with per-container stats filled, sampling at most
// once per TTL per endpoint. A runtime restart changes nothing about the
// key, but the short TTL means a stale sample clears within seconds.
func (c *containerStatsCache) Enrich(base containers.Report) containers.Report {
	if !base.Reachable || len(base.Containers) == 0 {
		return base
	}
	key := base.Runtime + "\x00" + base.Endpoint
	c.mu.Lock()
	if e, ok := c.by[key]; ok && time.Now().Before(e.expiry) {
		c.mu.Unlock()
		return e.value
	}
	c.mu.Unlock()

	v := c.sample(base)

	c.mu.Lock()
	c.by[key] = statsEntry{v, time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return v
}

// renderRuntimeSection renders one reachable runtime as a self-contained,
// visually-distinct block: a coloured left border + icon header (Docker
// green with the container mark, Kubernetes blue with the helm mark), a
// summary line, then the containers grouped by compose project /
// namespace. Per-container CPU/mem is sampled here (cached).
func renderRuntimeSection(base containers.Report) string {
	k8s := base.Runtime == "kubernetes"
	cls, icon, label := "rtsection", iconContainers, "Docker"
	if k8s {
		cls, icon, label = "rtsection k8s", iconKubernetes, "Kubernetes"
	}

	inner := `<div class="rtsection-head"><svg viewBox="0 0 24 24">` + string(icon) + `</svg>` +
		template.HTMLEscapeString(label)
	if base.Endpoint != "" {
		inner += `<span class="ep">` + template.HTMLEscapeString(base.Endpoint) + `</span>`
	}
	inner += `</div>`

	if !base.Reachable {
		note := base.Note
		if note == "" {
			note = "the runtime is not responding"
		}
		return `<div class="` + cls + `">` + inner +
			`<p class="unavailable">` + template.HTMLEscapeString(note) + `</p></div>`
	}

	rep := defaultContainerStatsCache.Enrich(base)
	run, stopped, unhealthy := rep.Counts()
	inner += `<div style="color:var(--muted);font-size:.85rem;margin-bottom:.8rem">` +
		fmt.Sprintf("%d running · %d stopped · %d unhealthy", run, stopped, unhealthy)
	if rep.VMTotalBytes > 0 {
		inner += fmt.Sprintf(" · VM %s RAM", ui.HumanBytes(int64(rep.VMTotalBytes)))
	}
	inner += `</div>`

	for _, g := range groupContainers(rep.Containers) {
		inner += `<div class="sectiontitle" style="margin:1rem 0 .5rem">` + template.HTMLEscapeString(g.Title) + `</div>`
		inner += mustExecute(containerCardsTmpl, g.Items)
	}
	return `<div class="` + cls + `">` + inner + `</div>`
}

type containerGroup struct {
	Title string
	Items []containerCard
}

type containerCard struct {
	Name, Image, State, StateClass string
	Uptime, ID                     string
	Ports                          []string
	CPU, Mem                       string
	MemPct                         int // 0 = no limit known / no bar
	StatusLine                     string
	Bad                            bool
}

// groupContainers buckets by compose project (k8s: namespace), so a
// multi-service stack reads as a unit — the same grouping the Docker
// VS Code view and `docker compose ps` use. Ungrouped containers fall
// under "Standalone". Groups and items keep the runtime's own order.
func groupContainers(cs []containers.Container) []containerGroup {
	order := []string{}
	byGroup := map[string][]containerCard{}
	for _, c := range cs {
		g := c.ComposeProj
		if g == "" && c.Namespace != "" {
			g = "namespace: " + c.Namespace
		}
		if g == "" {
			g = "Standalone"
		}
		if _, seen := byGroup[g]; !seen {
			order = append(order, g)
		}
		byGroup[g] = append(byGroup[g], toCard(c))
	}
	out := make([]containerGroup, 0, len(order))
	for _, g := range order {
		out = append(out, containerGroup{Title: g, Items: byGroup[g]})
	}
	return out
}

// shortImage trims a `repo@sha256:<64 hex>` reference down to
// `repo@sha256:<12 hex>…` for display — the full digest is in --json,
// but it blows the card layout and tells a person nothing.
func shortImage(img string) string {
	if i := strings.Index(img, "@sha256:"); i >= 0 && len(img) > i+20 {
		return img[:i+16] + "…"
	}
	return img
}

func toCard(c containers.Container) containerCard {
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
	if c.RestartCount > 0 {
		status += fmt.Sprintf(" · %d restarts", c.RestartCount)
	}

	cpu := "—"
	if c.CPUPct > 0 {
		cpu = fmt.Sprintf("%.0f%%", c.CPUPct)
	}
	mem, memPct := "—", 0
	if c.MemBytes > 0 {
		mem = ui.HumanBytes(int64(c.MemBytes))
		if c.MemLimitBytes > 0 {
			mem += " / " + ui.HumanBytes(int64(c.MemLimitBytes))
			memPct = int(float64(c.MemBytes) / float64(c.MemLimitBytes) * 100)
			if memPct > 100 {
				memPct = 100
			}
		}
	}

	sc := "ok"
	if !c.Running() {
		sc = "muted"
	}
	if c.Health == "unhealthy" || c.WaitingOn != "" || c.OOMKilled {
		sc = "crit"
	} else if c.Health == "starting" || c.State == "restarting" {
		sc = "warn"
	}

	return containerCard{
		Name: name, Image: shortImage(c.Image), State: strings.ToLower(c.State), StateClass: sc,
		Uptime: containerUptime(c), ID: c.ID, Ports: c.Ports,
		CPU: cpu, Mem: mem, MemPct: memPct,
		StatusLine: status,
		Bad:        c.OOMKilled || c.WaitingOn != "" || c.Health == "unhealthy",
	}
}

// containerUptime prefers the runtime's own human blurb ("Up 3 hours")
// and falls back to a relative age from the creation timestamp.
func containerUptime(c containers.Container) string {
	if s := strings.TrimSpace(c.Status); strings.HasPrefix(s, "Up ") {
		return strings.TrimPrefix(s, "Up ")
	}
	if c.CreatedUnix > 0 {
		d := time.Since(time.Unix(c.CreatedUnix, 0))
		if d < 0 {
			d = 0
		}
		return shortAge(d) + " old"
	}
	return ""
}

func shortAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

var containerCardsTmpl = template.Must(template.New("containerCards").Parse(
	`{{range .}}<div class="card" style="margin-bottom:.7rem">` +
		`<div style="display:flex;align-items:center;gap:.6rem;flex-wrap:wrap">` +
		`<span style="font-weight:700">{{.Name}}</span>` +
		`<span class="pill {{.StateClass}}">{{.State}}</span>` +
		`{{if .Uptime}}<span style="color:var(--muted);font-size:.78rem">{{.Uptime}}</span>{{end}}` +
		`<span style="margin-left:auto;color:var(--muted);font-size:.76rem" class="mono">{{.ID}}</span>` +
		`</div>` +
		`<div style="color:var(--muted);font-size:.82rem;margin-top:.15rem" class="mono">{{.Image}}</div>` +
		`<div style="display:flex;gap:1.4rem;flex-wrap:wrap;margin-top:.6rem;font-size:.84rem">` +
		`<div><span style="color:var(--muted);font-size:.72rem;text-transform:uppercase">CPU</span><br><span class="mono">{{.CPU}}</span></div>` +
		`<div style="min-width:150px"><span style="color:var(--muted);font-size:.72rem;text-transform:uppercase">Memory</span><br><span class="mono">{{.Mem}}</span>` +
		`{{if .MemPct}}<div class="bar" style="margin:.3rem 0 0;max-width:150px"><span style="width:{{.MemPct}}%"></span></div>{{end}}</div>` +
		`{{if .Ports}}<div><span style="color:var(--muted);font-size:.72rem;text-transform:uppercase">Ports</span><br>{{range .Ports}}<span class="mono" style="margin-right:.5rem">{{.}}</span>{{end}}</div>{{end}}` +
		`</div>` +
		`<div style="margin-top:.5rem;font-size:.84rem{{if .Bad}};color:var(--crit);font-weight:600{{end}}">{{.StatusLine}}</div>` +
		`</div>{{end}}`))
