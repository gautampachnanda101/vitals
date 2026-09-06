package containers

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIsLocalAPIServer(t *testing.T) {
	local := []string{
		"https://127.0.0.1:6443",
		"https://localhost:6443",
		"https://192.168.65.3:6443",
		"https://10.96.0.1",
		"https://172.18.0.2:6443",
		"https://100.64.1.1:6443", // CGNAT, k3s/VM
		"https://kubernetes.docker.internal:6443",
		"https://my-cluster.local:8443",
	}
	for _, u := range local {
		if !isLocalAPIServer(u) {
			t.Errorf("%s should be treated as local", u)
		}
	}
	remote := []string{
		"",
		"https://ABCD1234.gr7.us-east-1.eks.amazonaws.com",
		"https://34.72.11.9",
		"https://mycluster.k8s.example.com:443",
		"https://8.8.8.8:6443",
	}
	for _, u := range remote {
		if isLocalAPIServer(u) {
			t.Errorf("%s must NOT be treated as local", u)
		}
	}
}

// kubeStub returns a transport whose run() answers the three kubectl
// invocations probeKubernetes makes, in order of URL args.
func kubeStub(ctxName, server, pods string, listErr error) transport {
	return transport{
		goos:     "linux",
		lookPath: func(string) (string, error) { return "/usr/local/bin/kubectl", nil },
		// no docker
		dockerEndpoint: func() string { return "" },
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			switch {
			case len(args) >= 2 && args[0] == "config" && args[1] == "current-context":
				return []byte(ctxName + "\n"), nil
			case len(args) >= 2 && args[0] == "config" && args[1] == "view":
				return []byte(server), nil
			case len(args) >= 1 && args[0] == "get":
				return []byte(pods), listErr
			}
			return nil, errors.New("unexpected kubectl call: " + name + " " + strings.Join(args, " "))
		},
	}
}

func TestProbeKubernetesLocalContextParsesPods(t *testing.T) {
	pods := `{"items":[
	  {"metadata":{"name":"web-abc","namespace":"default","labels":{"app.kubernetes.io/name":"web"}},
	   "spec":{"containers":[{"image":"nginx:1.25"}]},
	   "status":{"phase":"Running","containerStatuses":[{"name":"web","image":"nginx:1.25","ready":true,"restartCount":0,"state":{"running":{}}}]}},
	  {"metadata":{"name":"job-xyz","namespace":"batch"},
	   "spec":{"containers":[{"image":"job:1"}]},
	   "status":{"phase":"Running","containerStatuses":[{"name":"job","ready":false,"restartCount":7,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}},
	  {"metadata":{"name":"oom-1","namespace":"default"},
	   "spec":{"containers":[{"image":"hungry:1"}]},
	   "status":{"phase":"Failed","containerStatuses":[{"name":"h","ready":false,"restartCount":2,"state":{"terminated":{"reason":"OOMKilled"}}}]}}
	]}`
	r := probe(context.Background(), kubeStub("kind-kind", "https://127.0.0.1:6443", pods, nil))
	if r.Runtime != "kubernetes" || !r.Reachable || r.Endpoint != "kind-kind" {
		t.Fatalf("want a reachable local k8s report, got %+v", r)
	}
	if len(r.Containers) != 3 {
		t.Fatalf("want 3 pods, got %d: %+v", len(r.Containers), r.Containers)
	}
	web, job, oom := r.Containers[0], r.Containers[1], r.Containers[2]
	if !web.Running() || web.Health == "unhealthy" || web.Image != "nginx:1.25" || web.ComposeProj != "web" {
		t.Errorf("web pod parsed wrong: %+v", web)
	}
	if job.WaitingOn != "CrashLoopBackOff" || job.RestartCount != 7 {
		t.Errorf("crashloop pod parsed wrong: %+v", job)
	}
	if job.Health != "unhealthy" || !strings.Contains(job.Status, "0/1 ready") {
		t.Errorf("running-but-not-ready pod should read unhealthy with a ready count: %+v", job)
	}
	if !oom.OOMKilled || oom.RestartCount != 2 {
		t.Errorf("oomkilled pod parsed wrong: %+v", oom)
	}
	_, _, unhealthy := r.Counts()
	if unhealthy != 3 { // crashloop + not-ready(job) collapse to one; oom; ... actually job counts once
		// job: WaitingOn set -> counts; oom: OOMKilled -> counts; web: fine.
		if unhealthy != 2 {
			t.Errorf("Counts unhealthy = %d, want 2", unhealthy)
		}
	}
}

func TestProbeKubernetesIgnoresCloudContext(t *testing.T) {
	r := probe(context.Background(), kubeStub("prod-eks", "https://abc.eks.amazonaws.com", `{"items":[]}`, nil))
	if r.Runtime != "" {
		t.Errorf("a cloud kube context must be ignored entirely, got %+v", r)
	}
}

func TestProbeKubernetesNoKubectl(t *testing.T) {
	tr := transport{
		goos:           "linux",
		dockerEndpoint: func() string { return "" },
		lookPath:       func(string) (string, error) { return "", errors.New("not found") },
	}
	if r := probe(context.Background(), tr); r.Runtime != "" {
		t.Errorf("no kubectl -> empty report, got %+v", r)
	}
}

func TestProbeKubernetesListErrorKeepsRuntimeWithNote(t *testing.T) {
	r := probe(context.Background(), kubeStub("kind-kind", "https://127.0.0.1:6443", "", errors.New("timeout\nmore")))
	if r.Runtime != "kubernetes" || r.Reachable {
		t.Errorf("a list error should give Runtime=kubernetes, Reachable=false: %+v", r)
	}
	if !strings.Contains(r.Note, "timeout") || strings.Contains(r.Note, "more") {
		t.Errorf("Note should carry just the first line of the error: %q", r.Note)
	}
}

func TestParsePodsGarbage(t *testing.T) {
	if got := parsePods([]byte("{{{")); got != nil {
		t.Errorf("bad JSON -> nil, got %+v", got)
	}
}

func TestProbeDockerWinsOverKubernetes(t *testing.T) {
	f := newFakeDocker()
	defer f.srv.Close()
	tr := f.transport()
	tr.lookPath = func(string) (string, error) { return "/bin/kubectl", nil } // both available
	r := probe(context.Background(), tr)
	if r.Runtime != "docker" {
		t.Errorf("docker should be probed first, got %+v", r)
	}
}
