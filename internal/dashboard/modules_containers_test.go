package dashboard

import (
	"strings"
	"testing"
	"time"

	"vitals/internal/containers"
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
	if !strings.Contains(out, "did not respond") || !strings.Contains(out, "DOCKER") {
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
		"DOCKER · /var/run/docker.sock", "KUBERNETES · kind-kind",
		"shop", "nginx:1.25", "8080→80/tcp", "64", "128", "OOMKilled",
		"namespace: prod", "CrashLoopBackOff", "Runtime VM RAM",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Containers page missing %q:\n%s", want, out)
		}
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
