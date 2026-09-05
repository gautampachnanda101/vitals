package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/llm"
	"vitals/internal/monitor"
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

func TestRenderOverviewShowsResourceCardsForEveryAvailableResource(t *testing.T) {
	s := doctor.Snapshot{
		CPU:    doctor.CPU{UsedPct: 12},
		Memory: doctor.Memory{UsedPct: 34},
		Disks:  []doctor.Disk{{Mount: "/", UsedPct: 56}},
		Net:    []doctor.NetIface{{Name: "en0", RxBytesPerSec: 1000, TxBytesPerSec: 200}},
	}
	out := renderOverview(PageContext{Snapshot: s})
	for _, want := range []string{`href="/cpu"`, `href="/mem"`, `href="/disk"`, `href="/net"`, "12%", "34%", "56%", "en0"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderOverview missing %q, got: %s", want, out)
		}
	}
	// No battery/GPU on this snapshot — those cards must not appear.
	if strings.Contains(out, `href="/power"`) || strings.Contains(out, `href="/gpu"`) {
		t.Errorf("renderOverview should omit power/GPU cards when unavailable, got: %s", out)
	}
}

func TestRenderOverviewShowsPowerAndGPUCardsWhenAvailable(t *testing.T) {
	s := doctor.Snapshot{
		Power: doctor.Power{OnBattery: true, Percent: 71, MinutesLeft: 125},
		GPUs:  []doctor.GPU{{Name: "Apple M3 Max", UtilPct: 22}},
	}
	out := renderOverview(PageContext{Snapshot: s})
	for _, want := range []string{`href="/power"`, `href="/gpu"`, "71%", "Apple M3 Max"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderOverview missing %q, got: %s", want, out)
		}
	}
}

func TestRenderOverviewShowsLoadedModelsOnlyWhenPresent(t *testing.T) {
	withModels := renderOverview(PageContext{Snapshot: doctor.Snapshot{
		LLM: []doctor.LLMModel{{Name: "llama3.1:70b", OffloadPct: 95}},
	}})
	if !strings.Contains(withModels, "llama3.1:70b") || !strings.Contains(withModels, "Loaded models") {
		t.Errorf("renderOverview should show a loaded model, got: %s", withModels)
	}

	noModels := renderOverview(PageContext{})
	if strings.Contains(noModels, "Loaded models") {
		t.Errorf("renderOverview should omit the Loaded models section with none loaded, got: %s", noModels)
	}
}

func TestModelCardPillReflectsOffload(t *testing.T) {
	cases := []struct {
		offload  float64
		wantPill string
	}{
		{100, "fully on GPU"},
		{50, "50% GPU"},
		{0, "CPU-only"},
	}
	for _, c := range cases {
		out := modelCard(doctor.LLMModel{Name: "x", OffloadPct: c.offload})
		if !strings.Contains(out, c.wantPill) {
			t.Errorf("modelCard(offload=%.0f) missing pill %q, got: %s", c.offload, c.wantPill, out)
		}
	}
}

func TestRenderOverviewShowsQuickActions(t *testing.T) {
	out := renderOverview(PageContext{})
	for _, want := range []string{`href="/clean"`, `href="/dupes"`, `href="/advice"`, "Quick actions"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderOverview missing quick action %q, got: %s", want, out)
		}
	}
}

func TestDiskCardSeverityMatchesConfigDefaultThresholds(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{50, "ok"},
		{92, "warning"},
		{98, "critical"},
	}
	for _, c := range cases {
		if got := diskCardSeverity(c.pct); got != c.want {
			t.Errorf("diskCardSeverity(%.0f) = %q, want %q", c.pct, got, c.want)
		}
	}
}

func TestBusiestNetPicksHighestCombinedThroughputAndSkipsIdle(t *testing.T) {
	n, ok := busiestNet([]doctor.NetIface{
		{Name: "idle0", RxBytesPerSec: 0, TxBytesPerSec: 0},
		{Name: "en0", RxBytesPerSec: 500, TxBytesPerSec: 100},
		{Name: "en1", RxBytesPerSec: 5000, TxBytesPerSec: 1000},
	})
	if !ok || n.Name != "en1" {
		t.Errorf("busiestNet = %+v, %v, want en1", n, ok)
	}
	if _, ok := busiestNet([]doctor.NetIface{{Name: "idle0"}}); ok {
		t.Error("busiestNet should report not-found when every interface is idle")
	}
}

