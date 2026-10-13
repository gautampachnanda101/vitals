package dashboard

import (
	"context"
	"crypto/tls"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vitals/internal/diskusage"
	"vitals/internal/doctor"
)

func withFakeDiskUsageCache(t *testing.T, fn func(context.Context, string, diskusage.Options) (diskusage.Result, error)) {
	t.Helper()
	old := defaultDiskUsageCache
	c := newDiskUsageCache()
	c.drainGrace = 10 * time.Millisecond
	c.scan = fn
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	c.home = func() (string, error) { return home, nil }
	defaultDiskUsageCache = c
	t.Cleanup(func() { defaultDiskUsageCache = old })
}

func diskRequest(query string) PageContext {
	return PageContext{Request: httptest.NewRequest(http.MethodGet, "http://localhost:1234/disk/usage"+query, nil)}
}

func TestDiskInitialPageDoesNotScan(t *testing.T) {
	withFakeDiskUsageCache(t, func(context.Context, string, diskusage.Options) (diskusage.Result, error) {
		t.Fatal("initial page must not scan")
		return diskusage.Result{}, nil
	})
	out := biggestPathsSection()
	for _, want := range []string{`id="disk-usage"`, "Loading", "/disk/usage", "URLSearchParams", "AbortController"} {
		if !strings.Contains(out, want) {
			t.Errorf("placeholder missing %q", want)
		}
	}
	if noLiveRefresh["disk"] != true {
		t.Fatal("disk drill-down state must not be replaced by auto-refresh")
	}
}

func TestDiskHTTPFragmentDoesNotCollectSnapshot(t *testing.T) {
	withFakeDiskUsageCache(t, func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		return diskusage.Result{Root: root, Options: opts, Complete: true}, nil
	})
	snapshots := 0
	cache := &snapshotCache{ttl: time.Hour, refresh: func() cachedSnapshot {
		snapshots++
		return cachedSnapshot{}
	}}
	handler := dashboardHandler(Options{Version: "test"}, cache)
	for _, kind := range []string{"same-origin", "foreign", "head"} {
		request := diskRequest("?top=1&budget=1m").Request
		expected := http.StatusOK
		if kind == "foreign" {
			request.Header.Set("Origin", "https://foreign.example")
			expected = http.StatusForbidden
		} else if kind == "head" {
			request.Method = http.MethodHead
			expected = http.StatusMethodNotAllowed
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != expected || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(kind, response.Code, response.Body.String())
		}
	}
	if snapshots != 0 {
		t.Fatal("disk fragments must bypass slow system/provider collection", snapshots)
	}
	started, release := make(chan struct{}), make(chan struct{})
	defaultDiskUsageCache.scan = func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		close(started)
		<-release
		return diskusage.Result{Root: root}, nil
	}
	request := diskRequest("?budget=1ms").Request
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	<-started
	defaultDiskUsageCache.mu.Lock()
	done := defaultDiskUsageCache.active.done
	defaultDiskUsageCache.mu.Unlock()
	close(release)
	<-done
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" || !strings.Contains(response.Body.String(), "Retry") {
		t.Fatal("busy HTTP state must expose actionable retry", response.Code, response.Body.String())
	}
}

func TestDiskHandlerPreservesOtherAsyncBehaviorAndQueryContext(t *testing.T) {
	withAsyncRegistry(t, []AsyncFragment{{
		Path: "/advice/commentary", Handler: func(ctx PageContext) (int, string) {
			if ctx.Request == nil || ctx.Request.URL.Query().Get("example") != "yes" || ctx.Version != "test" {
				t.Error("ordinary fragments lost request context", ctx)
			}
			return http.StatusAccepted, "ADVICE"
		},
	}})
	cache := &snapshotCache{ttl: time.Hour, refresh: func() cachedSnapshot { return cachedSnapshot{} }}
	handler := dashboardHandler(Options{Version: "test"}, cache)
	r := httptest.NewRequest(http.MethodGet, "http://localhost:1234/advice/commentary?example=yes", nil)
	r.Header.Set("Origin", "http://foreign.example")
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	if out.Code != http.StatusAccepted || out.Body.String() != "ADVICE" {
		t.Fatal(out.Code, out.Body.String())
	}
	withWriteRegistry(t, []WriteAction{{
		Path: "/test/write", Available: Always, Handler: func(ctx PageContext, body []byte) (int, string) {
			return http.StatusOK, string(body)
		},
	}})
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "http://localhost:1234/test/write", strings.NewReader("body")))
	if out.Code != http.StatusOK || out.Body.String() != "body" {
		t.Fatal("POST routing changed", out.Code, out.Body.String())
	}
}

