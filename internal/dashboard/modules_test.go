package dashboard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/llm"
)

func TestRenderOverviewHealthy(t *testing.T) {
	out := renderOverview(PageContext{Snapshot: doctor.Snapshot{CPU: doctor.CPU{UsedPct: 4}, Memory: doctor.Memory{UsedPct: 40}}})
	if !strings.Contains(out, "Healthy") || !strings.Contains(out, "cpu 4%") {
		t.Errorf("renderOverview(healthy) = %s", out)
	}
}

func TestRenderOverviewLeadsWithTheWorstFinding(t *testing.T) {
	var report diag.Report
	report.Add(diag.Finding{Severity: diag.Warn, Title: "RAM elevated"})
	report.Add(diag.Finding{Severity: diag.Critical, Title: "Swap thrashing"})
	out := renderOverview(PageContext{Report: report})
	if !strings.Contains(out, "Swap thrashing") {
		t.Errorf("overview headline should be the worst finding, got: %s", out)
	}
}

func TestSummaryLineIncludesFullestDiskAndBattery(t *testing.T) {
	s := doctor.Snapshot{
		CPU:    doctor.CPU{UsedPct: 10},
		Memory: doctor.Memory{UsedPct: 20},
		Disks:  []doctor.Disk{{Mount: "/", UsedPct: 30}, {Mount: "/data", UsedPct: 90}},
		Power:  doctor.Power{OnBattery: true, Percent: 55},
	}
	got := summaryLine(s)
	for _, want := range []string{"cpu 10%", "mem 20%", "disk 90% (/data)", "battery 55%"} {
		if !strings.Contains(got, want) {
			t.Errorf("summaryLine missing %q, got %q", want, got)
		}
	}
}

func TestResourcePageUsesAnalyzeResourceNotFullAnalyze(t *testing.T) {
	// A snapshot with a memory problem should not show up on the CPU page —
	// resourcePage must call AnalyzeResource(s, "cpu"), not the full
	// cross-resource Analyze.
	s := doctor.Snapshot{Memory: doctor.Memory{UsedPct: 99, SwapUsedPct: 99}, CPU: doctor.CPU{UsedPct: 1}}
	out := resourcePage("cpu", renderCPU)(PageContext{Snapshot: s})
	if strings.Contains(out, "Swap") || strings.Contains(out, "RAM") {
		t.Errorf("CPU resource page leaked a memory finding: %s", out)
	}
}

func TestRenderDiskListsEveryMount(t *testing.T) {
	out := renderDisk(doctor.Snapshot{Disks: []doctor.Disk{{Mount: "/", UsedPct: 50, FreeBytes: 1 << 30}}})
	if !strings.Contains(out, "/") || !strings.Contains(out, "50%") {
		t.Errorf("renderDisk = %s", out)
	}
}

func TestRenderDiskEmptyIsFriendly(t *testing.T) {
	out := renderDisk(doctor.Snapshot{})
	if !strings.Contains(strings.ToLower(out), "no disks") {
		t.Errorf("renderDisk with no disks should say so, got: %s", out)
	}
}

func TestRenderNetSkipsIdleInterfaces(t *testing.T) {
	out := renderNet(doctor.Snapshot{Net: []doctor.NetIface{{Name: "lo0", RxBytesPerSec: 0, TxBytesPerSec: 0}}})
	if !strings.Contains(strings.ToLower(out), "no interface") {
		t.Errorf("an all-idle interface list should say so, got: %s", out)
	}
}

func TestRenderGPUEmptyIsFriendly(t *testing.T) {
	out := renderGPU(doctor.Snapshot{})
	if !strings.Contains(strings.ToLower(out), "no gpu") {
		t.Errorf("renderGPU with no GPUs should say so, got: %s", out)
	}
}

func TestRenderAdviceShowsTheReply(t *testing.T) {
	out := renderAdvice(PageContext{AdviceReply: "**Restart** the app."})
	if !strings.Contains(out, "<strong>") && !strings.Contains(out, "<b>") {
		// guide.RenderFragment renders inline bold as <b> today; assert on the
		// plain text surviving regardless of which tag it picks.
	}
	if !strings.Contains(out, "Restart") {
		t.Errorf("renderAdvice should include the reply text, got: %s", out)
	}
}