func TestResourcePageUsesAnalyzeResourceNotFullAnalyze(t *testing.T) {
	// A snapshot with a memory problem should not show up on the CPU page —
	// resourcePage must call AnalyzeResource(s, "cpu"), not the full
	// cross-resource Analyze. The process cache is faked empty so this
	// only exercises resourcePage's own AnalyzeResource wiring, not
	// whatever processes happen to be running on the machine executing
	// this test (the "Top processes" table's own RAM column is
	// legitimate CPU-page content, not a leaked finding, and would
	// otherwise trip this same "RAM" substring check).
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	s := doctor.Snapshot{Memory: doctor.Memory{UsedPct: 99, SwapUsedPct: 99}, CPU: doctor.CPU{UsedPct: 1}}
	out := resourcePage("cpu", renderCPU)(PageContext{Snapshot: s})
	if strings.Contains(out, "Swap") || strings.Contains(out, "RAM") {
		t.Errorf("CPU resource page leaked a memory finding: %s", out)
	}
}

func TestRenderCPUShowsOptionalRowsWhenPresent(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	out := renderCPU(doctor.Snapshot{CPU: doctor.CPU{
		UsedPct: 40, IOWaitPct: 5, Load1: 1.2, Cores: 8, FreqMHz: 3200,
		TopProc: doctor.ProcRef{Name: "chrome", PID: 42, CPUPct: 30},
	}})
	for _, want := range []string{"40%", "1.2", "8 cores", "3200 MHz", "chrome", "42", "30%"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCPU missing %q, got: %s", want, out)
		}
	}
}

func TestRenderCPUOmitsOptionalRowsWhenAbsent(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	out := renderCPU(doctor.Snapshot{CPU: doctor.CPU{UsedPct: 4}})
	if strings.Contains(out, "Clock") || strings.Contains(out, "Top process") {
		t.Errorf("renderCPU should omit rows for zero-value fields, got: %s", out)
	}
}

func TestRenderMemShowsOptionalRowsWhenPresent(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	out := renderMem(doctor.Snapshot{Memory: doctor.Memory{
		UsedPct: 78, AvailablePct: 15, SwapUsedPct: 20,
		TopProc: doctor.ProcRef{Name: "Code Helper", PID: 99, RSSBytes: 500 << 20},
	}})
	for _, want := range []string{"78%", "15%", "20%", "Code Helper", "99", "MB"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderMem missing %q, got: %s", want, out)
		}
	}
}

func TestRenderMemOmitsOptionalRowsWhenAbsent(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	out := renderMem(doctor.Snapshot{Memory: doctor.Memory{UsedPct: 40}})
	if strings.Contains(out, "Available") || strings.Contains(out, "Top process") {
		t.Errorf("renderMem should omit rows for zero-value fields, got: %s", out)
	}
}

func TestRenderPowerShowsOptionalRowsWhenPresent(t *testing.T) {
	out := renderPower(doctor.Snapshot{Power: doctor.Power{OnBattery: true, Percent: 62, MinutesLeft: 90}})
	for _, want := range []string{"battery", "62%", "90 min"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderPower missing %q, got: %s", want, out)
		}
	}
}

func TestRenderPowerOnACWithNoEstimate(t *testing.T) {
	out := renderPower(doctor.Snapshot{Power: doctor.Power{OnBattery: false}})
	if !strings.Contains(out, "AC power") {
		t.Errorf("renderPower should report AC power when not on battery, got: %s", out)
	}
	if strings.Contains(out, "Charge") || strings.Contains(out, "Remaining") {
		t.Errorf("renderPower should omit charge/remaining rows when unknown, got: %s", out)
	}
}

func TestRenderNetShowsActiveInterfaces(t *testing.T) {
	out := renderNet(doctor.Snapshot{Net: []doctor.NetIface{
		{Name: "en0", RxBytesPerSec: 2 << 20, TxBytesPerSec: 1 << 20},
		{Name: "lo0", RxBytesPerSec: 0, TxBytesPerSec: 0}, // idle, should be skipped
	}})
	if !strings.Contains(out, "en0") {
		t.Errorf("renderNet should list the active interface, got: %s", out)
	}
	if strings.Contains(out, "lo0") {
		t.Errorf("renderNet should skip the idle interface, got: %s", out)
	}
}

func TestRenderGPUListsEveryDevice(t *testing.T) {
	out := renderGPU(doctor.Snapshot{GPUs: []doctor.GPU{
		{Name: "RTX 4090", UtilPct: 80, VRAMUsed: 10 << 30, VRAMTotal: 24 << 30},
	}})
	for _, want := range []string{"RTX 4090", "80%", "GB"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderGPU missing %q, got: %s", want, out)
		}
	}
}