func TestDiskFragmentConfinementAndForeignRequests(t *testing.T) {
	var calls atomic.Int32
	withFakeDiskUsageCache(t, func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		calls.Add(1)
		return diskusage.Result{Root: root, Options: opts, Complete: true}, nil
	})
	home, _ := defaultDiskUsageCache.home()
	if err := os.WriteFile(filepath.Join(home, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	symlinkErr := os.Symlink(outside, filepath.Join(home, "escape"))
	for _, root := range []string{"../outside", outside, "file", "missing"} {
		status, body := renderDiskUsage(diskRequest("?root=" + url.QueryEscape(root)))
		if status != http.StatusBadRequest || !strings.Contains(body, "real directory") {
			t.Errorf("root %q: %d %s", root, status, body)
		}
	}
	if symlinkErr == nil {
		for _, root := range []string{"escape", "escape/child"} {
			status, _ := renderDiskUsage(diskRequest("?root=" + root))
			if status != http.StatusBadRequest {
				t.Errorf("symlink root %q accepted: %d", root, status)
			}
		}
	}
	for _, header := range []struct{ key, value string }{
		{"Origin", "http://evil.example"}, {"Origin", "null"},
		{"Origin", "http://localhost:1234/"},
		{"Sec-Fetch-Site", "cross-site"}, {"Sec-Fetch-Site", "same-site"},
	} {
		ctx := diskRequest("")
		ctx.Request.Header.Set(header.key, header.value)
		status, _ := route("/disk/usage", ctx)
		if status != http.StatusForbidden {
			t.Errorf("%s %q: %d", header.key, header.value, status)
		}
		if status, _ := renderDiskUsage(ctx); status != http.StatusForbidden {
			t.Error("direct handler must enforce protection too", status)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected requests must not scan")
	}
	for _, query := range []string{"?top=0", "?top=101", "?top=x", "?budget=0s", "?budget=6m", "?budget=x", "?rescan=x", "?root=a&root=b"} {
		status, _ := renderDiskUsage(diskRequest(query))
		if status != http.StatusBadRequest {
			t.Errorf("invalid options %q: %d", query, status)
		}
	}
	for _, method := range []string{http.MethodHead, http.MethodPut, http.MethodPost} {
		ctx := diskRequest("")
		ctx.Request.Method = method
		if status, _ := route("/disk/usage", ctx); status != http.StatusMethodNotAllowed {
			t.Errorf("method %s: %d", method, status)
		}
	}
	ctx := diskRequest("")
	ctx.Request.Header.Set("Origin", "http://localhost:1234")
	ctx.Request.Header.Set("Sec-Fetch-Site", "same-origin")
	if status, body := route("/disk/usage", ctx); status != http.StatusOK || !strings.Contains(body, "No regular-file bytes") {
		t.Fatalf("same-origin request: %d %s", status, body)
	}
	if calls.Load() != 1 {
		t.Fatal("valid request should scan once")
	}
	for _, root := range []string{".", "child/.."} {
		if status, _ := renderDiskUsage(diskRequest("?root=" + url.QueryEscape(root))); status != http.StatusOK {
			t.Fatal("equivalent canonical home scope rejected", root, status)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("equivalent cleaned roots must share the cache", calls.Load())
	}
}

func TestDiskFragmentDrilldownAndSafeRendering(t *testing.T) {
	withFakeDiskUsageCache(t, func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		return diskusage.Result{
			Root: root, Options: opts,
			Directories: []diskusage.Row{
				{Path: filepath.Join(root, "z"), Bytes: 50, Complete: false},
				{Path: filepath.Join(root, "a & #?"), Bytes: 50, Complete: true},
			},
			Files:     []diskusage.Row{{Path: filepath.Join(root, "<script>\n\u202e.bin"), Bytes: 20, Complete: true}},
			RootFiles: []diskusage.Row{{Path: filepath.Join(root, "root.dat"), Bytes: 3, Complete: true}},
			Entries:   1001, ElapsedMS: 123, ObservedBytes: 73,
			StopReasons: []string{"budget"}, ErrorCount: 2,
			Errors:          []diskusage.Issue{{Path: filepath.Join(root, "<bad>"), Message: "<denied>\n"}},
			SkippedSymlinks: 4, SkippedSpecial: 5, SkippedMounts: 6,
		}, nil
	})
	home, _ := defaultDiskUsageCache.home()
	child := filepath.Join(home, "personal & models")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	status, body := renderDiskUsage(diskRequest("?root=" + url.QueryEscape("personal & models")))
	if status != http.StatusOK {
		t.Fatal(status, body)
	}
	for _, want := range []string{"Home", "Up", "Rescan", `id="disk-rescan"`, `data-disk-root="personal &amp; models/a &amp; #?"`, "personal &amp; models", "Largest observed", "lower bound",
		"Files directly", "1k", "123 ms", "2 access errors", "4 symlinks", "5 special", "6 mounts", "budget",
		"logical bytes", "not guaranteed reclaim", "snapshots", "owning application", "&lt;script&gt;",
		"\\x0a", "\\u202e", "&lt;denied&gt;", "Boundary detection unavailable", "budget=30s", "top=20"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "\u202e") || strings.Contains(body, "<script>") {
		t.Fatal("raw filename controls/markup rendered")
	}
	if strings.Index(body, "a &amp; #?") > strings.Index(body, ">~/personal &amp; models/z") {
		t.Fatal("equal rows must be ordered lexically")
	}
	if !strings.Contains(html.UnescapeString(body), "root=personal+%26+models%2Fa+%26+%23%3F") {
		t.Fatal("drill-down URL must preserve raw filename with structured encoding", body)
	}
	_, rootBody := renderDiskUsage(diskRequest(""))
	if strings.Contains(rootBody, ">Up<") {
		t.Fatal("home root must not offer navigation outside home")
	}
}

func TestDiskHealthyBannerDoesNotPromiseHeadroom(t *testing.T) {
	stubResourceExtras(t)
	outHealthy := resourcePage("disk", renderDisk)(PageContext{Snapshot: doctor.Snapshot{
		Disks: []doctor.Disk{{Mount: "/", UsedPct: 50, FreeBytes: 200 << 30}},
	}})
	if !strings.Contains(outHealthy, "No configured health threshold exceeded") || !strings.Contains(outHealthy, "does not guarantee enough free space") || strings.Contains(outHealthy, "No issues found") {
		t.Fatal(outHealthy)
	}

	outElevated := resourcePage("disk", renderDisk)(PageContext{Snapshot: doctor.Snapshot{
		Disks: []doctor.Disk{{Mount: "/", UsedPct: 83, FreeBytes: 40 << 30}},
	}})
	if !strings.Contains(outElevated, "usage elevated") {
		t.Fatal("expected elevated disk finding in banner, got:", outElevated)
	}

	if out := renderDisk(doctor.Snapshot{}); !strings.Contains(out, `id="disk-usage"`) || !strings.Contains(out, "No disks reported") {
		t.Fatal("capacity failure must not suppress path analysis", out)
	}
}

func TestDiskFragmentFailureAndEmptyRecovery(t *testing.T) {
	withFakeDiskUsageCache(t, func(context.Context, string, diskusage.Options) (diskusage.Result, error) {
		return diskusage.Result{}, errors.New("cannot open <root>")
	})
	status, body := renderDiskUsage(diskRequest(""))
	if status != http.StatusInternalServerError || !strings.Contains(body, "&lt;root&gt;") || !strings.Contains(body, "Retry") {
		t.Fatal(status, body)
	}
	defaultDiskUsageCache.scan = func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		return diskusage.Result{Root: root, Options: opts, StopReasons: []string{"entry limit"}}, nil
	}
	status, body = renderDiskUsage(diskRequest("?rescan=1"))
	if status != http.StatusOK || !strings.Contains(body, "No regular-file bytes observed yet") || strings.Contains(body, "No regular-file bytes found") {
		t.Fatal(status, body)
	}
	if !strings.Contains(body, "narrow") || !strings.Contains(body, "budget") {
		t.Fatal("partial-empty needs recovery", body)
	}
	defaultDiskUsageCache.home = func() (string, error) { return "", errors.New("home unavailable") }
	status, _ = renderDiskUsage(diskRequest(""))
	if status != http.StatusInternalServerError {
		t.Fatal(status)
	}
}

func TestDiskFragmentCompleteZeroBytesAndCanceledRequest(t *testing.T) {
	withFakeDiskUsageCache(t, func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		return diskusage.Result{
			Root: root, Options: opts, Complete: true, BoundaryDetectionAvailable: true,
			RootFiles: []diskusage.Row{{Path: filepath.Join(root, "zero"), Complete: true}},
		}, nil
	})
	status, body := renderDiskUsage(diskRequest("?top=1&budget=1m"))
	for _, want := range []string{"Complete", "No regular-file bytes found", "Top 1", "budget 1m", "zero"} {
		if status != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("missing %q: %d %s", want, status, body)
		}
	}
	if strings.Contains(body, "Boundary detection unavailable") {
		t.Fatal(body)
	}
	ctx := diskRequest("")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx.Request = ctx.Request.WithContext(canceled)
	status, body = renderDiskUsage(ctx)
	if status != http.StatusRequestTimeout || !strings.Contains(body, "stopped waiting") {
		t.Fatal(status, body)
	}
	status, _ = renderDiskUsage(PageContext{})
	if status != http.StatusOK {
		t.Fatal("headerless internal/local caller should be supported", status)
	}
}