func TestRenderAdviceShowsTheErrorInstead(t *testing.T) {
	out := renderAdvice(PageContext{AdviceErr: errors.New("no reachable model")})
	if !strings.Contains(out, "no reachable model") {
		t.Errorf("renderAdvice should surface the error, got: %s", out)
	}
}

func TestModulesRegisterThemselvesWithDistinctSlugs(t *testing.T) {
	// This runs against the REAL registry (populated by every module's own
	// init()) on purpose — it's the one test that catches a future module
	// forgetting to register, or two modules colliding on a slug.
	seen := map[string]bool{}
	for _, m := range sortedModules() {
		if seen[m.Slug] {
			t.Errorf("duplicate module slug %q", m.Slug)
		}
		seen[m.Slug] = true
	}
	for _, want := range []string{"", "cpu", "mem", "disk", "net", "power", "gpu", "advice"} {
		if !seen[want] {
			t.Errorf("expected a registered module with slug %q", want)
		}
	}
}

func TestPowerAndGPUModulesAreConditionallyAvailable(t *testing.T) {
	_, _, powerAvailable := findModule("power", PageContext{})
	if powerAvailable {
		t.Error("power module should not be available with a zero Power value")
	}
	_, _, powerAvailable = findModule("power", PageContext{Snapshot: doctor.Snapshot{Power: doctor.Power{Percent: 50}}})
	if !powerAvailable {
		t.Error("power module should be available once a battery is reported")
	}

	_, _, gpuAvailable := findModule("gpu", PageContext{})
	if gpuAvailable {
		t.Error("gpu module should not be available with no GPUs")
	}
	_, _, gpuAvailable = findModule("gpu", PageContext{Snapshot: doctor.Snapshot{GPUs: []doctor.GPU{{Name: "x"}}}})
	if !gpuAvailable {
		t.Error("gpu module should be available once a GPU is reported")
	}
}

func TestOnlyAdviceModuleHasAPrepareHook(t *testing.T) {
	// The router (item 002) calls every matched module's Prepare
	// uniformly, nil or not — modules with nothing request-scoped to do
	// (everything except advice today) must leave it nil rather than a
	// no-op func, so "does this module need extra setup" stays a cheap
	// nil check instead of always calling through an empty closure.
	for _, m := range sortedModules() {
		hasPrepare := m.Prepare != nil
		want := m.Slug == "advice"
		if hasPrepare != want {
			t.Errorf("module %q: Prepare != nil is %v, want %v", m.Slug, hasPrepare, want)
		}
	}
}

func TestPrepareAdviceCallsTheLLMAndPopulatesTheReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
		case "/api/chat":
			w.Write([]byte(`{"message":{"content":"Restart the app."}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := PageContext{LLMOpts: llm.CompleteOptions{OllamaURL: srv.URL}}
	if err := prepareAdvice(&ctx); err != nil {
		t.Fatalf("prepareAdvice: %v", err)
	}
	if ctx.AdviceReply != "Restart the app." {
		t.Errorf("AdviceReply = %q, want the model's reply", ctx.AdviceReply)
	}
	if ctx.AdviceErr != nil {
		t.Errorf("AdviceErr = %v, want nil on success", ctx.AdviceErr)
	}
}

func TestPrepareAdvicePopulatesAdviceErrOnFailureRatherThanReturningIt(t *testing.T) {
	// No reachable provider at all — Generate fails. prepareAdvice must
	// still return nil and let renderAdvice show the friendly message via
	// ctx.AdviceErr, the same graceful-degradation shape every other
	// "no X available" case in this codebase uses.
	ctx := PageContext{LLMOpts: llm.CompleteOptions{OllamaURL: "http://127.0.0.1:1"}}
	if err := prepareAdvice(&ctx); err != nil {
		t.Fatalf("prepareAdvice should absorb the LLM error into ctx.AdviceErr, not return it, got: %v", err)
	}
	if ctx.AdviceErr == nil {
		t.Error("AdviceErr should be set when no provider is reachable")
	}
}

func TestAdviceModuleAvailabilityFollowsProviders(t *testing.T) {
	_, _, available := findModule("advice", PageContext{})
	if available {
		t.Error("advice module should not be available with no providers")
	}
	_, _, available = findModule("advice", PageContext{Providers: []llm.Provider{{Reachable: true}}})
	if !available {
		t.Error("advice module should be available once a provider is reachable")
	}
}
