package doctor

import (
	"strings"
	"testing"

	"vitals/internal/containers"
	"vitals/internal/ui"
)

func TestRenderContainersStates(t *testing.T) {
	t.Run("no runtime", func(t *testing.T) {
		out := ui.StripANSI(captureStdout(t, func() { renderContainers(containers.Report{}, false) }))
		if !strings.Contains(out, "no local container runtime") {
			t.Errorf("want the no-runtime line, got:\n%s", out)
		}
	})

	t.Run("socket present, daemon wedged", func(t *testing.T) {
		out := ui.StripANSI(captureStdout(t, func() {
			renderContainers(containers.Report{Runtime: "docker", Note: "daemon did not respond"}, false)
		}))
		if !strings.Contains(out, "daemon did not respond") {
			t.Errorf("want the note surfaced, got:\n%s", out)
		}
	})

	t.Run("reachable with containers and stats", func(t *testing.T) {
		rep := containers.Report{
			Runtime: "docker", Reachable: true, Endpoint: "/var/run/docker.sock", VMTotalBytes: 8 << 30,
			Containers: []containers.Container{
				{Name: "web", State: "running", Status: "Up 2 hours", CPUPct: 12, MemBytes: 256 << 20, MemLimitBytes: 512 << 20},
				{Name: "old", State: "exited", Status: "Exited (0) 3 days ago"},
				{Name: "bad", State: "exited", OOMKilled: true, Status: "Exited (137)"},
			},
		}
		out := ui.StripANSI(captureStdout(t, func() { renderContainers(rep, false) }))
		for _, want := range []string{"docker via /var/run/docker.sock", "1 running", "2 stopped", "runtime VM: 8.00 GB RAM", "web", "12%", "256.00 MB/512.00 MB", "bad", "OOMKilled"} {
			if !strings.Contains(out, want) {
				t.Errorf("renderContainers output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("k8s pods show namespace", func(t *testing.T) {
		rep := containers.Report{
			Runtime: "kubernetes", Reachable: true, Endpoint: "kind-kind",
			Containers: []containers.Container{{Name: "api-1", Namespace: "prod", State: "Running", Status: "Running", WaitingOn: "CrashLoopBackOff"}},
		}
		out := ui.StripANSI(captureStdout(t, func() { renderContainers(rep, true) }))
		if !strings.Contains(out, "prod/api-1") || !strings.Contains(out, "CrashLoopBackOff") {
			t.Errorf("k8s render missing namespace or waiting reason:\n%s", out)
		}
	})
}

func TestRunContainersJSONAndSamplerSeam(t *testing.T) {
	orig := containerStatsSampler
	defer func() { containerStatsSampler = orig }()
	sampled := false
	containerStatsSampler = func(r containers.Report) containers.Report {
		sampled = true
		return r
	}

	code := RunContainers(RunOptions{JSON: true, Quiet: false})
	if sampled != true {
		t.Error("RunContainers should always run the stats sampler seam")
	}
	// On a machine with no runtime the report is empty and there are no
	// findings, so the exit code is 0.
	if code != 0 {
		t.Errorf("no runtime -> exit 0, got %d", code)
	}
}

func TestRunContainersQuietReturnsExitCodeOnly(t *testing.T) {
	orig := containerStatsSampler
	defer func() { containerStatsSampler = orig }()
	containerStatsSampler = func(r containers.Report) containers.Report {
		r.Runtime, r.Reachable = "docker", true
		r.Containers = []containers.Container{{Name: "x", State: "exited", OOMKilled: true}}
		return r
	}
	out := captureStdout(t, func() {
		if code := RunContainers(RunOptions{Quiet: true}); code != 2 {
			t.Errorf("an OOM-kill finding is critical -> exit 2, got %d", code)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("--quiet should print nothing, got:\n%s", out)
	}
}
