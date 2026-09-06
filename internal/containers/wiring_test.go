package containers

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

var errDial = errors.New("dial: no such socket")

// Exercise the exported one-liners and the real exec/PATH wiring once, on
// every platform — the same "one real call" convention the rest of the
// repo uses. In CI there is usually no Docker socket and no local kube
// context, so these return an empty Report; the point is that the wiring
// runs without panicking.
func TestExportedWrappersAndDefaultWiring(t *testing.T) {
	_ = Probe(context.Background())    // must not panic with or without a runtime
	_ = ProbeAll(context.Background()) // 0, 1 or 2 entries — must not panic either

	// Sample short-circuits for anything that isn't a reachable docker report.
	if r := Sample(context.Background(), Report{}); r.Runtime != "" {
		t.Errorf("Sample on an empty report should pass it straight back, got %+v", r)
	}

	// defaultTransport's own closures.
	_ = defaultTransport.dockerEndpoint() // resolves a path or "" — either is fine
	if _, err := defaultTransport.lookPath("go"); err != nil {
		t.Errorf("lookPath(go) should succeed under a Go test run: %v", err)
	}
	if out, err := defaultTransport.run(context.Background(), "go", "version"); err != nil || !strings.Contains(string(out), "go") {
		t.Errorf("defaultTransport.run(go version) failed: out=%q err=%v", out, err)
	}
	// dialDocker against a definitely-absent socket must just error, not hang.
	if _, err := defaultTransport.dialDocker(context.Background(), "/nonexistent/vitals-test.sock"); err == nil {
		t.Error("dialing a missing socket should error")
	}
}

func TestDefaultDockerEndpointHonoursDockerHost(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///definitely/not/here.sock")
	if got := defaultDockerEndpoint(); got != "" {
		// The configured socket doesn't exist, and the well-known fallbacks
		// shouldn't either on a CI runner — but a dev box may run Docker, so
		// only assert the negative: a non-existent DOCKER_HOST is not returned.
		if got == "/definitely/not/here.sock" {
			t.Errorf("returned a DOCKER_HOST path that doesn't exist: %q", got)
		}
	}
}

func TestDockerGETSurfacesADialFailure(t *testing.T) {
	tr := transport{
		goos:           "linux",
		dockerEndpoint: func() string { return "/fake.sock" },
		dialDocker: func(context.Context, string) (net.Conn, error) {
			return nil, errDial
		},
		lookPath: func(string) (string, error) { return "", errDial },
	}
	r := probe(context.Background(), tr)
	// ping fails at the transport layer -> "socket present, daemon silent"
	if r.Runtime != "docker" || r.Reachable || !strings.Contains(r.Note, "did not respond") {
		t.Errorf("a dial failure should read as a wedged daemon, got %+v", r)
	}
}

func TestParsePodsImageFromContainerStatusAndCap(t *testing.T) {
	// image only present on the containerStatus, not the spec
	one := parsePods([]byte(`{"items":[{"metadata":{"name":"p","namespace":"d"},"spec":{"containers":[{}]},"status":{"phase":"Running","containerStatuses":[{"name":"c","image":"img:2","ready":true,"state":{"running":{}}}]}}]}`))
	if len(one) != 1 || one[0].Image != "img:2" {
		t.Errorf("image should fall back to containerStatus image: %+v", one)
	}

	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := 0; i < maxContainers+10; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"metadata":{"name":"p","namespace":"d"},"status":{"phase":"Running"}}`)
	}
	b.WriteString(`]}`)
	if n := len(parsePods([]byte(b.String()))); n != maxContainers {
		t.Errorf("parsePods cap not applied: %d", n)
	}
}

func TestSmallHelpers(t *testing.T) {
	if shortID("abc") != "abc" {
		t.Error("shortID should pass through an already-short id")
	}
	if shortID(strings.Repeat("f", 64)) != strings.Repeat("f", 12) {
		t.Error("shortID should truncate to 12")
	}
	if firstName(nil) != "" {
		t.Error("firstName(nil) should be empty")
	}
	if firstName([]string{"/only"}) != "only" {
		t.Error("firstName should strip the leading slash")
	}
	if firstLine("no newline here") != "no newline here" {
		t.Error("firstLine should return the whole string when there's no newline")
	}
	if itoa(0) != "0" || itoa(1207) != "1207" {
		t.Errorf("itoa broken: %q %q", itoa(0), itoa(1207))
	}
}

func TestOneStatHandlesGarbageAndMissingOnlineCPUs(t *testing.T) {
	f := newFakeDocker()
	defer f.srv.Close()
	f.listBody = `[{"Id":"g00000000001","Names":["/g"],"State":"running","Status":"Up"},
	               {"Id":"g00000000002","Names":["/h"],"State":"running","Status":"Up"}]`
	f.stats["g00000000001"] = `not json at all`
	// online_cpus absent -> fall back to len(percpu_usage)
	f.stats["g00000000002"] = `{"cpu_stats":{"cpu_usage":{"total_usage":300,"percpu_usage":[1,2,3,4]},"system_cpu_usage":3000},
	 "precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},"memory_stats":{"usage":1000,"limit":2000}}`

	r := probe(context.Background(), f.transport())
	r = sampleStats(context.Background(), f.transport(), r)
	if r.Containers[0].CPUPct != 0 || r.Containers[0].MemBytes != 0 {
		t.Errorf("garbage stats should leave the container's stat fields zero: %+v", r.Containers[0])
	}
	// cpuDelta=200, sysDelta=2000, ncpu=4 -> (200/2000)*4*100 = 40
	if got := r.Containers[1].CPUPct; got < 39.9 || got > 40.1 {
		t.Errorf("ncpu fallback to len(percpu) wrong: CPU%% = %v, want ~40", got)
	}
}

func TestInspectPassIsCappedAndToleratesErrors(t *testing.T) {
	f := newFakeDocker()
	defer f.srv.Close()
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < maxInspect+5; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"Id":"stopped00000","Names":["/s"],"State":"exited","Status":"Exited (0) 1 minute ago"}`)
	}
	b.WriteString("]")
	f.listBody = b.String()
	// no inspect bodies registered -> every inspect 404s; must not error out
	r := probe(context.Background(), f.transport())
	if !r.Reachable || len(r.Containers) == 0 {
		t.Fatalf("probe should still succeed when inspect calls 404: %+v", r)
	}
	// total requests = /_ping + /info + /containers/json + at most maxInspect
	if n := len(f.seenMethods()); n > 3+maxInspect {
		t.Errorf("inspect pass not capped: %d requests", n)
	}
}
