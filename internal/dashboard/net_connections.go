package dashboard

import (
	"fmt"
	"html/template"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// The Network page's per-process detail: not a ranked-by-bytes table
// (gopsutil exposes a process's sockets, not its transferred byte
// counts — see roadmap item 012) but the live connection list itself,
// the way netstat shows it — which process, talking to which remote
// host, in which state.

const (
	connCacheTTL    = 5 * time.Second
	connsDisplayCap = 15
)

// connCache is a single-flight TTL cache over one gnet.Connections call
// — same shape as processCache/snapshotCache. gnet.Connections walks the
// kernel socket table (~35ms on a normal machine, more under load), so a
// burst of Network-page loads shares one call rather than each paying.
type connCache struct {
	ttl   time.Duration
	fetch func() ([]gnet.ConnectionStat, error)

	mu      sync.Mutex
	value   []gnet.ConnectionStat
	err     error
	expiry  time.Time
	loading chan struct{}
}

func newConnCache() *connCache {
	return &connCache{ttl: connCacheTTL, fetch: func() ([]gnet.ConnectionStat, error) {
		return gnet.Connections("all")
	}}
}

var defaultConnCache = newConnCache()

func (c *connCache) Get() ([]gnet.ConnectionStat, error) {
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

	v, err := c.fetch()

	c.mu.Lock()
	c.value, c.err = v, err
	c.expiry = time.Now().Add(c.ttl)
	c.loading = nil
	c.mu.Unlock()
	close(ch)

	return v, err
}

type connRow struct {
	Proc   string
	Remote string
	State  string
}

// activeConnectionsSection renders the "Active connections" table for the
// Network page. "" when the socket table can't be read or there's
// nothing worth showing, so the section just doesn't appear.
func activeConnectionsSection() string {
	conns, err := defaultConnCache.Get()
	if err != nil || len(conns) == 0 {
		return ""
	}

	names := pidNames() // pid -> process name, best-effort

	rows := make([]connRow, 0, len(conns))
	for _, c := range conns {
		if !hasRealRemote(c.Raddr) || isLoopback(c.Raddr.IP) {
			continue // listening sockets, UNIX sockets, loopback chatter
		}
		if !liveState(c.Status) {
			continue // CLOSED / TIME_WAIT / etc. — not "talking right now"
		}
		proc := names[c.Pid]
		if proc == "" {
			proc = "pid " + strconv.Itoa(int(c.Pid))
		} else {
			proc = fmt.Sprintf("%s (pid %d)", proc, c.Pid)
		}
		rows = append(rows, connRow{
			Proc:   proc,
			Remote: fmt.Sprintf("%s:%d", c.Raddr.IP, c.Raddr.Port),
			State:  c.Status,
		})
	}
	if len(rows) == 0 {
		return ""
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Proc != rows[j].Proc {
			return rows[i].Proc < rows[j].Proc
		}
		return rows[i].Remote < rows[j].Remote
	})
	total := len(rows)
	if len(rows) > connsDisplayCap {
		rows = rows[:connsDisplayCap]
	}

	out := `<div class="sectiontitle">Active connections</div>`
	if total > len(rows) {
		out += fmt.Sprintf(`<p class="caption">%d outbound connections; showing %d.</p>`, total, len(rows))
	}
	return out + card(mustExecute(connsTmpl, rows))
}

// liveState keeps only connections that represent traffic happening now
// — an established session or one being opened — and drops the
// half-closed / waiting states that make up most of a socket table's
// noise.
func liveState(s string) bool {
	switch s {
	case "ESTABLISHED", "SYN_SENT", "SYN_RECV":
		return true
	default:
		return false
	}
}

func hasRealRemote(a gnet.Addr) bool {
	return a.IP != "" && a.IP != "0.0.0.0" && a.IP != "::" && a.Port != 0
}

func isLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// pidNames returns a best-effort pid -> name map from the process cache
// the Processes/CPU/Memory pages already populate. Empty map (never nil)
// when that cache is unavailable — callers fall back to a bare pid.
func pidNames() map[int32]string {
	m := map[int32]string{}
	snap, err := defaultProcessCache.Get()
	if err != nil {
		return m
	}
	for _, p := range snap.Processes {
		m[p.PID] = p.Name
	}
	return m
}

var connsTmpl = template.Must(template.New("conns").Parse(`<table style="width:100%;border-collapse:collapse;background:var(--surface);border:1px solid var(--line);border-radius:10px;overflow:hidden;font-size:.86rem">` +
	`<tr style="background:var(--surface-2)">` +
	`<th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Process</th>` +
	`<th style="text-align:left;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">Remote</th>` +
	`<th style="text-align:right;padding:.5rem .8rem;font-size:.7rem;color:var(--muted);text-transform:uppercase">State</th></tr>` +
	`{{range .}}<tr>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);font-weight:600">{{.Proc}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line)" class="mono">{{.Remote}}</td>` +
	`<td style="padding:.5rem .8rem;border-top:1px solid var(--line);text-align:right" class="mono">{{.State}}</td>` +
	`</tr>{{end}}</table>`))
