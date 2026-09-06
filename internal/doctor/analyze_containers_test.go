package doctor

import (
	"strings"
	"testing"

	"vitals/internal/containers"
	"vitals/internal/diag"
)

func findingTitled(r diag.Report, substr string) (diag.Finding, bool) {
	for _, f := range r.Findings {
		if strings.Contains(f.Title, substr) {
			return f, true
		}
	}
	return diag.Finding{}, false
}

func TestAnalyzeContainersSilentWhenNoRuntime(t *testing.T) {
	var r diag.Report
	analyzeContainers(&r, Snapshot{}) // zero Containers -> not reachable
	if len(r.Findings) != 0 {
		t.Errorf("no runtime should raise nothing, got %+v", r.Findings)
	}
	// A reachable runtime with only healthy containers is also silent.
	r = diag.Report{}
	analyzeContainers(&r, Snapshot{Containers: []containers.Report{{
		Runtime: "docker", Reachable: true,
		Containers: []containers.Container{{Name: "web", State: "running", Health: "healthy"}},
	}}})
	if len(r.Findings) != 0 {
		t.Errorf("all-healthy should raise nothing, got %+v", r.Findings)
	}
}

func TestAnalyzeContainersOOMKillIsCritical(t *testing.T) {
	var r diag.Report
	analyzeContainers(&r, Snapshot{Containers: []containers.Report{{
		Runtime: "docker", Reachable: true,
		Containers: []containers.Container{{Name: "worker", State: "exited", OOMKilled: true, ExitCode: 137}},
	}}})
	f, ok := findingTitled(r, "worker was OOM-killed")
	if !ok {
		t.Fatalf("expected an OOM-kill finding, got %+v", r.Findings)
	}
	if f.Severity != diag.Critical {
		t.Errorf("OOM-kill should be critical, got %v", f.Severity)
	}
	if !strings.Contains(f.Detail, "exit 137") {
		t.Errorf("detail should carry the exit code: %q", f.Detail)
	}
	if !strings.Contains(strings.Join(f.Fixes, " "), "docker logs worker") {
		t.Errorf("fixes should point at `docker logs`: %v", f.Fixes)
	}
}

func TestAnalyzeContainersCrashLoopIsCriticalWithKubectlFixes(t *testing.T) {
	var r diag.Report
	analyzeContainers(&r, Snapshot{Containers: []containers.Report{{
		Runtime: "kubernetes", Reachable: true,
		Containers: []containers.Container{{Name: "api-7c9", Namespace: "prod", State: "Running", WaitingOn: "CrashLoopBackOff"}},
	}}})
	f, ok := findingTitled(r, "api-7c9 is stuck in CrashLoopBackOff")
	if !ok {
		t.Fatalf("expected a crashloop finding, got %+v", r.Findings)
	}
	if f.Severity != diag.Critical {
		t.Errorf("crashloop should be critical")
	}
	joined := strings.Join(f.Fixes, " | ")
	if !strings.Contains(joined, "kubectl describe pod api-7c9 -n prod") || !strings.Contains(joined, "--previous") {
		t.Errorf("fixes should be namespaced kubectl commands: %v", f.Fixes)
	}
}

func TestAnalyzeContainersRestartLoopAndUnhealthyAreWarnings(t *testing.T) {
	var r diag.Report
	analyzeContainers(&r, Snapshot{Containers: []containers.Report{{
		Runtime: "docker", Reachable: true,
		Containers: []containers.Container{
			{Name: "flappy", State: "running", RestartCount: 12},
			{Name: "sick", State: "running", Health: "unhealthy"},
			{Name: "fine", State: "running", RestartCount: 2},
		},
	}}})
	if f, ok := findingTitled(r, "flappy has restarted 12 times"); !ok || f.Severity != diag.Warn {
		t.Errorf("restart loop should be a warning, got ok=%v %+v", ok, f)
	}
	if f, ok := findingTitled(r, "sick is unhealthy"); !ok || f.Severity != diag.Warn {
		t.Errorf("unhealthy should be a warning, got ok=%v %+v", ok, f)
	}
	if _, ok := findingTitled(r, "fine"); ok {
		t.Errorf("a container with 2 restarts and no other problem should not raise anything")
	}
}

func TestAnalyzeContainersVMRAMHogOnlyWhenMemoryIsTight(t *testing.T) {
	base := Snapshot{Containers: []containers.Report{{Runtime: "docker", Reachable: true, VMTotalBytes: 8 << 30}}}

	var loose diag.Report
	base.Memory.UsedPct = 40
	analyzeContainers(&loose, base)
	if _, ok := findingTitled(loose, "Container runtime VM is holding"); ok {
		t.Error("VM-RAM finding must not fire when memory isn't under pressure")
	}

	var tight diag.Report
	base.Memory.UsedPct = 92
	analyzeContainers(&tight, base)
	f, ok := findingTitled(tight, "Container runtime VM is holding")
	if !ok || f.Severity != diag.Warn {
		t.Fatalf("VM-RAM finding should fire (warning) when memory is tight, got ok=%v %+v", ok, f)
	}
	if !strings.Contains(f.Detail, "won't show up ranked in the host process list") {
		t.Errorf("detail should explain the host-scan blind spot: %q", f.Detail)
	}
}

func TestAnalyzeContainersAcrossBothRuntimes(t *testing.T) {
	var r diag.Report
	analyzeContainers(&r, Snapshot{Containers: []containers.Report{
		{Runtime: "docker", Reachable: true,
			Containers: []containers.Container{{Name: "worker", State: "exited", OOMKilled: true}}},
		{Runtime: "kubernetes", Reachable: true,
			Containers: []containers.Container{{Name: "api", Namespace: "prod", State: "Running", WaitingOn: "CrashLoopBackOff"}}},
	}})
	if _, ok := findingTitled(r, "worker was OOM-killed"); !ok {
		t.Error("the docker runtime's OOM finding is missing")
	}
	if _, ok := findingTitled(r, "api is stuck in CrashLoopBackOff"); !ok {
		t.Error("the kubernetes runtime's crashloop finding is missing")
	}
	// an unreachable runtime in the list contributes nothing, doesn't panic
	r = diag.Report{}
	analyzeContainers(&r, Snapshot{Containers: []containers.Report{{Runtime: "docker", Note: "wedged"}}})
	if len(r.Findings) != 0 {
		t.Errorf("an unreachable runtime should raise nothing, got %+v", r.Findings)
	}
}

func TestAnalyzeContainersRunsViaAnalyzeAndAnalyzeResource(t *testing.T) {
	s := Snapshot{Containers: []containers.Report{{
		Runtime: "docker", Reachable: true,
		Containers: []containers.Container{{Name: "x", State: "exited", OOMKilled: true}},
	}}}
	if _, ok := findingTitled(Analyze(s), "OOM-killed"); !ok {
		t.Error("Analyze should include container findings")
	}
	if _, ok := findingTitled(AnalyzeResource(s, "containers"), "OOM-killed"); !ok {
		t.Error("AnalyzeResource(\"containers\") should run analyzeContainers")
	}
	if _, ok := findingTitled(AnalyzeResource(s, "cpu"), "OOM-killed"); ok {
		t.Error("AnalyzeResource(\"cpu\") must not include container findings")
	}
}
