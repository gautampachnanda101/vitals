package main

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// cleanApplyConfirmBody is a valid, well-formed confirm body — used only
// in a case that must still be rejected (cross-origin), never in a case
// that would let the real handler call clean.Apply and actually delete
// something on the machine running this test. See the /clean/apply
// assertions inside TestDashboardSmoke below for why a real same-origin
// /clean/apply call is deliberately never exercised here, mirroring
// cli_smoke_test.go's own exclusion of `clean` without
// --dry-run.
const cleanApplyConfirmBody = `{"confirm": true}`

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
	writeDupesFixture(t, scratch)

	cmd := exec.Command(bin, "dashboard", "--addr", "127.0.0.1:0", "--no-open")
	// USERPROFILE, not HOME, is what os.UserHomeDir() actually reads on
	// Windows (Go's own documented behavior) — the dupes "home" scope
	// (internal/dashboard/modules_dupes.go's resolveScopeOnThisHost) is
	// the first thing in this dashboard to call os.UserHomeDir() from a
	// live subprocess reachable over HTTP, so this gap was real but
	// latent until then: HOME alone isolates nothing on that OS, and a
	// /dupes/preview or /dupes/hardlink call below would silently scan
	// this runner's real profile directory instead of scratch.
	cmd.Env = append(os.Environ(), "HOME="+scratch, "USERPROFILE="+scratch, "APPDATA="+scratch, "XDG_CONFIG_HOME=", "NO_COLOR=1")

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
	// advice is always available (its heuristic half needs no LLM at
	// all), so assert on that heuristic content — present whether or not
	// this machine happens to have a real local LLM answering, unlike
	// asserting on the LLM-unreachable note specifically.
	assertRoute(t, url, "advice", http.StatusOK, "rule-based checks found")
	// advice's LLM commentary is a separate AsyncFragment (asked by the
	// page's own client-side JS, not blocked on by the page itself) — the
	// real regression this guards is Serve's own handler wiring the async
	// route through route() at all against a live server, not just a
	// route() unit test. ai-commentary is present whether the LLM
	// answered or not (a friendly "unavailable" message either way).
	assertRoute(t, url, "advice/commentary", http.StatusOK, "ai-commentary")
	assertRoute(t, url, "nope-does-not-exist", http.StatusNotFound, "")
	assertRoute(t, url, "clean", http.StatusOK, "clean-preview-btn")
	// Exercises the actual write-action HTTP path end to end, not just
	// routeWrite in isolation: a real bug shipped where Serve's handler
	// never dispatched POST to routeWrite at all (every POST silently
	// fell through to the GET route and 404'd), invisible to any test
	// that only called routeWrite directly.
	assertPostRoute(t, url, "clean/preview", http.StatusOK, "Would reclaim")
	// /clean/apply is deliberately never exercised here with a body that
	// would let it actually run clean.Apply — see
	// cleanApplyConfirmBody's own comment and design.md §7's testing
	// section, mirroring cli_smoke_test.go's exclusion of `clean` without
	// --dry-run. What IS safe, and is exercised below, never reaches
	// clean.Apply at all: confirm-body validation (rejected before the
	// single-flight guard) and the cross-origin same-origin check
	// (rejected before the handler runs).
	assertPostRouteWithBody(t, url, "clean/apply", "", http.StatusBadRequest, "confirm")
	assertPostRouteWithBody(t, url, "clean/apply", `{"confirm": false}`, http.StatusBadRequest, "")
	assertCrossOriginPostRejected(t, url, "clean/apply", cleanApplyConfirmBody)

	assertRoute(t, url, "dupes", http.StatusOK, "dupes-preview-btn")
	// A real preview against the fixture pair writeDupesFixture wrote into
	// scratch (this process's HOME) above — proves the write-action HTTP
	// path for /dupes/preview end to end, same reasoning as /clean/preview
	// above. /dupes/hardlink is deliberately never exercised here with a
	// body that would let it actually run — see dupesHardlinkConfirmBody's
	// own comment, mirroring cleanApplyConfirmBody's.
	assertPostRouteWithBody(t, url, "dupes/preview", `{"scope":"home"}`, http.StatusOK, "reclaimable")
	assertPostRouteWithBody(t, url, "dupes/hardlink", "", http.StatusBadRequest, "confirm")
	assertPostRouteWithBody(t, url, "dupes/hardlink", `{"confirm": false, "scope":"home"}`, http.StatusBadRequest, "")
	assertCrossOriginPostRejected(t, url, "dupes/hardlink", dupesHardlinkConfirmBody)

	assertHistoryWasRecorded(t, scratch)

	assertGracefulShutdown(t, cmd)

	assertDupesFixtureUntouched(t, scratch)
}

// dupesHardlinkConfirmBody is a valid, well-formed confirm+scope body —
// used only in a case that must still be rejected (cross-origin), never
// in a case that would let the real handler call ApplyHardlinks and
// actually hardlink something on the machine running this test. Mirrors
// cleanApplyConfirmBody's own reasoning.
const dupesHardlinkConfirmBody = `{"confirm": true, "scope": "home"}`

