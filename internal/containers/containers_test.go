package containers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- Docker: an httptest server behind the injected dialer ---------------

type fakeDocker struct {
	srv        *httptest.Server
	mu         sync.Mutex
	methods    []string // every method the server was asked for (stats fan-out hits this concurrently)
	pingStatus int
	infoBody   string
	listStatus int
	listBody   string
	inspect    map[string]string // short id -> inspect JSON
	stats      map[string]string // short id -> stats JSON
}

func (f *fakeDocker) record(method string) {
	f.mu.Lock()
	f.methods = append(f.methods, method)
	f.mu.Unlock()
}

func (f *fakeDocker) seenMethods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.methods...)
}

func newFakeDocker() *fakeDocker {
	f := &fakeDocker{
		pingStatus: 200,
		infoBody:   `{"MemTotal": 8360198144}`,
		listStatus: 200,
		listBody:   "[]",
		inspect:    map[string]string{},
		stats:      map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		switch {
		case r.URL.Path == "/_ping":
			w.WriteHeader(f.pingStatus)
			_, _ = w.Write([]byte("OK"))
		case r.URL.Path == "/info":
			_, _ = w.Write([]byte(f.infoBody))
		case r.URL.Path == "/containers/json":
			w.WriteHeader(f.listStatus)
			_, _ = w.Write([]byte(f.listBody))
		case strings.HasSuffix(r.URL.Path, "/json"): // /containers/<id>/json
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/containers/"), "/")[0]
			if body, ok := f.inspect[id]; ok {
				_, _ = w.Write([]byte(body))
				return
			}
			w.WriteHeader(404)
		case strings.Contains(r.URL.Path, "/stats"):
			id := strings.Split(strings.TrimPrefix(r.URL.Path, "/containers/"), "/")[0]
			if body, ok := f.stats[id]; ok {
				_, _ = w.Write([]byte(body))
				return
			}
			w.WriteHeader(404)
		default:
			w.WriteHeader(418)
		}
	})
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *fakeDocker) transport() transport {
	return transport{
		goos:           "linux",
		dockerEndpoint: func() string { return "/fake/docker.sock" },
		dialDocker: func(ctx context.Context, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", f.srv.Listener.Addr().String())
		},
		lookPath: func(string) (string, error) { return "", errors.New("no kubectl") },
		run:      func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("no exec") },
	}
}

func TestProbeDockerHappyPath(t *testing.T) {
	f := newFakeDocker()
	defer f.srv.Close()
	f.listBody = `[
	  {"Id":"web000000001","Names":["/web"],"Image":"nginx","State":"running","Status":"Up 2 hours (healthy)","Labels":{"com.docker.compose.project":"shop"}},
	  {"Id":"wrk000000002","Names":["/worker"],"Image":"busybox","State":"exited","Status":"Exited (137) 5 minutes ago","Labels":{}},
	  {"Id":"api000000003","Names":["/api"],"Image":"api:1","State":"running","Status":"Up 1 hour (unhealthy)","Labels":{}}
	]`
	f.inspect["wrk000000002"] = `{"RestartCount":4,"State":{"OOMKilled":true,"ExitCode":137}}`
	f.inspect["api000000003"] = `{"RestartCount":9,"State":{"OOMKilled":false,"ExitCode":0}}`

	r := probe(context.Background(), f.transport())
	if r.Runtime != "docker" || !r.Reachable {
		t.Fatalf("want a reachable docker report, got %+v", r)
	}
	if r.VMTotalBytes != 8360198144 {
		t.Errorf("VMTotalBytes = %d, want the /info MemTotal", r.VMTotalBytes)
	}
	if len(r.Containers) != 3 {
		t.Fatalf("want 3 containers, got %d", len(r.Containers))
	}
	web, worker, api := r.Containers[0], r.Containers[1], r.Containers[2]
	if web.Name != "web" || web.ComposeProj != "shop" || web.Health != "healthy" || !web.Running() {
		t.Errorf("web parsed wrong: %+v", web)
	}
	if worker.ExitCode != 137 || !worker.OOMKilled || worker.RestartCount != 4 {
		t.Errorf("worker (exited, inspected) parsed wrong: %+v", worker)
	}
	if api.Health != "unhealthy" || api.RestartCount != 9 {
		t.Errorf("api (running but unhealthy, should be inspected) parsed wrong: %+v", api)
	}
	// A healthy running container must NOT have triggered an inspect call.
	for _, m := range f.seenMethods() {
		if m != http.MethodGet {
			t.Fatalf("non-GET request reached the daemon: %v", f.seenMethods())
		}
	}
	run, stop, unhealthy := r.Counts()
	if run != 2 || stop != 1 || unhealthy != 2 { // worker OOMKilled + api unhealthy
		t.Errorf("Counts() = %d/%d/%d, want 2/1/2", run, stop, unhealthy)
	}
}

func TestProbeDockerNoEndpoint(t *testing.T) {
	tr := transport{
		goos:           "linux",
		dockerEndpoint: func() string { return "" },
		lookPath:       func(string) (string, error) { return "", errors.New("no kubectl") },
	}
	if r := probe(context.Background(), tr); r.Runtime != "" || r.Reachable {
		t.Errorf("no socket and no kubectl should give an empty report, got %+v", r)
	}
}

func TestProbeDockerSocketPresentButDaemonWedged(t *testing.T) {
	f := newFakeDocker()
	defer f.srv.Close()
	f.pingStatus = 500

	r := probe(context.Background(), f.transport())
	if r.Runtime != "docker" || r.Reachable {
		t.Errorf("a wedged daemon should be Runtime=docker, Reachable=false, got %+v", r)
	}
	if !strings.Contains(r.Note, "did not respond") {
		t.Errorf("Note should explain the daemon didn't answer, got %q", r.Note)
	}
}

