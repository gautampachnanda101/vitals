package dashboard

import (
	"strings"
	"testing"
	"time"

	"vitals/internal/containers"
	"vitals/internal/doctor"
)

func withFakeContainerStatsCache(t *testing.T, fn func(containers.Report) containers.Report) {
	t.Helper()
	old := defaultContainerStatsCache
	defaultContainerStatsCache = &containerStatsCache{ttl: time.Hour, by: map[string]statsEntry{}, sample: fn}
	t.Cleanup(func() { defaultContainerStatsCache = old })
}

func ctxWith(reps ...containers.Report) PageContext {
	var ctx PageContext
	ctx.Snapshot.Containers = reps
	return ctx
}

func TestRenderContainersPageNoRuntime(t *testing.T) {
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report { return r })
	out := renderContainersPage(PageContext{})
	if !strings.Contains(out, "No local container runtime") {
		t.Errorf("want the no-runtime message, got: %s", out)
	}
}

func TestRenderContainersPageWedgedDaemon(t *testing.T) {
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report { return r })
	out := renderContainersPage(ctxWith(containers.Report{Runtime: "docker", Note: "socket present but the daemon did not respond"}))
	if !strings.Contains(out, "did not respond") || !strings.Contains(out, `class="rtsection"`) || !strings.Contains(out, ">Docker<") {
		t.Errorf("a wedged daemon should surface its titled note: %s", out)
	}
}

func TestRenderContainersPageShowsBothRuntimes(t *testing.T) {
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report {
		for i := range r.Containers {
			r.Containers[i].CPUPct = 5
			r.Containers[i].MemBytes = 64 << 20
			r.Containers[i].MemLimitBytes = 128 << 20
		}
		return r
	})
	out := renderContainersPage(ctxWith(
		containers.Report{Runtime: "docker", Reachable: true, Endpoint: "/var/run/docker.sock", VMTotalBytes: 8 << 30,
			Containers: []containers.Container{
				{Name: "web", Image: "nginx:1.25", State: "running", Status: "Up 3 hours", ComposeProj: "shop", Ports: []string{"8080→80/tcp"}},
				{Name: "job", Image: "job:1", State: "exited", OOMKilled: true, Status: "Exited (137)"},
			}},
		containers.Report{Runtime: "kubernetes", Reachable: true, Endpoint: "kind-kind",
			Containers: []containers.Container{{Name: "api", Namespace: "prod", State: "Running", WaitingOn: "CrashLoopBackOff"}}},
	))
	for _, want := range []string{
		`class="rtsection"`, `class="rtsection k8s"`,
		">Docker<", ">Kubernetes<", "/var/run/docker.sock", "kind-kind",
		"shop", "nginx:1.25", "8080→80/tcp", "64", "128", "OOMKilled",
		"namespace: prod", "CrashLoopBackOff", "VM 8.00 GB RAM",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Containers page missing %q:\n%s", want, out)
		}
	}
	// the k8s section must come with the distinct helm icon, not the box
	if strings.Count(out, `class="rtsection-head"`) != 2 {
		t.Errorf("want a header per runtime section, got:\n%s", out)
	}
}

func TestRenderContainersPageEscapesHostileFields(t *testing.T) {
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report { return r })
	out := renderContainersPage(ctxWith(containers.Report{
		Runtime: "docker", Reachable: true,
		Containers: []containers.Container{{Name: "<script>x</script>", Image: "<b>i</b>", State: "running", Status: "<i>up</i>"}},
	}))
	if strings.Contains(out, "<script>x</script>") || strings.Contains(out, "<b>i</b>") {
		t.Errorf("container fields were not HTML-escaped: %s", out)
	}
}

func TestContainerStatsCacheKeyedPerEndpoint(t *testing.T) {
	calls := map[string]int{}
	c := &containerStatsCache{ttl: time.Hour, by: map[string]statsEntry{}, sample: func(r containers.Report) containers.Report {
		calls[r.Endpoint]++
		return r
	}}
	docker := containers.Report{Runtime: "docker", Reachable: true, Endpoint: "sockA", Containers: []containers.Container{{Name: "a"}}}
	k8s := containers.Report{Runtime: "kubernetes", Reachable: true, Endpoint: "kind", Containers: []containers.Container{{Name: "b"}}}

	// Both sampled once, and showing one does not evict the other.
	_ = c.Enrich(docker)
	_ = c.Enrich(k8s)
	_ = c.Enrich(docker)
	_ = c.Enrich(k8s)
	if calls["sockA"] != 1 || calls["kind"] != 1 {
		t.Errorf("each endpoint should be sampled once within TTL, got %v", calls)
	}
	// unreachable / empty short-circuits without sampling
	_ = c.Enrich(containers.Report{})
	if calls["sockA"]+calls["kind"] != 2 {
		t.Errorf("an empty report must not trigger a sample, got %v", calls)
	}
}

func TestContainerCardHelpers(t *testing.T) {
	// uptime from the creation timestamp when there's no "Up ..." blurb
	c := containers.Container{Status: "Restarting (1) 2s ago", CreatedUnix: time.Now().Add(-50 * time.Hour).Unix()}
	if got := containerUptime(c); got != "2d old" {
		t.Errorf("containerUptime from CreatedUnix = %q, want \"2d old\"", got)
	}
	if got := containerUptime(containers.Container{Status: "Up 3 hours"}); got != "3 hours" {
		t.Errorf("containerUptime should strip the \"Up \" prefix, got %q", got)
	}
	for d, want := range map[time.Duration]string{
		30 * time.Second: "30s", 5 * time.Minute: "5m", 3 * time.Hour: "3h", 100 * time.Hour: "4d",
	} {
		if got := shortAge(d); got != want {
			t.Errorf("shortAge(%v) = %q, want %q", d, got, want)
		}
	}
	if shortImage("chromadb/chroma@sha256:"+strings.Repeat("a", 64)) != "chromadb/chroma@sha256:aaaaaaaa…" {
		t.Errorf("shortImage digest trim wrong: %q", shortImage("chromadb/chroma@sha256:"+strings.Repeat("a", 64)))
	}
	if shortImage("nginx:1.25") != "nginx:1.25" {
		t.Error("shortImage should leave a tag reference alone")
	}
}

func TestOverviewContainersCard(t *testing.T) {
	var s doctor.Snapshot
	s.Containers = []containers.Report{
		{Runtime: "docker", Reachable: true, Containers: []containers.Container{
			{Name: "a", State: "running"}, {Name: "b", State: "exited"}}},
		{Runtime: "kubernetes", Reachable: true, Containers: []containers.Container{
			{Name: "p", State: "Running", WaitingOn: "CrashLoopBackOff"}}},
	}
	out := containersCard(s)
	for _, want := range []string{">Containers</h3>", "2 / 3", "docker", "kubernetes", "1 unhealthy", `href="/containers"`} {
		if !strings.Contains(out, want) {
			t.Errorf("overview containers card missing %q:\n%s", want, out)
		}
	}
}

func TestHasContainersGate(t *testing.T) {
	if HasContainers(PageContext{}) {
		t.Error("no runtime -> module unavailable")
	}
	if !HasContainers(ctxWith(containers.Report{Runtime: "kubernetes"})) {
		t.Error("a detected runtime -> module available")
	}
	if HasContainers(ctxWith(containers.Report{})) {
		t.Error("a report with an empty Runtime is not a detection")
	}
}
