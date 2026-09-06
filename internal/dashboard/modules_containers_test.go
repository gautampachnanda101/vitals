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
	defaultContainerStatsCache = &containerStatsCache{ttl: time.Hour, sample: fn}
	t.Cleanup(func() { defaultContainerStatsCache = old })
}

func TestRenderContainersNoRuntime(t *testing.T) {
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report { return r })
	out := renderContainers(doctor.Snapshot{})
	if !strings.Contains(out, "No local container runtime") {
		t.Errorf("want the no-runtime message, got: %s", out)
	}
}

func TestRenderContainersWedgedDaemon(t *testing.T) {
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report { return r })
	s := doctor.Snapshot{}
	s.Containers = containers.Report{Runtime: "docker", Note: "socket present but the daemon did not respond"}
	out := renderContainers(s)
	if !strings.Contains(out, "did not respond") {
		t.Errorf("a wedged daemon should surface its note: %s", out)
	}
}

func TestRenderContainersListsWithStatsAndFlagsBadOnes(t *testing.T) {
	base := containers.Report{
		Runtime: "docker", Reachable: true, Endpoint: "/var/run/docker.sock", VMTotalBytes: 8 << 30,
		Containers: []containers.Container{
			{Name: "web", Image: "nginx:1.25", State: "running", Status: "Up 3 hours"},
			{Name: "cache", Image: "redis", State: "running", Status: "Up 3 hours", Health: "unhealthy"},
			{Name: "job", Image: "job:1", State: "exited", OOMKilled: true, Status: "Exited (137)"},
		},
	}
	// the cache is what fills CPU/mem — prove renderContainers uses its result
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report {
		r.Containers[0].CPUPct = 8
		r.Containers[0].MemBytes = 128 << 20
		r.Containers[0].MemLimitBytes = 256 << 20
		return r
	})
	s := doctor.Snapshot{}
	s.Containers = base
	out := renderContainers(s)

	for _, want := range []string{"docker", "2 running, 1 stopped, 2 unhealthy", "Runtime VM RAM", "nginx:1.25", "8%", "128", "256", "OOMKilled"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderContainers output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderContainersEscapesHostileFields(t *testing.T) {
	withFakeContainerStatsCache(t, func(r containers.Report) containers.Report { return r })
	s := doctor.Snapshot{}
	s.Containers = containers.Report{
		Runtime: "docker", Reachable: true,
		Containers: []containers.Container{{Name: "<script>x</script>", Image: "<b>i</b>", State: "running", Status: "<i>up</i>"}},
	}
	out := renderContainers(s)
	if strings.Contains(out, "<script>x</script>") || strings.Contains(out, "<b>i</b>") {
		t.Errorf("container fields were not HTML-escaped: %s", out)
	}
}

func TestContainerStatsCacheIsSingleFlightAndKeyed(t *testing.T) {
	calls := 0
	c := &containerStatsCache{ttl: time.Hour, sample: func(r containers.Report) containers.Report {
		calls++
		return r
	}}
	rep := containers.Report{Reachable: true, Endpoint: "sockA", Containers: []containers.Container{{Name: "a"}}}
	_ = c.Enrich(rep)
	_ = c.Enrich(rep)
	if calls != 1 {
		t.Errorf("second Enrich within TTL should reuse the sample, calls=%d", calls)
	}
	rep.Endpoint = "sockB" // runtime endpoint changed -> re-sample
	_ = c.Enrich(rep)
	if calls != 2 {
		t.Errorf("a changed endpoint should invalidate the cache, calls=%d", calls)
	}
	// unreachable / empty short-circuits without sampling
	_ = c.Enrich(containers.Report{})
	if calls != 2 {
		t.Errorf("an empty report must not trigger a sample, calls=%d", calls)
	}
}

func TestHasContainersGate(t *testing.T) {
	if HasContainers(PageContext{}) {
		t.Error("no runtime -> module unavailable")
	}
	ctx := PageContext{}
	ctx.Snapshot.Containers.Runtime = "docker"
	if !HasContainers(ctx) {
		t.Error("a detected runtime -> module available")
	}
}
