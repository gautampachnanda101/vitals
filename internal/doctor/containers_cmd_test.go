package doctor

import (
	"strings"
	"testing"

	"vitals/internal/containers"
	"vitals/internal/ui"
)

func TestRenderContainersReportStates(t *testing.T) {
	t.Run("socket present, daemon wedged", func(t *testing.T) {
		out := ui.StripANSI(captureStdout(t, func() {
			renderContainersReport(containers.Report{Runtime: "docker", Note: "daemon did not respond"}, false)
		}))
		if !strings.Contains(out, "daemon did not respond") {
			t.Errorf("want the note surfaced, got:\n%s", out)
		}
	})

	t.Run("reachable with containers and stats", func(t *testing.T) {
		rep := containers.Report{
			Runtime: "docker", Reachable: true, Endpoint: "/var/run/docker.sock", VMTotalBytes: 8 << 30,
			Containers: []containers.Container{
				{Name: "web", State: "running", Status: "Up 2 hours", CPUPct: 12, MemBytes: 256 << 20, MemLimitBytes: 512 << 20, Ports: []string{"8080→80/tcp"}},
				{Name: "old", State: "exited", Status: "Exited (0) 3 days ago"},
				{Name: "bad", State: "exited", OOMKilled: true, Status: "Exited (137)"},
			},
		}
		out := ui.StripANSI(captureStdout(t, func() { renderContainersReport(rep, false) }))
		for _, want := range []string{"docker via /var/run/docker.sock", "1 running", "2 stopped", "runtime VM: 8.00 GB RAM", "web", "12%", "256.00 MB/512.00 MB", "8080", "bad", "OOMKilled"} {
			if !strings.Contains(out, want) {
				t.Errorf("renderContainersReport output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("k8s pods show namespace", func(t *testing.T) {
		rep := containers.Report{
			Runtime: "kubernetes", Reachable: true, Endpoint: "kind-kind",
			Containers: []containers.Container{{Name: "api-1", Namespace: "prod", State: "Running", Status: "Running", WaitingOn: "CrashLoopBackOff"}},
		}
		out := ui.StripANSI(captureStdout(t, func() { renderContainersReport(rep, true) }))
		if !strings.Contains(out, "prod/api-1") || !strings.Contains(out, "CrashLoopBackOff") {
			t.Errorf("k8s render missing namespace or waiting reason:\n%s", out)
		}
	})
}

// withContainerReports overrides the runtime list RunContainers works
// over, so a test doesn't depend on a live Docker/kube.
func withContainerReports(t *testing.T, reps ...containers.Report) {
	t.Helper()
	orig := containerReports
	containerReports = func([]containers.Report) []containers.Report { return reps }
	t.Cleanup(func() { containerReports = orig })
}

func TestRunContainersShowsEveryRuntimeAndSamplesEach(t *testing.T) {
	withContainerReports(t,
		containers.Report{Runtime: "docker", Reachable: true, Endpoint: "/run/docker.sock",
			Containers: []containers.Container{{Name: "web", State: "running"}}},
		containers.Report{Runtime: "kubernetes", Reachable: true, Endpoint: "kind-kind",
			Containers: []containers.Container{{Name: "api", Namespace: "default", State: "Running"}}},
	)
	origS := containerStatsSampler
	defer func() { containerStatsSampler = origS }()
	sampled := map[string]bool{}
	containerStatsSampler = func(r containers.Report) containers.Report {
		sampled[r.Runtime] = true
		return r
	}

	out := ui.StripANSI(captureStdout(t, func() {
		if code := RunContainers(RunOptions{}, ""); code != 0 {
			t.Errorf("all-healthy -> exit 0, got %d", code)
		}
	}))
	if !sampled["docker"] || !sampled["kubernetes"] {
		t.Errorf("both runtimes should be stats-sampled, got %v", sampled)
	}
	if !strings.Contains(out, "docker") || !strings.Contains(out, "kubernetes") {
		t.Errorf("output should show both runtime sections:\n%s", out)
	}
}

func TestRunContainersRuntimeFilterNarrowsOutput(t *testing.T) {
	withContainerReports(t,
		containers.Report{Runtime: "docker", Reachable: true,
			Containers: []containers.Container{{Name: "web", State: "running"}}},
		containers.Report{Runtime: "kubernetes", Reachable: true,
			Containers: []containers.Container{{Name: "crashy", Namespace: "d", State: "Running", WaitingOn: "CrashLoopBackOff"}}},
	)
	containerStatsSampler = func(r containers.Report) containers.Report { return r }

	out := ui.StripANSI(captureStdout(t, func() {
		if code := RunContainers(RunOptions{}, "kubernetes"); code != 2 {
			t.Errorf("--runtime kubernetes with a crashloop pod -> exit 2, got %d", code)
		}
	}))
	if strings.Contains(out, "web") {
		t.Errorf("--runtime kubernetes must not render the docker section:\n%s", out)
	}
	if !strings.Contains(out, "crashy") {
		t.Errorf("--runtime kubernetes should render the pod:\n%s", out)
	}
}

func TestRunContainersJSONEnvelope(t *testing.T) {
	withContainerReports(t, containers.Report{Runtime: "docker", Reachable: true,
		Containers: []containers.Container{{Name: "web", State: "running"}}})
	containerStatsSampler = func(r containers.Report) containers.Report { return r }

	out := captureStdout(t, func() { _ = RunContainers(RunOptions{JSON: true}, "") })
	if !strings.Contains(out, `"resource": "containers"`) || !strings.Contains(out, `"schema_version"`) {
		t.Errorf("--json envelope missing resource/schema_version:\n%s", out)
	}
}

func TestRunContainersQuietPrintsNothing(t *testing.T) {
	withContainerReports(t, containers.Report{Runtime: "docker", Reachable: true,
		Containers: []containers.Container{{Name: "x", State: "exited", OOMKilled: true}}})
	containerStatsSampler = func(r containers.Report) containers.Report { return r }

	out := captureStdout(t, func() {
		if code := RunContainers(RunOptions{Quiet: true}, ""); code != 2 {
			t.Errorf("an OOM-kill finding is critical -> exit 2, got %d", code)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Errorf("--quiet should print nothing, got:\n%s", out)
	}
}

func TestRunContainersNoRuntime(t *testing.T) {
	withContainerReports(t) // empty
	out := ui.StripANSI(captureStdout(t, func() {
		if code := RunContainers(RunOptions{}, ""); code != 0 {
			t.Errorf("no runtime -> exit 0, got %d", code)
		}
	}))
	if !strings.Contains(out, "no local container runtime") {
		t.Errorf("want the no-runtime line, got:\n%s", out)
	}
}
