package metrics

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"vitals/internal/doctor"
	"vitals/internal/ui"
)

// Options configures the exporter.
type Options struct {
	OllamaURL string
	Addr      string // ":9100" style; empty for a one-shot stdout dump
}

// collect takes a fresh snapshot and renders it.
func collect(ollamaURL string) string {
	snap := doctor.Collect(doctor.Options{OllamaURL: ollamaURL})
	rep := doctor.Analyze(snap)
	return Render(snap, rep)
}

// deps is the live surface RunOnce/Serve read from, pulled out so a test
// can substitute fakes — same shape as internal/tools'/internal/dupes'
// deps structs. defaultDeps wires the real calls; production code
// always goes through it via RunOnce/Serve.
type deps struct {
	collect          func(ollamaURL string) string
	newSignalContext func() (context.Context, context.CancelFunc)
}

var defaultDeps = deps{
	collect: collect,
	newSignalContext: func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt)
	},
}

// RunOnce prints one scrape to stdout and exits.
func RunOnce(opts Options) error { return runOnce(defaultDeps, opts) }

func runOnce(d deps, opts Options) error {
	fmt.Print(d.collect(opts.OllamaURL))
	return nil
}

// resolveAddr fills in the default listen address. The default is
// loopback-only (127.0.0.1), not a bare ":PORT" — vitals ships as a
// single binary anyone can run with no setup, and "just run vitals serve"
// shouldn't silently open a port reachable from the network. An operator
// who wants the wider, node_exporter-style all-interfaces bind still gets
// it by passing --addr explicitly (":9100" or "0.0.0.0:9100").
func resolveAddr(addr string) string {
	if addr == "" {
		return "127.0.0.1:9100"
	}
	return addr
}

// newMux builds the exporter's two routes — split out from Serve so it's
// testable via httptest.NewServer without ever binding a real port or
// depending on resolveAddr's fixed default.
func newMux(d deps, ollamaURL string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprint(w, d.collect(ollamaURL))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "vitals metrics exporter — scrape /metrics\n")
	})
	return mux
}

// Serve runs an HTTP server exposing /metrics until interrupted.
func Serve(opts Options) error { return serve(defaultDeps, opts) }

func serve(d deps, opts Options) error {
	addr := resolveAddr(opts.Addr)
	srv := &http.Server{Addr: addr, Handler: newMux(d, opts.OllamaURL), ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := d.newSignalContext()
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	ui.Okf("serving Prometheus metrics on http://localhost%s/metrics", addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		fmt.Println()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
