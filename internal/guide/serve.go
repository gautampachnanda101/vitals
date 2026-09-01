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
	page := RenderHTML(md, title)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start local server: %w", err)
	}
	url := "http://" + ln.Addr().String() + "/"

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})
	srv := &http.Server{Handler: mux}

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

	ui.Okf("serving the guide at %s — press Ctrl+C to stop", url)
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
