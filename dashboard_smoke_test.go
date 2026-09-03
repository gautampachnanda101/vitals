package main

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// dashboardURLRE matches the "serving the dashboard at http://127.0.0.1:PORT/"
// line ServeLocal (internal/guide/serve.go) prints once it's actually
// listening — the only portable way to learn the ephemeral port --addr
// 127.0.0.1:0 was assigned.
var dashboardURLRE = regexp.MustCompile(`serving .* at (http://127\.0\.0\.1:\d+/)`)

// TestDashboardSmoke is vitals' first smoke test for a long-running server
// command — everything else cli_smoke_test.go covers is one-shot. It execs
// the real binary (not an in-process call), because the behavior under
// test — os.Interrupt triggering signal.NotifyContext's graceful shutdown
// in guide.ServeLocal — only exists at the process level; calling
// dashboard.Serve directly from this test process would deliver the
// interrupt to the test binary itself, not to an isolated server.
func TestDashboardSmoke(t *testing.T) {
	bin := buildCLIOnce(t)
	scratch := t.TempDir()

	cmd := exec.Command(bin, "dashboard", "--addr", "127.0.0.1:0", "--no-open")
	cmd.Env = append(os.Environ(), "HOME="+scratch, "APPDATA="+scratch, "XDG_CONFIG_HOME=", "NO_COLOR=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	cmd.Stderr = cmd.Stdout // combine, so a failure's diagnostics show everything either wrote

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting vitals dashboard: %v", err)
	}
	// Always reap the process, however this test exits — a t.Fatal above
	// or an assertion failure below must not leave a listening vitals
	// process behind.
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	url, output := waitForDashboardURL(t, stdout)
	if url == "" {
		t.Fatalf("never saw the dashboard's listen line in its output:\n%s", output)
	}

	assertRoute(t, url, "", http.StatusOK, "vitals")
	// advice is registered but gated on AnyLLMReachable — CI has no
	// Ollama running and no cloud API key env vars set, so this
	// specifically exercises the unavailable-page path (200, not a bare
	// 404) that findModule/route exist to produce instead.
	assertRoute(t, url, "advice", http.StatusOK, "")
	assertRoute(t, url, "nope-does-not-exist", http.StatusNotFound, "")

	assertGracefulShutdown(t, cmd)
}

// waitForDashboardURL reads r line by line until it sees ServeLocal's
// listen line or r closes, whichever comes first, bounded to 10s so a
// startup hang fails this test instead of the whole suite. Returns
// everything read so far either way, for a failing test's diagnostics.
func waitForDashboardURL(t *testing.T, r io.Reader) (url, output string) {
	t.Helper()
	type result struct{ url, output string }
	done := make(chan result, 1)

	go func() {
		var buf strings.Builder
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := sc.Text()
			buf.WriteString(line)
			buf.WriteByte('\n')
			if m := dashboardURLRE.FindStringSubmatch(line); m != nil {
				done <- result{url: m[1], output: buf.String()}
				return
			}
		}
		done <- result{output: buf.String()}
	}()

	select {
	case r := <-done:
		return r.url, r.output
	case <-time.After(10 * time.Second):
		return "", "(timed out waiting for the listen line)"
	}
}

// assertRoute GETs base+path and checks the status code and, when
// wantBodyContains is non-empty, that the body contains it.
func assertRoute(t *testing.T, base, path string, wantStatus int, wantBodyContains string) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Errorf("GET %s%s: %v", base, path, err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Errorf("GET %s%s = %d, want %d:\n%s", base, path, resp.StatusCode, wantStatus, body)
	}
	if wantBodyContains != "" && !strings.Contains(string(body), wantBodyContains) {
		t.Errorf("GET %s%s body missing %q:\n%s", base, path, wantBodyContains, body)
	}
}

// assertGracefulShutdown sends an interrupt and asserts the process exits
// within a bound comfortably longer than ServeLocal's own 2s shutdown
// timeout. os.Interrupt isn't deliverable via Process.Signal on Windows
// (the Go runtime returns an error instead of sending anything real) —
// there this falls back to Kill and only checks that the process actually
// terminates, not that it did so via the graceful-shutdown code path.
func assertGracefulShutdown(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("Kill: %v", err)
		}
	} else if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("sending interrupt: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	select {
	case err := <-waitErr:
		if runtime.GOOS != "windows" && err != nil {
			t.Errorf("vitals dashboard did not exit cleanly after an interrupt: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("vitals dashboard did not exit within 5s of being interrupted")
	}
}