func TestProbeDockerListError(t *testing.T) {
	f := newFakeDocker()
	defer f.srv.Close()
	f.listStatus = 500
	f.listBody = "boom"

	r := probe(context.Background(), f.transport())
	if !r.Reachable || r.Note == "" || len(r.Containers) != 0 {
		t.Errorf("list failure should keep Reachable with a Note and no containers, got %+v", r)
	}
}

func TestPortMappings(t *testing.T) {
	in := []apiPort{
		{PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}, // dup of the above -> collapsed
		{PrivatePort: 443, PublicPort: 8443},                       // no Type -> tcp default
		{PrivatePort: 9000, PublicPort: 0},                         // not published -> dropped
		{PrivatePort: 53, PublicPort: 5353, Type: "udp"},
	}
	got := portMappings(in)
	want := []string{"8080→80/tcp", "8443→443/tcp", "5353→53/udp"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("portMappings = %v, want %v", got, want)
	}

	// cap at 8
	var many []apiPort
	for i := 0; i < 20; i++ {
		many = append(many, apiPort{PrivatePort: i, PublicPort: 30000 + i, Type: "tcp"})
	}
	if n := len(portMappings(many)); n != 8 {
		t.Errorf("portMappings should cap at 8, got %d", n)
	}
	if portMappings(nil) != nil {
		t.Error("portMappings(nil) should be nil")
	}
}

func TestParseContainerListCarriesPortsAndCreated(t *testing.T) {
	body := `[{"Id":"abc123def456ff","Names":["/web"],"Image":"nginx","State":"running","Status":"Up 1 hour",
	  "Created":1725600000,
	  "Ports":[{"PrivatePort":80,"PublicPort":8080,"Type":"tcp"},{"PrivatePort":8443,"PublicPort":0,"Type":"tcp"}]}]`
	got := parseContainerList([]byte(body))
	if len(got) != 1 {
		t.Fatalf("want 1 container, got %d", len(got))
	}
	c := got[0]
	if len(c.Ports) != 1 || c.Ports[0] != "8080→80/tcp" {
		t.Errorf("published port not parsed: %v", c.Ports)
	}
	if c.CreatedUnix != 1725600000 {
		t.Errorf("CreatedUnix = %d, want 1725600000", c.CreatedUnix)
	}
}

func TestParseContainerListHealthExitAndCap(t *testing.T) {
	if got := parseContainerList([]byte("not json")); got != nil {
		t.Errorf("bad JSON should parse to nil, got %+v", got)
	}
	body := `[
	  {"Id":"1","Names":["/a"],"State":"running","Status":"Up 10 seconds (health: starting)"},
	  {"Id":"2","Names":["/b"],"State":"restarting","Status":"Restarting (1) 4 seconds ago"}
	]`
	got := parseContainerList([]byte(body))
	if got[0].Health != "starting" {
		t.Errorf("health: starting not detected: %+v", got[0])
	}
	if got[1].ExitCode != 1 || got[1].State != "restarting" {
		t.Errorf("restarting exit code not parsed: %+v", got[1])
	}

	// cap
	var big strings.Builder
	big.WriteString("[")
	for i := 0; i < maxContainers+20; i++ {
		if i > 0 {
			big.WriteString(",")
		}
		big.WriteString(`{"Id":"x","Names":["/n"],"State":"running","Status":"Up"}`)
	}
	big.WriteString("]")
	if n := len(parseContainerList([]byte(big.String()))); n != maxContainers {
		t.Errorf("cap not applied: got %d, want %d", n, maxContainers)
	}
}

func TestOneStatComputesDockerStyleCPUAndDropsCache(t *testing.T) {
	f := newFakeDocker()
	defer f.srv.Close()
	f.listBody = `[{"Id":"db0000000004","Names":["/db"],"Image":"pg","State":"running","Status":"Up"}]`
	f.stats["db0000000004"] = `{
	  "cpu_stats":{"cpu_usage":{"total_usage":200,"percpu_usage":[1,2]},"system_cpu_usage":2000,"online_cpus":2},
	  "precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},
	  "memory_stats":{"usage":524288000,"limit":1073741824,"stats":{"cache":24288000}}
	}`
	r := probe(context.Background(), f.transport())
	r = sampleStats(context.Background(), f.transport(), r)
	db := r.Containers[0]
	// cpuDelta=100, sysDelta=1000, ncpu=2 -> (100/1000)*2*100 = 20%
	if db.CPUPct < 19.9 || db.CPUPct > 20.1 {
		t.Errorf("CPU%% = %v, want ~20", db.CPUPct)
	}
	if db.MemBytes != 524288000-24288000 {
		t.Errorf("cgroup-v1 cache not subtracted from mem usage: %d", db.MemBytes)
	}
	if db.MemLimitBytes != 1073741824 {
		t.Errorf("mem limit = %d", db.MemLimitBytes)
	}
}

func TestSampleStatsIgnoresNonDockerOrEmpty(t *testing.T) {
	if r := sampleStats(context.Background(), transport{}, Report{Runtime: "kubernetes", Reachable: true}); len(r.Containers) != 0 {
		t.Error("k8s report should pass through Sample untouched")
	}
}

func TestSanitizeStripsControlSequences(t *testing.T) {
	if got := sanitize("\x1b[31mevil\x1b[0m  "); got != "evil" {
		t.Errorf("sanitize = %q, want %q", got, "evil")
	}
}
