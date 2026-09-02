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
	return ServeLocal(mux, "the guide")
}

// ServeLocal runs handler on a random loopback port (127.0.0.1 — never on
// the network), opens the user's default browser to it, and blocks until
// interrupted. This is the shared plumbing behind every local web view
// vitals offers — the guide, `vitals dashboard` — so each one only has to
// define its own routes; the safe-bind/auto-open/graceful-shutdown behavior
// is written once and can't drift between them.
func ServeLocal(handler http.Handler, label string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start local server: %w", err)
	}
	url := "http://" + ln.Addr().String() + "/"

	srv := &http.Server{Handler: handler}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		time.Sleep(150 * time.Millisecond) // let Serve start accepting before we open the browser
		if err := openBrowser(url); err != nil {
			ui.Warnf("could not open a browser automatically: %v", err)
		}
	}()

	ui.Okf("serving %s at %s — press Ctrl+C to stop", label, url)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	fmt.Println()
	return nil
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