func TestDiskFragmentBusyAndBudgetRecovery(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	withFakeDiskUsageCache(t, func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		close(started)
		<-release
		return diskusage.Result{Root: root, Options: opts}, nil
	})
	home, _ := defaultDiskUsageCache.home()
	if err := os.Mkdir(filepath.Join(home, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	first := make(chan int, 1)
	go func() {
		status, _ := renderDiskUsage(diskRequest("?budget=10ms"))
		first <- status
	}()
	<-started
	status, body := renderDiskUsage(diskRequest("?root=child&budget=10ms"))
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "another disk scan") || !strings.Contains(body, "Retry") {
		t.Error(status, body)
	}
	if status = <-first; status != http.StatusServiceUnavailable {
		t.Error(status)
	}
	status, body = renderDiskUsage(diskRequest("?budget=10ms&rescan=1"))
	if status != http.StatusServiceUnavailable || !strings.Contains(body, "budget elapsed") {
		t.Error(status, body)
	}
	defaultDiskUsageCache.mu.Lock()
	done := defaultDiskUsageCache.active.done
	defaultDiskUsageCache.mu.Unlock()
	close(release)
	<-done
}

func TestDiskBudgetExpirationDeliversPromptPartialResult(t *testing.T) {
	withFakeDiskUsageCache(t, func(ctx context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		<-ctx.Done()
		// Cooperative workers need time to drain and aggregate after cancellation.
		time.Sleep(5 * time.Millisecond)
		return diskusage.Result{
			Root: root, Options: opts, ObservedBytes: 4096,
			Directories: []diskusage.Row{{Path: filepath.Join(root, "large"), Bytes: 4096}},
			StopReasons: []string{"budget"}, ElapsedMS: opts.Budget.Milliseconds() + 5,
		}, nil
	})
	r := diskRequest("?budget=1ms").Request
	out := httptest.NewRecorder()
	dashboardHandler(Options{}, nil).ServeHTTP(out, r)
	if out.Code != http.StatusOK {
		t.Fatalf("cooperative partial results discarded: %d %s", out.Code, out.Body.String())
	}
	for _, want := range []string{"Largest observed", "~/large", "4.00 KB", "lower bound", `id="disk-rescan"`, `name="root"`} {
		if !strings.Contains(out.Body.String(), want) {
			t.Errorf("partial result missing %q: %s", want, out.Body.String())
		}
	}
}