func TestRenderGPUListsProcessesHoldingVRAMWhenKnown(t *testing.T) {
	out := renderGPU(doctor.Snapshot{GPUs: []doctor.GPU{{
		Name: "RTX 4090", UtilPct: 40, VRAMUsed: 8 << 30, VRAMTotal: 24 << 30,
		Processes: []doctor.GPUProc{
			{PID: 111, Name: "ollama", VRAMUsed: 6 << 30},
			{PID: 222, Name: "python", VRAMUsed: 2 << 30},
		},
	}}})
	for _, want := range []string{"Processes holding VRAM", "ollama", "111", "python", "222", "6.00 GB"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderGPU should list per-process VRAM, missing %q, got: %s", want, out)
		}
	}
	// heaviest first
	if strings.Index(out, "ollama") > strings.Index(out, "python") {
		t.Errorf("renderGPU should sort VRAM processes heaviest-first, got: %s", out)
	}
}

func TestRenderGPUOmitsProcessSectionWhenNoPerProcessData(t *testing.T) {
	out := renderGPU(doctor.Snapshot{GPUs: []doctor.GPU{{
		Name: "RTX 4090", UtilPct: 40, VRAMUsed: 8 << 30, VRAMTotal: 24 << 30,
	}}})
	if strings.Contains(out, "Processes holding VRAM") {
		t.Errorf("renderGPU should not show an empty VRAM-process section, got: %s", out)
	}
}

func TestRenderGPUShowsLiveRAMPressureForAppleUnifiedMemory(t *testing.T) {
	// A real user report: an Apple Silicon GPU (no discrete VRAM to
	// report — see internal/gpu/gpu.go's parseIORegApple) rendered as
	// "0% util, 0 B / 0 B VRAM", which reads as broken telemetry. A first
	// fix that replaced it with a sentence pointing at the Memory page
	// ("go look elsewhere") was rightly rejected as still not actionable —
	// GPU and RAM are the same pool here, so this page shows that live
	// pressure directly: the same numbers/format renderMem uses.
	s := doctor.Snapshot{
		GPUs:   []doctor.GPU{{Name: "Apple M3 Pro"}},
		Memory: doctor.Memory{UsedPct: 86, TopProc: doctor.ProcRef{Name: "llama-server", PID: 123, RSSBytes: 2 << 30}},
	}
	out := renderGPU(s)
	if strings.Contains(out, "0 B / 0 B") || strings.Contains(out, "0% util") {
		t.Errorf("renderGPU should never print a bare zero VRAM/util reading, got: %s", out)
	}
	if !strings.Contains(out, "unified memory") {
		t.Errorf("an Apple GPU with no VRAM reading should explain why (unified memory), got: %s", out)
	}
	for _, want := range []string{"86%", "llama-server", "pid 123"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderGPU should show the actual live RAM pressure (same as renderMem), missing %q, got: %s", want, out)
		}
	}
}

func TestRenderGPUUnknownVendorWithNoTelemetrySaysSoRatherThanZero(t *testing.T) {
	out := renderGPU(doctor.Snapshot{GPUs: []doctor.GPU{{Name: "Some GPU"}}})
	if strings.Contains(out, "0 B / 0 B") || strings.Contains(out, "0% util") {
		t.Errorf("renderGPU should never print a bare zero VRAM/util reading, got: %s", out)
	}
	if !strings.Contains(out, "no utilisation/VRAM telemetry") {
		t.Errorf("a non-Apple GPU with no telemetry should say so explicitly, got: %s", out)
	}
}