// dupesFixtureMinSize matches the dashboard's own dupes.go "home" scope
// minSize (1 MiB) — a smaller pair would be silently filtered out before
// ever becoming a scan candidate.
const dupesFixtureMinSize = 1 << 20

// writeDupesFixture writes a real byte-identical pair under home so
// /dupes/preview above has something genuine to find, not an empty scan.
func writeDupesFixture(t *testing.T, home string) {
	t.Helper()
	content := make([]byte, dupesFixtureMinSize)
	for i := range content {
		content[i] = byte(i)
	}
	for _, name := range []string{"vitals-smoke-a.bin", "vitals-smoke-b.bin"} {
		if err := os.WriteFile(filepath.Join(home, name), content, 0o644); err != nil {
			t.Fatalf("writing dupes fixture %s: %v", name, err)
		}
	}
}

// assertDupesFixtureUntouched confirms the fixture pair writeDupesFixture
// wrote is still two separate files, not hardlinked together — this test
// never sends a real confirmed /dupes/hardlink call (see
// dupesHardlinkConfirmBody's own comment), so nothing should have
// changed on disk.
func assertDupesFixtureUntouched(t *testing.T, home string) {
	t.Helper()
	a, err := os.Stat(filepath.Join(home, "vitals-smoke-a.bin"))
	if err != nil {
		t.Fatalf("stat fixture a: %v", err)
	}
	b, err := os.Stat(filepath.Join(home, "vitals-smoke-b.bin"))
	if err != nil {
		t.Fatalf("stat fixture b: %v", err)
	}
	if os.SameFile(a, b) {
		t.Error("the dupes fixture pair should still be two separate files — no real /dupes/hardlink call was ever sent")
	}
}

// assertHistoryWasRecorded confirms the GET above actually wrote a point to
// doctor's trend history file — the bug this guards against: the dashboard
// used to call doctor.Collect+Analyze directly, bypassing doctor.Assess's
// recordHistory entirely, so a dashboard-only user silently accumulated no
// history at all (internal/dashboard/snapshot_cache.go now calls Assess
// specifically to fix this). Resolves the same config-dir path the
// subprocess resolved by temporarily setting the identical env vars in
// this process and calling os.UserConfigDir() — t.Setenv auto-restores
// them after the test.
func assertHistoryWasRecorded(t *testing.T, scratch string) {
	t.Helper()
	t.Setenv("HOME", scratch)
	t.Setenv("APPDATA", scratch)
	t.Setenv("XDG_CONFIG_HOME", "")

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir(): %v", err)
	}
	historyPath := filepath.Join(configDir, "vitals", "history.jsonl")

	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Errorf("expected %s to exist after hitting the dashboard, got: %v", historyPath, err)
		return
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Errorf("%s exists but is empty — no history point was recorded", historyPath)
	}
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

// assertPostRoute mirrors assertRoute for a write action — no Origin
// header set, matching a same-machine caller (e.g. curl), which
// guide.ServeLocal's sameOriginOnly middleware allows through.
func assertPostRoute(t *testing.T, base, path string, wantStatus int, wantBodyContains string) {
	t.Helper()
	resp, err := http.Post(base+path, "application/json", nil)
	if err != nil {
		t.Errorf("POST %s%s: %v", base, path, err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Errorf("POST %s%s = %d, want %d:\n%s", base, path, resp.StatusCode, wantStatus, body)
	}
	if wantBodyContains != "" && !strings.Contains(string(body), wantBodyContains) {
		t.Errorf("POST %s%s body missing %q:\n%s", base, path, wantBodyContains, body)
	}
}

// assertPostRouteWithBody mirrors assertPostRoute but sends body as the
// request payload — used for /clean/apply's confirm-body validation
// cases, where the point of the assertion is that a bad body is rejected
// with 400 before clean.Apply ever runs. Like assertPostRoute, no Origin
// header is set, matching a same-machine caller.
func assertPostRouteWithBody(t *testing.T, base, path, body string, wantStatus int, wantBodyContains string) {
	t.Helper()
	resp, err := http.Post(base+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Errorf("POST %s%s: %v", base, path, err)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Errorf("POST %s%s (body %q) = %d, want %d:\n%s", base, path, body, resp.StatusCode, wantStatus, respBody)
	}
	if wantBodyContains != "" && !strings.Contains(string(respBody), wantBodyContains) {
		t.Errorf("POST %s%s body missing %q:\n%s", base, path, wantBodyContains, respBody)
	}
}

// assertCrossOriginPostRejected sends a POST carrying an Origin header
// for a page this server never served, and asserts guide.ServeLocal's
// sameOriginOnly middleware rejects it with 403 before the request ever
// reaches the write action's own handler — the actual regression
// design.md's whole same-origin model exists to prevent. body is a
// deliberately valid confirm body: proving the *cross-origin* check
// alone is what blocks this call (not a 400 from body validation) is the
// point, and the request is still safe because sameOriginOnly runs
// before routeWrite/handleCleanApply ever sees it.
func assertCrossOriginPostRejected(t *testing.T, base, path, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Errorf("POST %s%s (cross-origin): %v", base, path, err)
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin POST %s%s = %d, want 403:\n%s", base, path, resp.StatusCode, respBody)
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