func TestDiskErrorRecoveryProvidesInterceptedLinksAndScopeForm(t *testing.T) {
	body := diskRecovery("scan budget elapsed", diskURL("models", diskusage.Options{Top: 5, Budget: time.Minute}, true))
	for _, want := range []string{`data-disk-scan href=`, `id="disk-scope-options"`, `name="root"`, `value="models"`, `value="5"`, `value="1m0s"`} {
		if !strings.Contains(body, want) {
			t.Errorf("recovery missing %q: %s", want, body)
		}
	}
	for _, want := range []string{"closest('[data-disk-scan]')", "e.preventDefault(); load(", "disk-scope-options", "q.set('root', data.get('root'))"} {
		if !strings.Contains(diskUsageScript, want) {
			t.Errorf("client recovery missing %q", want)
		}
	}
}

func TestDiskProtectedFragmentAllowsOnlySameOriginGET(t *testing.T) {
	r := diskRequest("").Request
	for _, site := range []string{"", "none", "same-origin"} {
		r.Header.Set("Sec-Fetch-Site", site)
		if got := protectedFragmentStatus(r); got != http.StatusOK {
			t.Fatal(site, got)
		}
	}
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Origin", "https://localhost:1234")
	if got := protectedFragmentStatus(r); got != http.StatusOK {
		t.Fatal(got)
	}
	r.Header.Set("Origin", "http://localhost:1234")
	if got := protectedFragmentStatus(r); got != http.StatusForbidden {
		t.Fatal(got)
	}
}

