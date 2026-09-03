package guide

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"

	"vitals/internal/ui"
)

// Serve renders md as HTML and serves it on a local, loopback-only port
// (127.0.0.1 — never on the network), opens the user's default browser to
// it, and blocks until interrupted. It never binds to 0.0.0.0: the guide
// has nothing sensitive in it, but a local-only doc server has no business
// being reachable from anywhere else either.
func Serve(md, title string) error {
	return ServeHTML(RenderHTML(md, title))
}

// ServeHTML serves an already-rendered, single HTML page the same way Serve
// does — loopback-only, browser opened automatically, blocks until
// interrupted.
func ServeHTML(page string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	return ServeLocal(mux, "the guide", ServeOptions{})
}

// ServeOptions configures ServeLocal's listen address and browser-opening
// behavior. The zero value is ServeLocal's original behavior: an
// ephemeral loopback port with the browser opened automatically.
type ServeOptions struct {
	Addr   string // "" = ephemeral 127.0.0.1:0, chosen by the OS
	NoOpen bool   // true = never call openBrowser; the URL is still printed
}

// ServeLocal runs handler on a loopback port (127.0.0.1 — never on the
// network; opts.Addr, if set, must already be a loopback address — see
// dashboard.loopbackAddr for the caller-side enforcement of that), opens
// the user's default browser to it unless opts.NoOpen, and blocks until
// interrupted. This is the shared plumbing behind every local web view
// vitals offers — the guide, `vitals dashboard` — so each one only has to
// define its own routes; the safe-bind/auto-open/graceful-shutdown behavior
// is written once and can't drift between them.
func ServeLocal(handler http.Handler, label string, opts ServeOptions) error {
	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("start local server: %w", err)
	}
	addr = ln.Addr().String()
	url := "http://" + addr + "/"

	// Binding to 127.0.0.1 alone does not stop DNS rebinding: an external
	// page can rebind its own origin's DNS to 127.0.0.1 with a short TTL,
	// then fetch() this server from what the browser now considers the
	// same origin. The request still arrives with the attacker's
	// original hostname in the Host header, not 127.0.0.1 — net/http
	// does not validate Host by default, so checking it against the
	// actual bound address is what actually defeats this, not the bind
	// address alone.
	_, port, _ := net.SplitHostPort(addr)
	handler = allowedHostsOnly(handler, addr, "localhost:"+port)

	// ReadHeaderTimeout guards against a client that opens a connection
	// and trickles headers in slowly (a slowloris-style hang) — the same
	// defense internal/metrics.Serve already sets; this server had none.
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if !opts.NoOpen {
		go func() {
			time.Sleep(150 * time.Millisecond) // let Serve start accepting before we open the browser
			if err := openBrowser(url); err != nil {
				ui.Warnf("could not open a browser automatically: %v", err)
			}
		}()
	}

	ui.Okf("serving %s at %s — press Ctrl+C to stop", label, url)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	fmt.Println()
	return nil
}

// allowedHostsOnly rejects any request whose Host header isn't one of
// allowed with 400, before it reaches next. See the comment in ServeLocal
// for why binding to loopback alone isn't sufficient.
func allowedHostsOnly(next http.Handler, allowed ...string) http.Handler {
	ok := make(map[string]bool, len(allowed))
	for _, h := range allowed {
		ok[h] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok[r.Host] {
			http.Error(w, "invalid host", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// openBrowser hands url to the OS's own "open this" command — never a
// vitals-bundled browser or renderer, consistent with reaching for
// established tools instead of reimplementing them.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