func TestResourcePageHeadlinesTheWorstFindingWhenThereIsOne(t *testing.T) {
	// The zero-findings path is already covered by
	// TestResourcePageUsesAnalyzeResourceNotFullAnalyze; this covers the
	// other branch — a resource with a real problem should headline it,
	// not the generic "No issues found".
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	s := doctor.Snapshot{CPU: doctor.CPU{UsedPct: 99, Load1: 20, Cores: 2}}
	out := resourcePage("cpu", renderCPU)(PageContext{Snapshot: s})
	if strings.Contains(out, "No issues found") {
		t.Errorf("resourcePage should headline the actual finding, not the healthy default, got: %s", out)
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

func TestRenderAdviceAlwaysShowsHeuristicFindingsAndAPlaceholderForTheLLM(t *testing.T) {
	// renderAdvice no longer knows anything about the LLM's state at all —
	// that's the whole point of the AsyncFragment split (it can't block on
	// a slow/unreachable LLM if it never calls one). It always shows the
	// heuristic findings plus a placeholder the client-side fetch replaces.
	report := diag.Report{Findings: []diag.Finding{{Severity: diag.Critical, Title: "disk nearly full", Fixes: []string{"run vitals clean"}}}}
	out := renderAdvice(PageContext{Report: report})
	if !strings.Contains(out, "disk nearly full") || !strings.Contains(out, "run vitals clean") {
		t.Errorf("renderAdvice should always include the heuristic finding and fix, got: %s", out)
	}
	if !strings.Contains(out, `id="ai-commentary"`) {
		t.Errorf("renderAdvice should include the ai-commentary placeholder, got: %s", out)
	}
	if !strings.Contains(out, "/advice/commentary") {
		t.Errorf("renderAdvice should fetch the commentary AsyncFragment, got: %s", out)
	}
}

func TestRenderAdviceCommentaryShowsTheReply(t *testing.T) {
	withFreshAdviceCache(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
		case "/api/chat":
			w.Write([]byte(`{"message":{"content":"**Restart** the app."}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	status, out := renderAdviceCommentary(PageContext{LLMOpts: llm.CompleteOptions{OllamaURL: srv.URL}})
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if !strings.Contains(out, "<strong>Restart</strong>") {
		t.Errorf("renderAdviceCommentary should render the reply's Markdown, got: %s", out)
	}
	if !strings.Contains(out, "the app.") {
		t.Errorf("renderAdviceCommentary should include the reply's plain text too, got: %s", out)
	}
}

func TestRenderAdviceCommentaryShowsTheErrorInstead(t *testing.T) {
	withFreshAdviceCache(t)
	status, out := renderAdviceCommentary(PageContext{LLMOpts: llm.CompleteOptions{OllamaURL: "http://127.0.0.1:1"}})
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 even when no LLM is reachable", status)
	}
	if !strings.Contains(out, "No LLM reachable") {
		t.Errorf("renderAdviceCommentary should surface the error, got: %s", out)
	}
}

func TestRenderAdviceCommentaryEscapesTheErrorMessage(t *testing.T) {
	// An error's message can embed arbitrary provider/network detail —
	// this was a raw string-concat + manual html.EscapeString call before
	// the html/template migration; confirm it's still escaped now that
	// escaping is the template's job, not a call the author has to
	// remember.
	out := mustExecute(adviceErrorTmpl, "<script>alert(1)</script>")
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("error message was not escaped: %s", out)
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
	for _, want := range []string{"", "cpu", "mem", "disk", "net", "power", "gpu", "advice", "clean"} {
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

func TestOnlyAdviceRegistersAnAsyncFragment(t *testing.T) {
	// Runs against the real registry (populated by every module's own
	// init()) — advice is the only page today with request-scoped work
	// too slow to do inline; a future one would register its own
	// AsyncFragment the same way, not grow this test.
	if _, ok := findAsyncFragment("/advice/commentary"); !ok {
		t.Error("advice should have registered /advice/commentary as an AsyncFragment")
	}
	if len(asyncFragments) != 1 {
		t.Errorf("expected exactly one registered AsyncFragment, got %d: %+v", len(asyncFragments), asyncFragments)
	}
}

// withFreshAdviceCache swaps defaultAdviceCache for a new, empty instance
// for the duration of a test and restores the original after — the same
// swap-and-restore shape withRegistry uses, needed because
// TestRenderAdviceCommentaryShowsTheReply and
// TestRenderAdviceCommentaryShowsTheErrorInstead each expect
// renderAdviceCommentary to actually call the LLM, not serve a result
// cached by whichever of the two ran first in the same test binary.
func withFreshAdviceCache(t *testing.T) {
	t.Helper()
	old := defaultAdviceCache
	defaultAdviceCache = newPrepareAdviceCache()
	t.Cleanup(func() { defaultAdviceCache = old })
}

func TestAdviceModuleIsAlwaysAvailable(t *testing.T) {
	// The heuristic half of the advice page needs no LLM at all (see
	// renderAdvice), so unlike the old provider-gated behavior, the page
	// itself must stay available with no providers reachable at all.
	_, _, available := findModule("advice", PageContext{})
	if !available {
		t.Error("advice module should be available with no providers reachable — its heuristic half needs no LLM")
	}
	_, _, available = findModule("advice", PageContext{Providers: []llm.Provider{{Reachable: true}}})
	if !available {
		t.Error("advice module should also be available with a provider reachable")
	}
}
