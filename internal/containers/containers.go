// Package containers surfaces a locally-running container runtime or
// Kubernetes when one is present — the diagnostic layer over `docker` /
// `kubectl`, not a reimplementation of them.
//
// It is strictly read-only. The Docker path speaks the Engine API's
// HTTP+JSON over the local unix socket with the standard library only
// (no `github.com/docker/docker` client) and issues nothing but GET
// against a fixed endpoint list. The Kubernetes path shells out to an
// installed `kubectl`, and only when its current context points at a
// loopback / private-range API server — a cloud context is ignored, so
// vitals never handles cluster credentials or makes a network call.
//
// Everything is bounded: a wedged daemon yields "no container data this
// pass", never a hung caller. Container/image/pod names are
// attacker-influenceable text and are sanitised at this boundary.
package containers

import (
	"context"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"vitals/internal/ui"
)

// Container is one container or pod, normalised across runtimes. Stats
// fields (CPUPct/MemBytes/MemLimitBytes) are zero unless a stats sample
// was explicitly requested — see Sample.
type Container struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Image         string   `json:"image"`
	State         string   `json:"state"`  // running, exited, paused, restarting, created, dead (k8s: Running, Pending, ...)
	Status        string   `json:"status"` // human blurb: "Up 3 hours", "Exited (137) 5 minutes ago"
	RestartCount  int      `json:"restart_count"`
	OOMKilled     bool     `json:"oom_killed,omitempty"`
	ExitCode      int      `json:"exit_code,omitempty"`
	Health        string   `json:"health,omitempty"`          // healthy, unhealthy, starting, or ""
	WaitingOn     string   `json:"waiting_on,omitempty"`      // k8s: CrashLoopBackOff, ImagePullBackOff, ...
	ComposeProj   string   `json:"compose_project,omitempty"` // docker: com.docker.compose.project label
	Namespace     string   `json:"namespace,omitempty"`       // k8s only
	Ports         []string `json:"ports,omitempty"`           // docker: published "host→container/proto" mappings
	CreatedUnix   int64    `json:"created_unix,omitempty"`    // docker: container creation time, unix seconds
	CPUPct        float64  `json:"cpu_percent,omitempty"`
	MemBytes      uint64   `json:"mem_bytes,omitempty"`
	MemLimitBytes uint64   `json:"mem_limit_bytes,omitempty"`
}

// Running reports whether this container is in a live state.
func (c Container) Running() bool {
	return strings.EqualFold(c.State, "running")
}

// Report is the whole container-runtime picture for one snapshot.
type Report struct {
	Runtime      string      `json:"runtime"`                  // "docker", "kubernetes", or "" when none is reachable
	Reachable    bool        `json:"reachable"`                // a runtime was found and answered
	Endpoint     string      `json:"endpoint,omitempty"`       // the socket path or kube context that answered
	VMTotalBytes uint64      `json:"vm_total_bytes,omitempty"` // Engine API /info MemTotal — the runtime VM's RAM ceiling
	Containers   []Container `json:"containers,omitempty"`
	Note         string      `json:"note,omitempty"` // why nothing was returned despite a socket (e.g. daemon not responding)
}

// Counts summarises Containers by state for a one-line headline.
func (r Report) Counts() (running, stopped, unhealthy int) {
	for _, c := range r.Containers {
		switch {
		case c.Running():
			running++
		default:
			stopped++
		}
		if c.Health == "unhealthy" || c.WaitingOn != "" || c.OOMKilled {
			unhealthy++
		}
	}
	return
}

// pingTimeout bounds runtime detection; probeTimeout bounds the whole
// list fetch. Both are short — this rides the snapshot refresh and must
// never be what makes `doctor` slow.
const (
	pingTimeout  = 300 * time.Millisecond
	probeTimeout = 2 * time.Second
)

// maxContainers caps how many containers are carried into a Report, so a
// host running hundreds can't turn a render or a stats fan-out into a
// problem. Newest first (the Engine API's default order).
const maxContainers = 50

