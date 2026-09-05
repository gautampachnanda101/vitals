package dashboard

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vitals/internal/monitor"
)

func TestProcessesModuleIsRegistered(t *testing.T) {
	m, exists, available := findModule("processes", PageContext{})
	if !exists {
		t.Fatal("processes module should be registered")
	}
	if !available {
		t.Error("processes module should always be available")
	}
	if m.NavLabel != "Processes" || m.Group != "Resources" {
		t.Errorf("NavLabel/Group = %q/%q, want \"Processes\"/\"Resources\"", m.NavLabel, m.Group)
	}
}

// --- processCache: same shape/tests as prepareAdviceCache's own suite --

func TestProcessCacheRefreshesOnceThenServesFromCacheWithinTTL(t *testing.T) {
	var calls int32
	c := &processCache{
		ttl: time.Hour,
		sample: func() (monitor.Snapshot, error) {
			atomic.AddInt32(&calls, 1)
			return monitor.Snapshot{}, nil
		},
	}
	c.Get()
	c.Get()
	c.Get()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("sample called %d times, want 1 (cached within TTL)", got)
	}
}

func TestProcessCacheRefreshesAgainAfterTTLExpires(t *testing.T) {
	var calls int32
	c := &processCache{
		ttl: 10 * time.Millisecond,
		sample: func() (monitor.Snapshot, error) {
			atomic.AddInt32(&calls, 1)
			return monitor.Snapshot{}, nil
		},
	}
	c.Get()
	time.Sleep(20 * time.Millisecond)
	c.Get()
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("sample called %d times, want 2 (once per TTL window)", got)
	}
}

func TestProcessCacheSingleFlightsConcurrentRefreshes(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	c := &processCache{
		ttl: time.Hour,
		sample: func() (monitor.Snapshot, error) {
			atomic.AddInt32(&calls, 1)
			<-release
			return monitor.Snapshot{}, nil
		},
	}
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() { defer wg.Done(); c.Get() }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("sample called %d times for %d concurrent callers, want 1", got, n)
	}
}

func TestProcessCacheReturnsTheCachedValueAndError(t *testing.T) {
	wantErr := errors.New("boom")
	c := &processCache{
		ttl: time.Hour,
		sample: func() (monitor.Snapshot, error) {
			return monitor.Snapshot{Processes: []monitor.ProcInfo{{Name: "x"}}}, wantErr
		},
	}
	snap, err := c.Get()
	if err != wantErr {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if len(snap.Processes) != 1 {
		t.Errorf("snap = %+v, want the cached partial value alongside the error", snap)
	}
}

// --- renderProcesses ---------------------------------------------------

func withFakeProcessCache(t *testing.T, fn func() (monitor.Snapshot, error)) {
	t.Helper()
	old := defaultProcessCache
	defaultProcessCache = &processCache{ttl: time.Hour, sample: fn}
	t.Cleanup(func() { defaultProcessCache = old })
}

func TestRenderProcessesShowsEachProcess(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) {
		return monitor.Snapshot{Processes: []monitor.ProcInfo{
			{PID: 4821, Name: "Copilot", CPUPct: 100, RSSBytes: 1 << 30},
			{PID: 5190, Name: "llama-server", CPUPct: 62, RSSBytes: 512 << 20},
		}}, nil
	})
	out := renderProcesses(PageContext{})
	for _, want := range []string{"Copilot", "4821", "100%", "llama-server", "5190", "62%"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderProcesses missing %q, got: %s", want, out)
		}
	}
}

func TestRenderProcessesEmptyIsFriendly(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	out := renderProcesses(PageContext{})
	if !strings.Contains(out, "No processes to show") {
		t.Errorf("renderProcesses(empty) = %s", out)
	}
}

func TestRenderProcessesShowsTheErrorWhenSamplingFails(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, errors.New("permission denied") })
	out := renderProcesses(PageContext{})
	if !strings.Contains(out, "permission denied") {
		t.Errorf("renderProcesses should surface the sampling error, got: %s", out)
	}
}

func TestRenderProcessesEscapesUntrustedProcessNames(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) {
		return monitor.Snapshot{Processes: []monitor.ProcInfo{{Name: "<script>alert(1)</script>"}}}, nil
	})
	out := renderProcesses(PageContext{})
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("renderProcesses did not escape a crafted process name, got: %s", out)
	}
}

func TestProcessCacheDefaultSamplesTheRealMonitorPackage(t *testing.T) {
	// One real end-to-end call through newProcessCache's own wiring —
	// same style as this package's other "exercise the real call once"
	// tests (e.g. TestHandleCleanPreviewReturns200WithARenderedBody).
	c := newProcessCache()
	snap, err := c.Get()
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}
	if len(snap.Processes) == 0 {
		t.Error("expected at least one process from the real machine")
	}
}