func TestDiskHomeRootLiveWiringUsesOnlyIsolatedFixture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := os.Mkdir(filepath.Join(home, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "child", "data"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, handle, info, err := confinedDiskRoot(home, "child")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := handle.Stat(".")
	handle.Close()
	if err != nil || !os.SameFile(info, opened) {
		t.Fatal("selected directory identity not preserved", err)
	}
	result, err := scanHomeDirectory(context.Background(), root, diskusage.DefaultOptions())
	if err != nil || result.ObservedBytes != 5 || !result.Complete {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(home, "child", "data")); err != nil {
		t.Fatal("scan must not mutate fixture", err)
	}
	if _, err := scanHomeDirectory(context.Background(), filepath.Dir(root), diskusage.Options{}); err == nil {
		t.Fatal("scanner option validation must remain shared")
	}
	if _, err := scanHomeDirectory(context.Background(), t.TempDir(), diskusage.DefaultOptions()); err == nil {
		t.Fatal("live seam must recheck home confinement")
	}
	if _, handle, _, err := confinedDiskRoot(filepath.Join(home, "missing"), ""); err == nil {
		handle.Close()
		t.Fatal("missing home accepted")
	}
}

func TestDiskRootValidationClosesHandlesOnFailuresAndReplacement(t *testing.T) {
	home := t.TempDir()
	other, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fail := errors.New("injected filesystem failure")
	for _, kind := range []string{"resolve", "anchor", "lstat", "child", "opened-stat", "post-lstat", "opened-identity", "post-identity", "anchor-stat"} {
		t.Run(kind, func(t *testing.T) {
			ops := liveDiskRootOps
			var handles []*os.Root
			ops.open = func(path string) (*os.Root, error) {
				if kind == "anchor" {
					return nil, fail
				}
				root, err := os.OpenRoot(path)
				if err == nil {
					handles = append(handles, root)
				}
				return root, err
			}
			ops.child = func(root *os.Root, name string) (*os.Root, error) {
				if kind == "child" {
					return nil, fail
				}
				child, err := root.OpenRoot(name)
				if err == nil {
					handles = append(handles, child)
				}
				return child, err
			}
			switch kind {
			case "resolve":
				ops.resolve = func(string) (string, error) { return "", fail }
			case "lstat":
				ops.lstat = func(*os.Root, string) (os.FileInfo, error) { return nil, fail }
			case "post-lstat", "post-identity":
				calls := 0
				ops.lstat = func(root *os.Root, name string) (os.FileInfo, error) {
					calls++
					if calls == 2 {
						if kind == "post-lstat" {
							return nil, fail
						}
						return other, nil
					}
					return root.Lstat(name)
				}
			case "opened-stat":
				ops.stat = func(*os.Root) (os.FileInfo, error) { return nil, fail }
			case "opened-identity":
				ops.stat = func(*os.Root) (os.FileInfo, error) { return other, nil }
			case "anchor-stat":
				ops.resolve = func(string) (string, error) {
					return filepath.VolumeName(home) + string(filepath.Separator), nil
				}
				ops.stat = func(*os.Root) (os.FileInfo, error) { return nil, fail }
			}
			_, root, _, err := confinedDiskRootWith(home, "", ops)
			if err == nil {
				root.Close()
				t.Fatal("injected failure/replacement accepted")
			}
			for _, handle := range handles {
				if _, err := handle.Stat("."); err == nil {
					handle.Close()
					t.Error("failed validation leaked an open rooted handle")
				}
			}
		})
	}
	anchor := filepath.VolumeName(home) + string(filepath.Separator)
	_, root, info, err := confinedDiskRoot(anchor, "")
	if err != nil || info == nil {
		t.Fatal("filesystem anchor should still return a validated handle", err)
	}
	root.Close()
}

func TestDiskHomeScanNeverReopensDisplayRootAndChecksReplacement(t *testing.T) {
	home := t.TempDir()
	homeDir := func() (string, error) { return home, nil }
	root, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	fail := errors.New("home unavailable")
	if _, err := scanHomeDirectoryWith(context.Background(), root, diskusage.DefaultOptions(),
		func() (string, error) { return "", fail }, os.Lstat, diskusage.ScanRoot); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	for _, stage := range []int{1, 2} {
		calls := 0
		stat := func(path string) (os.FileInfo, error) {
			calls++
			if calls == stage {
				return nil, fail
			}
			return os.Lstat(path)
		}
		scans := 0
		scan := func(_ context.Context, handle *os.Root, label string, _ diskusage.Options) (diskusage.Result, error) {
			scans++
			if label != root {
				t.Errorf("display root = %q, want canonical %q", label, root)
			}
			if _, err := handle.Stat("."); err != nil {
				t.Error("rooted handle must stay alive during scan", err)
			}
			return diskusage.Result{Root: label}, nil
		}
		if _, err := scanHomeDirectoryWith(context.Background(), root, diskusage.DefaultOptions(), homeDir, stat, scan); err == nil {
			t.Fatal("identity verification failure accepted", stage)
		}
		if scans != stage-1 {
			t.Fatal("scan must start only after identity validation", stage, scans)
		}
	}
}

func TestDiskRowsPreserveOutsideDisplayButNeverLinkOutsideHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	outside := filepath.Join(filepath.Dir(home), "outside")
	rows := diskRows([]diskusage.Row{{Path: outside, Bytes: 10}}, home, diskusage.DefaultOptions(), true)
	if len(rows) != 1 || rows[0].URL != "" || rows[0].Path != filepath.ToSlash(outside) || !rows[0].Partial {
		t.Fatal(rows)
	}
}

