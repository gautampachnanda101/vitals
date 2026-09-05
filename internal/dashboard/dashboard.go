package dashboard

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"vitals/internal/guide"
	"vitals/internal/llm"
)

// Options configures `vitals dashboard`.
type Options struct {
	Addr      string // host:port or :port — only the port is honored, see loopbackAddr. Empty picks a random port.
	NoOpen    bool
	OllamaURL string
	Version   string // main.version, shown in the page footer
}

// maxWriteActionBody caps how much of a POST body Serve reads before
// handing it to routeWrite — a write action's own body is a small JSON
// confirmation, never a file upload, so there's no reason to let a
// caller hand this an unbounded read.
const maxWriteActionBody = 1 << 20 // 1 MiB

// Serve starts `vitals dashboard`: a shared, TTL-cached snapshot (see
// snapshot_cache.go) feeds every request's PageContext, routed by URL path
// to a registered Module via route — kept separate from this HTTP glue so
// the exists/available/404 branching is unit-tested without starting a
// server. Loopback-only binding, auto-open, and graceful shutdown on
// os.Interrupt are all guide.ServeLocal's plumbing, the same code
// `vitals guide --web` runs on.
func Serve(opts Options) error {
	addr, err := loopbackAddr(opts.Addr)
	if err != nil {
		return err
	}

	cache := newSnapshotCache(opts.OllamaURL)
	llmOpts := llm.CompleteOptions{OllamaURL: opts.OllamaURL}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := cache.Get()
		ctx := PageContext{
			Snapshot:  snap.Snapshot,
			Report:    snap.Report,
			Providers: snap.Providers,
			LLMOpts:   llmOpts,
			History:   snap.History,
			Version:   opts.Version,
		}

		var status int
		var body string
		if r.Method == http.MethodPost {
			// guide.ServeLocal's sameOriginOnly middleware has already
			// rejected any cross-origin POST before this handler ever
			// runs — see internal/guide/serve.go.
			b, _ := io.ReadAll(io.LimitReader(r.Body, maxWriteActionBody))
			status, body = routeWrite(r.URL.Path, b, ctx)
		} else {
			status, body = route(r.URL.Path, ctx)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})

	return guide.ServeLocal(handler, "the dashboard", guide.ServeOptions{Addr: addr, NoOpen: opts.NoOpen})
}

// loopbackAddr forces addr onto the loopback interface regardless of what
// host the caller specified — vitals dashboard promises "nothing leaves
// this machine" unconditionally (see docs/architecture/design.md), so
// --addr picks a port, never a bind interface. Empty addr keeps
// ServeLocal's ephemeral-port default.
func loopbackAddr(addr string) (string, error) {
	if addr == "" {
		return "", nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid --addr %q: %w", addr, err)
	}
	return "127.0.0.1:" + port, nil
}

// route computes the HTTP status and full page HTML for path against ctx.
// Kept pure (no *http.Request/ResponseWriter) so the exists/available/404
// branching is tested directly. An AsyncFragment path (e.g.
// "/advice/commentary") is dispatched before module lookup — it renders a
// bare HTML fragment, not a full page.Layout, so a module's own
// client-side JS can fetch() and inject it after the page itself has
// already rendered.
func route(path string, ctx PageContext) (int, string) {
	if a, ok := findAsyncFragment(path); ok {
		return a.Handler(ctx)
	}

	nav := availableModules(ctx)
	slug := strings.TrimPrefix(path, "/")

	m, exists, available := findModule(slug, ctx)
	if !exists {
		body := `<div class="card"><p class="unavailable">Page not found. Use the nav above to find what this machine can show.</p></div>`
		return http.StatusNotFound, layout("Not found", "", ctx.Version, nav, body)
	}
	if !available {
		reason := m.UnavailableReason
		if reason == "" {
			reason = "not available on this machine"
		}
		return http.StatusOK, layout(m.NavLabel, m.Slug, ctx.Version, nav, unavailablePage(m.NavLabel, reason))
	}

	return http.StatusOK, layout(m.NavLabel, m.Slug, ctx.Version, nav, m.Render(ctx))
}

// routeWrite computes the HTTP status and response body for a POST to
// path against ctx — the write-side counterpart to route, kept just as
// pure (raw bytes in, no *http.Request) so it's tested the same way.
// Same-origin protection is not this function's concern: guide.ServeLocal
// already rejects any non-GET/HEAD request that fails the Origin/
// Sec-Fetch-Site check before it ever reaches here.
func routeWrite(path string, body []byte, ctx PageContext) (int, string) {
	a, exists, available := findWriteAction(path, ctx)
	if !exists || !available {
		return http.StatusNotFound, `{"error":"not found"}`
	}
	return a.Handler(ctx, body)
}
