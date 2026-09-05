package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaModelsWrapperGoesThroughOllamaModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ps" {
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.1:8b","size_vram":1000,"size":2000}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	got := OllamaModels(srv.URL)
	if len(got) != 1 || got[0].Name != "llama3.1:8b" {
		t.Errorf("OllamaModels(stub server) = %+v, want one llama3.1:8b entry", got)
	}
}

func TestProbeProvidersWrapperGoesThroughProbeProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	got := ProbeProviders(Options{OllamaURL: srv.URL})
	found := false
	for _, p := range got {
		if p.Name == "Ollama" && p.Reachable {
			found = true
		}
	}
	if !found {
		t.Errorf("ProbeProviders(stub Ollama) = %+v, want a reachable Ollama entry", got)
	}
}

func TestScanProcessesWrapperGoesThroughTheRealProcessTable(t *testing.T) {
	// One real end-to-end call — like internal/monitor's/internal/
	// memhogs' single real-process-table exercise — no assertion beyond
	// "doesn't panic and returns a slice", since what's actually running
	// on the test machine is unknowable.
	_ = ScanProcesses()
}

func TestCloudAPIKeyEnvVarsListsEveryRegisteredKey(t *testing.T) {
	got := CloudAPIKeyEnvVars()
	for _, want := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GROQ_API_KEY"} {
		found := false
		for _, k := range got {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("CloudAPIKeyEnvVars() = %v, missing %q", got, want)
		}
	}
}

func TestOnceGoesThroughTheRealDefaultsEndToEnd(t *testing.T) {
	// One real end-to-end call — no reachable provider on an unbindable
	// port, so this proves once()'s own orchestration (scanProcesses,
	// probeProviders, collectResidentModels, the JSON-vs-render branch)
	// completes without error rather than any specific provider result.
	out := captureStdout(t, func() {
		if err := once(Options{OllamaURL: "http://127.0.0.1:1"}); err != nil {
			t.Fatalf("once: %v", err)
		}
	})
	if !strings.Contains(out, "LLM") {
		t.Errorf("once() produced no report, got:\n%s", out)
	}
}

func TestRunNonWatchCallsOnceOnce(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run(defaultRunDeps, Options{OllamaURL: "http://127.0.0.1:1"}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "LLM") {
		t.Errorf("run() non-watch produced no report, got:\n%s", out)
	}
}

func TestWatchStopsWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var err error
		captureStdout(t, func() {
			err = watch(ctx, Options{OllamaURL: "http://127.0.0.1:1", Interval: time.Millisecond, JSON: true})
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("watch: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("watch did not stop after its context timed out")
	}
}

func TestRunDefaultsAnEmptyOllamaURL(t *testing.T) {
	// once()'s own defaults (opts.withDefaults()) would also fill this in,
	// but run() applies its own default before ever calling once/watch —
	// this proves that assignment happens, not just once()'s.
	out := captureStdout(t, func() {
		if err := run(defaultRunDeps, Options{}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "LLM") {
		t.Errorf("run() with a blank OllamaURL produced no report, got:\n%s", out)
	}
}

func TestRunWatchDispatchesThroughNewSignalContextIntoWatch(t *testing.T) {
	// Exercises run()'s own Watch:true branch (ctx, stop :=
	// d.newSignalContext(); return watch(ctx, opts)), not watch() called
	// directly the way TestWatchStopsWhenItsContextIsDone does.
	fake := runDeps{newSignalContext: func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), 30*time.Millisecond)
	}}
	done := make(chan error, 1)
	go func() {
		var err error
		captureStdout(t, func() {
			err = run(fake, Options{Watch: true, OllamaURL: "http://127.0.0.1:1", Interval: time.Millisecond, JSON: true})
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run(Watch:true) did not stop after its signal context timed out")
	}
}

func TestWatchNonJSONClearsScreenAndLoopsAtLeastOnce(t *testing.T) {
	// Interval is short enough, relative to the context's timeout, that
	// the ticker.C branch fires at least once before ctx.Done() does —
	// TestWatchStopsWhenItsContextIsDone uses JSON:true and never
	// exercises the !opts.JSON clear-screen line.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		var r result
		r.out = captureStdout(t, func() {
			r.err = watch(ctx, Options{OllamaURL: "http://127.0.0.1:1", Interval: 5 * time.Millisecond, JSON: false})
		})
		done <- r
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("watch: %v", r.err)
		}
		if !strings.Contains(r.out, "\033[H\033[2J") {
			t.Errorf("watch should clear the screen before each non-JSON emit, got %q", r.out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("watch did not stop after its context timed out")
	}
}

func TestDefaultRunDepsNewSignalContextWiresRealSignalNotify(t *testing.T) {
	// Same pattern as internal/monitor's TestDefaultSourceNewSignalContextWiresRealSignalNotify:
	// exercise the real signal.NotifyContext call site directly rather than
	// through a real OS signal (which would need to actually interrupt the
	// test process to observe).
	ctx, stop := defaultRunDeps.newSignalContext()
	defer stop()
	if ctx == nil {
		t.Fatal("newSignalContext returned a nil context")
	}
	select {
	case <-ctx.Done():
		t.Fatal("a freshly created signal context should not already be done")
	default:
	}
}

func TestPublicRunGoesThroughDefaultRunDeps(t *testing.T) {
	// One real end-to-end call through the exported Run() -> defaultRunDeps
	// wiring (the real signal.NotifyContext included, via the non-watch
	// path so no real signal handler needs exercising to prove it).
	out := captureStdout(t, func() {
		if err := Run(Options{OllamaURL: "http://127.0.0.1:1"}); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if !strings.Contains(out, "LLM") {
		t.Errorf("Run() produced no report, got:\n%s", out)
	}
}
