package metrics

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolveAddrDefaultsToLoopbackOnly(t *testing.T) {
	// An empty Addr must never resolve to an all-interfaces bind — a metrics
	// server that ships as a "just run it" static binary shouldn't open a
	// port beyond the local machine unless the operator explicitly asks.
	got := resolveAddr("")
	if got != "127.0.0.1:9100" {
		t.Errorf("resolveAddr(\"\") = %q, want a loopback-only default", got)
	}
}

func TestResolveAddrHonorsExplicitOverride(t *testing.T) {
	// A bare ":PORT" is exactly how you'd ask Prometheus-style tools to bind
	// every interface — that has to stay reachable for anyone who wants it,
	// just never be the default.
	for _, addr := range []string{":9100", "0.0.0.0:9100", "127.0.0.1:9200", "10.0.0.5:9100"} {
		if got := resolveAddr(addr); got != addr {
			t.Errorf("resolveAddr(%q) = %q, want it passed through unchanged", addr, got)
		}
	}
}

func fakeCollect(want string) func(string) string {
	return func(ollamaURL string) string {
		if ollamaURL != want {
			return "wrong ollamaURL: " + ollamaURL
		}
		return "fake_metric 1\n"
	}
}

func TestRunOncePrintsTheCollectedScrape(t *testing.T) {
	d := deps{collect: fakeCollect("http://x")}
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	if err := runOnce(d, Options{OllamaURL: "http://x"}); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	w.Close()
	os.Stdout = old
	if out := <-done; !strings.Contains(out, "fake_metric 1") {
		t.Errorf("runOnce should print the collected scrape, got:\n%s", out)
	}
}

func TestNewMuxServesMetricsAndRoot(t *testing.T) {
	d := deps{collect: fakeCollect("http://ollama")}
	srv := httptest.NewServer(newMux(d, "http://ollama"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "fake_metric 1") {
		t.Errorf("/metrics body = %q, want the collected scrape", body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain (Prometheus exposition format)", ct)
	}

	resp2, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp2.Body.Close()
	body2, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body2), "scrape /metrics") {
		t.Errorf("/ body = %q, want a pointer at /metrics", body2)
	}
}

func TestServeShutsDownCleanlyWhenItsSignalContextIsDone(t *testing.T) {
	d := deps{
		collect: fakeCollect(""),
		newSignalContext: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // already done — serve should shut down almost immediately
			return ctx, cancel
		},
	}
	done := make(chan error, 1)
	go func() { done <- serve(d, Options{Addr: "127.0.0.1:0"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not shut down after its signal context was already done")
	}
}

func TestCollectAndRunOnceGoThroughTheRealDoctorCollect(t *testing.T) {
	// One real end-to-end call through the actual doctor.Collect/Analyze/
	// Render wiring — the memcheck/monitor-style exercise of collect()
	// and the public RunOnce() wrapper that defaultDeps' fake never
	// touches. Serve() itself (a real blocking HTTP server with no clean
	// way to interrupt it from inside a test) stays the one documented
	// live exception, same class as internal/guide's ServeLocal.
	out := collect("")
	if !strings.Contains(out, "system_cpu_utilization") {
		t.Errorf("collect(\"\") should return real Prometheus output, got:\n%s", out)
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	if err := RunOnce(Options{}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	w.Close()
	os.Stdout = old
	if out := <-done; !strings.Contains(out, "system_cpu_utilization") {
		t.Errorf("RunOnce() should print real Prometheus output, got:\n%s", out)
	}
}

func TestServeReturnsListenAndServeErrorForAnUnbindableAddr(t *testing.T) {
	d := deps{
		collect: fakeCollect(""),
		newSignalContext: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background()) // never cancelled — the bind error must win the race
		},
	}
	// Port 0 is only magic to net.Listen; an address ollama-style with no
	// such interface fails to bind for real.
	err := serve(d, Options{Addr: "256.256.256.256:9100"})
	if err == nil {
		t.Error("serve should return the real ListenAndServe error for an unbindable address")
	}
}