func TestDiskRowsSortDescendingWithLexicalTies(t *testing.T) {
	home := t.TempDir()
	rows := diskRows([]diskusage.Row{
		{Path: filepath.Join(home, "b"), Bytes: 1, Complete: true},
		{Path: filepath.Join(home, "a"), Bytes: 1, Complete: true},
		{Path: filepath.Join(home, "large"), Bytes: 2, Complete: true},
	}, home, diskusage.DefaultOptions(), true)
	if len(rows) != 3 || rows[0].Path != "~/large" || rows[1].Path != "~/a" || rows[2].Path != "~/b" {
		t.Fatal(rows)
	}
}

func TestDiskUsageCacheTTLRefreshOptionsAndEviction(t *testing.T) {
	c := newDiskUsageCache()
	c.maxEntries = 4
	now := time.Unix(100, 0)
	c.now = func() time.Time { return now }
	var calls int
	c.scan = func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		calls++
		return diskusage.Result{Root: root, Options: opts, Complete: true}, nil
	}
	opts := diskusage.DefaultOptions()
	get := func(root string, refresh bool) {
		t.Helper()
		if _, err := c.Get(context.Background(), root, opts, refresh); err != nil {
			t.Fatal(err)
		}
	}
	get("a", false)
	get("a", false)
	if calls != 1 {
		t.Fatal(calls)
	}
	get("a", true)
	opts.Top = 1
	get("a", false)
	opts.Budget = time.Second
	get("a", false)
	now = now.Add(diskScanCacheTTL)
	get("a", false)
	if calls != 5 {
		t.Fatal(calls)
	}
	for _, root := range []string{"b", "c", "d", "e"} {
		get(root, false)
	}
	if len(c.values) != 4 {
		t.Fatal("cache must retain four entries", len(c.values))
	}
	get("a", false)
	if calls != 10 {
		t.Fatal("oldest root must have been evicted", calls)
	}
	get("a", true)
	if len(c.values) != 4 {
		t.Fatal("refreshing an existing cache key must not evict another root", len(c.values))
	}
}