// transport is the injected OS surface: the Docker socket dialer, the
// kubectl exec, and PATH lookup. Tests substitute an httptest server and
// canned command output so the Engine-API and kubectl-JSON parsing are
// exercised with no daemon.
type transport struct {
	goos string
	// dockerEndpoint returns the local Engine API endpoint to try — a unix
	// socket path on Linux/macOS, a named pipe on Windows — or "" when
	// none is configured/present. Platform-specific (endpoint_{unix,windows}.go).
	dockerEndpoint func() string
	// dialDocker dials that endpoint. Platform-specific: a unix-socket
	// dial, or winio.DialPipeContext on Windows.
	dialDocker func(ctx context.Context, endpoint string) (net.Conn, error)
	lookPath   func(string) (string, error)
	run        func(ctx context.Context, name string, args ...string) ([]byte, error)
}

var defaultTransport = transport{
	goos:           runtime.GOOS,
	dockerEndpoint: defaultDockerEndpoint,
	dialDocker:     dialDockerDefault,
	lookPath:       execLookPath,
	run:            execRun,
}

// Probe returns the container-runtime picture: Docker if its local
// socket/pipe answers, else a local Kubernetes via kubectl, else an
// empty Report (Runtime ""). It never returns an error — an unreachable
// or wedged runtime is data (Report.Note), not a failure that should
// bubble up through a snapshot collector.
func Probe(ctx context.Context) Report { return ProbeRuntime(ctx, "") }

// ProbeAll returns every locally-reachable runtime, not just the first.
// A machine running kind/k3s/minikube has BOTH a Docker daemon (the
// cluster nodes run on it) and a Kubernetes API at once — a single-pick
// probe hides one of them. The result has 0, 1 or 2 entries, Docker
// first; an empty slice means no runtime at all.
func ProbeAll(ctx context.Context) []Report { return probeAll(ctx, defaultTransport) }

func probeAll(ctx context.Context, t transport) []Report {
	// A non-nil slice so `doctor --json`'s snapshot.containers is always
	// a JSON array ([]), never null — consumers can iterate it blindly.
	out := []Report{}
	if r, ok := probeDocker(ctx, t); ok {
		out = append(out, r)
	}
	if r, ok := probeKubernetes(ctx, t); ok {
		out = append(out, r)
	}
	return out
}

// ProbeRuntime is Probe with an explicit runtime preference: "" is the
// default docker-first auto-detect; "docker" or "kubernetes" probes only
// that one. The override matters when both are local at once — a
// kind/k3s/minikube cluster whose nodes are themselves Docker
// containers, where the auto path would stop at Docker and never show
// the pods.
func ProbeRuntime(ctx context.Context, want string) Report {
	return probeRuntime(ctx, defaultTransport, want)
}

func probeRuntime(ctx context.Context, t transport, want string) Report {
	if want != "kubernetes" {
		if r, ok := probeDocker(ctx, t); ok {
			return r
		}
		if want == "docker" {
			return Report{}
		}
	}
	if r, ok := probeKubernetes(ctx, t); ok {
		return r
	}
	return Report{}
}

// probe keeps the old signature for the existing tests (auto-detect).
func probe(ctx context.Context, t transport) Report { return probeRuntime(ctx, t, "") }

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// sanitize scrubs a name/image/label coming from the daemon before it
// reaches any renderer — the same choke point every other
// attacker-influenceable string in vitals goes through.
func sanitize(s string) string { return ui.Truncate(ui.Sanitize(strings.TrimSpace(s)), 200) }

// newDockerHTTP builds an *http.Client whose transport dials the given
// unix socket for every request; base is the scheme+host to prefix onto
// Engine API paths ("http://docker").
func newDockerHTTP(t transport, socket string) (*http.Client, string) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return t.dialDocker(ctx, socket)
		},
		DisableKeepAlives: true,
	}
	return &http.Client{Transport: tr}, "http://docker"
}