func TestDiskUsageCacheCoalescesAndKeepsAdmissionUntilReturn(t *testing.T) {
	c := newDiskUsageCache()
	c.drainGrace = 10 * time.Millisecond
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var scanCtx context.Context
	c.scan = func(ctx context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		if calls.Add(1) == 1 {
			scanCtx = ctx
			close(started)
			<-release // Simulate a filesystem syscall ignoring cancellation.
		}
		return diskusage.Result{Root: root, Complete: true}, nil
	}
	opts := diskusage.DefaultOptions()
	opts.Budget = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := c.Get(ctx, "a", opts, false); first <- err }()
	<-started
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := scanCtx.Err(); err != nil {
		t.Fatal("a waiter cancellation must not cancel shared scanning", err)
	}
	if _, err := c.Get(context.Background(), "b", opts, false); !errors.Is(err, errDiskScanBusy) {
		t.Fatal("distinct scan admitted while syscall active", err)
	}
	if _, err := c.Get(context.Background(), "a", opts, true); !errors.Is(err, errDiskScanTimeout) {
		t.Fatal("coalesced request should stop waiting at shared budget", err)
	}
	if calls.Load() != 1 {
		t.Fatal("rescan should coalesce while active", calls.Load())
	}
	c.mu.Lock()
	done := c.active.done
	c.mu.Unlock()
	close(release)
	<-done
	if _, err := c.Get(context.Background(), "b", opts, false); err != nil {
		t.Fatal(err)
	}
}

func TestDiskUsageCacheConcurrentSameKey(t *testing.T) {
	c := newDiskUsageCache()
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int32
	c.scan = func(_ context.Context, root string, _ diskusage.Options) (diskusage.Result, error) {
		calls.Add(1)
		close(started)
		<-release
		return diskusage.Result{Root: root, Entries: 42}, nil
	}
	opts := diskusage.DefaultOptions()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := c.Get(context.Background(), "a", opts, false); err != nil {
			t.Error(err)
		}
	}()
	<-started
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := c.Get(context.Background(), "a", opts, false)
			if err != nil || res.Entries != 42 {
				t.Error(res, err)
			}
		}()
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestFormatCount(t *testing.T) {
	for in, want := range map[int]string{5: "5", 12000: "12k", 2_500_000: "2.5M"} {
		if got := formatCount(in); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDiskUsageCacheDefaultCapacityAndTTL(t *testing.T) {
	c := newDiskUsageCache()
	if c.maxCap() != 64 {
		t.Fatalf("expected default max capacity 64, got %d", c.maxCap())
	}
	if c.ttl != 10*time.Minute {
		t.Fatalf("expected default TTL 10m, got %v", c.ttl)
	}
}

func TestDiskUsageCacheLatest(t *testing.T) {
	c := newDiskUsageCache()
	now := time.Unix(100, 0)
	c.now = func() time.Time { return now }

	if got := c.Latest(); got != nil {
		t.Fatalf("expected nil when cache empty, got %+v", got)
	}

	c.scan = func(_ context.Context, root string, opts diskusage.Options) (diskusage.Result, error) {
		return diskusage.Result{Root: root, Entries: 10, Complete: true}, nil
	}
	opts := diskusage.DefaultOptions()
	if _, err := c.Get(context.Background(), "first", opts, false); err != nil {
		t.Fatal(err)
	}
	latest := c.Latest()
	if latest == nil || latest.Root != "first" {
		t.Fatalf("expected root 'first', got %+v", latest)
	}

	// Advance time beyond TTL
	now = now.Add(11 * time.Minute)
	if got := c.Latest(); got != nil {
		t.Fatalf("expected nil when result expired, got %+v", got)
	}
}

func TestBuildDonutChart(t *testing.T) {
	// Empty items
	emptyChart := buildDonutChart("Empty", nil, 100)
	if emptyChart.HasData {
		t.Fatal("expected HasData=false for nil items")
	}

	// Populated items
	items := []diskusage.Row{
		{Path: "dir1", Bytes: 600},
		{Path: "dir2", Bytes: 300},
		{Path: "dir3\u202e", Bytes: 100},
	}
	chart := buildDonutChart("Top Folders", items, 1000)
	if !chart.HasData {
		t.Fatal("expected HasData=true")
	}
	if len(chart.Slices) != 3 {
		t.Fatalf("expected 3 slices, got %d", len(chart.Slices))
	}
	// Check sanitized label for Unicode control
	if strings.Contains(chart.Slices[2].Label, "\u202e") {
		t.Fatal("expected slice label to have sanitized control characters")
	}
	if !strings.Contains(chart.Slices[2].Label, "\\u202e") {
		t.Fatalf("expected escaped label, got %s", chart.Slices[2].Label)
	}
}
