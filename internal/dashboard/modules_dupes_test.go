package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vitals/internal/dupes"
)

// withFakeDupesApply swaps dupesApplyFn (the injected live call
// handleDupesHardlink makes) for fn, restored after the test — same
// reasoning as modules_clean_test.go's withFakeCleanApply: this makes
// handleDupesHardlink's own logic (parsing, scope resolution, the
// single-flight guard, rendering) testable without depending on a real
// filesystem mutation for every branch.
func withFakeDupesApply(t *testing.T, fn func([]dupes.Group) (int, int64, []string)) {
	t.Helper()
	old := dupesApplyFn
	dupesApplyFn = fn
	t.Cleanup(func() { dupesApplyFn = old })
}

// --- resolveScope: pure, table-driven, every OS from one machine -----------

func TestResolveScopeKnownKeysOnEveryOS(t *testing.T) {
	home := "/home/x"
	cases := []struct {
		key, goos, wantRoot string
	}{
		{"home", "darwin", "/home/x"},
		{"home", "linux", "/home/x"},
		{"home", "windows", "/home/x"},
		{"downloads", "darwin", filepath.Join(home, "Downloads")},
		{"downloads", "linux", filepath.Join(home, "Downloads")},
		{"downloads", "windows", filepath.Join(home, "Downloads")},
		{"caches", "darwin", filepath.Join(home, "Library", "Caches")},
		{"caches", "linux", filepath.Join(home, ".cache")},
	}
	for _, c := range cases {
		root, minSize, ok := resolveScope(c.key, home, c.goos)
		if !ok {
			t.Errorf("resolveScope(%q, %q) = not ok, want ok", c.key, c.goos)
			continue
		}
		if root != c.wantRoot {
			t.Errorf("resolveScope(%q, %q) root = %q, want %q", c.key, c.goos, root, c.wantRoot)
		}
		if minSize != dupesMinSize {
			t.Errorf("resolveScope(%q, %q) minSize = %d, want %d", c.key, c.goos, minSize, dupesMinSize)
		}
	}
}

func TestResolveScopeCachesUnsupportedOnWindows(t *testing.T) {
	if _, _, ok := resolveScope("caches", "/home/x", "windows"); ok {
		t.Error("resolveScope(caches, windows) should be unsupported — no single well-known cache root")
	}
}

func TestResolveScopeUnknownKeyIsRejected(t *testing.T) {
	for _, key := range []string{"", "root", "/etc", "../etc"} {
		if _, _, ok := resolveScope(key, "/home/x", "linux"); ok {
			t.Errorf("resolveScope(%q) should be rejected, got ok", key)
		}
	}
}

// --- renderDupesPreview -----------------------------------------------------

func TestRenderDupesPreviewShowsTotalAndGroups(t *testing.T) {
	r := dupes.Result{
		Root:         "/home/x",
		ScannedFiles: 42,
		WastedBytes:  2048,
		Groups: []dupes.Group{
			{SizeBytes: 1024, Paths: []string{"/home/x/a.txt", "/home/x/b.txt"}},
		},
	}
	out := renderDupesPreview(r)
	// Each path renders split — dir and filename in separate elements
	// (file-explorer-style: the filename is what stands out, not buried
	// at the end of a long monospace line) — so this asserts on each
	// half, not the joined "/home/x/a.txt" string.
	for _, want := range []string{"/home/x", "a.txt", "b.txt", "reclaimable", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDupesPreview missing %q, got: %s", want, out)
		}
	}
}

func TestNewDupesPathDisplaySplitsDirAndFilename(t *testing.T) {
	d := newDupesPathDisplay("/home/x/Downloads/report.pdf")
	if d.Dir != "/home/x/Downloads" {
		t.Errorf("Dir = %q, want /home/x/Downloads", d.Dir)
	}
	if d.Name != "report.pdf" {
		t.Errorf("Name = %q, want report.pdf", d.Name)
	}
}

func TestRenderDupesPreviewEscapesACraftedPath(t *testing.T) {
	r := dupes.Result{Groups: []dupes.Group{
		{SizeBytes: 10, Paths: []string{"<script>alert(1)</script>", "/home/x/b"}},
	}}
	out := renderDupesPreview(r)
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("renderDupesPreview did not escape a crafted path, got: %s", out)
	}
}

func TestRenderDupesPreviewNotesATruncatedScan(t *testing.T) {
	out := renderDupesPreview(dupes.Result{Truncated: true})
	if !strings.Contains(strings.ToLower(out), "stopped early") {
		t.Errorf("renderDupesPreview should note a truncated scan, got: %s", out)
	}
}

func TestRenderDupesPreviewEmptyIsFriendly(t *testing.T) {
	out := renderDupesPreview(dupes.Result{})
	if !strings.Contains(strings.ToLower(out), "no duplicate files found") {
		t.Errorf("renderDupesPreview with no groups should say so, got: %s", out)
	}
}

func TestRenderDupesPreviewCapsDisplayedGroupsAndNotesTheRest(t *testing.T) {
	groups := make([]dupes.Group, dupesGroupDisplayLimit+5)
	for i := range groups {
		groups[i] = dupes.Group{SizeBytes: 10, Paths: []string{"/a", "/b"}}
	}
	out := renderDupesPreview(dupes.Result{Groups: groups})
	if !strings.Contains(out, "...and 5 more group") {
		t.Errorf("renderDupesPreview should note the groups past the display cap, got: %s", out)
	}
}

// --- renderDupesHardlinkResult -----------------------------------------------

func TestRenderDupesHardlinkResultShowsCountAndFailures(t *testing.T) {
	out := renderDupesHardlinkResult(3, 4096, []string{"/nonexistent: no such file"})
	if !strings.Contains(out, "Hardlinked 3 file") {
		t.Errorf("renderDupesHardlinkResult missing linked count, got: %s", out)
	}
	if !strings.Contains(out, "/nonexistent: no such file") {
		t.Errorf("renderDupesHardlinkResult missing the failure, got: %s", out)
	}
}

func TestRenderDupesHardlinkResultEscapesACraftedFailure(t *testing.T) {
	out := renderDupesHardlinkResult(0, 0, []string{"<script>alert(1)</script>"})
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("renderDupesHardlinkResult did not escape a crafted failure, got: %s", out)
	}
}

// --- page / registry ---------------------------------------------------------

func TestRenderDupesPageHasScopeSelectAndButtons(t *testing.T) {
	out := renderDupesPage(PageContext{})
	for _, want := range []string{`id="dupes-scope"`, `value="home"`, `value="downloads"`, `value="caches"`,
		`id="dupes-preview-btn"`, `id="dupes-hardlink-btn" hidden`, "/dupes/preview", "/dupes/hardlink"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDupesPage missing %q, got: %s", want, out)
		}
	}
}

func TestDupesModuleIsRegistered(t *testing.T) {
	m, exists, available := findModule("dupes", PageContext{})
	if !exists {
		t.Fatal("dupes module should be registered")
	}
	if !available {
		t.Error("dupes module should always be available")
	}
	if m.NavLabel != "Duplicates" {
		t.Errorf("NavLabel = %q, want Duplicates", m.NavLabel)
	}
}

func TestDupesWriteActionsAreRegistered(t *testing.T) {
	for _, path := range []string{"/dupes/preview", "/dupes/hardlink"} {
		found := false
		for _, a := range writeActions {
			if a.Path == path {
				found = true
			}
		}
		if !found {
			t.Errorf("%s should be registered as a WriteAction", path)
		}
	}
}

// --- /dupes/preview: real scan against a temp tree --------------------------

func TestHandleDupesPreviewScansARealTempTreeUnderHomeScope(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	content := []byte("duplicate content for the dashboard preview test")
	writeFile(t, dir, "a.txt", content)
	writeFile(t, dir, "b.txt", content)

	status, body := handleDupesPreview(PageContext{}, []byte(`{"scope":"home"}`))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", status, body)
	}
	if !strings.Contains(body, "reclaimable") {
		t.Errorf("body missing the rendered total: %s", body)
	}
}

func TestHandleDupesPreviewUnknownScopeRejects(t *testing.T) {
	status, body := handleDupesPreview(PageContext{}, []byte(`{"scope":"not-a-scope"}`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, body: %s", status, body)
	}
}

func TestHandleDupesPreviewMalformedBodyRejects(t *testing.T) {
	status, _ := handleDupesPreview(PageContext{}, []byte(`{not json`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

// writeFile is dupes' own test helper, duplicated here (dashboard's test
// package can't import internal/dupes' unexported write helper) — same
// content it writes, just under this package's own name to avoid
// colliding with any future "write" helper here.
func writeFile(t *testing.T, root, rel string, content []byte) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// --- /dupes/hardlink: confirm/scope validation -------------------------------
// Each of these must reject before dupesApplyFn is ever called.

func failIfDupesApplyCalled(t *testing.T) func([]dupes.Group) (int, int64, []string) {
	return func([]dupes.Group) (int, int64, []string) {
		t.Fatal("dupes.ApplyHardlinks must not be called when confirm/scope validation fails")
		return 0, 0, nil
	}
}

func TestHandleDupesHardlinkRejectsMissingConfirm(t *testing.T) {
	withFakeDupesApply(t, failIfDupesApplyCalled(t))
	status, body := handleDupesHardlink(PageContext{}, []byte(`{"scope":"home"}`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "confirm") {
		t.Errorf("body should explain the confirm requirement, got: %s", body)
	}
}

func TestHandleDupesHardlinkRejectsFalseConfirm(t *testing.T) {
	withFakeDupesApply(t, failIfDupesApplyCalled(t))
	status, _ := handleDupesHardlink(PageContext{}, []byte(`{"confirm": false, "scope":"home"}`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestHandleDupesHardlinkRejectsMalformedJSON(t *testing.T) {
	withFakeDupesApply(t, failIfDupesApplyCalled(t))
	status, _ := handleDupesHardlink(PageContext{}, []byte(`{not json`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestHandleDupesHardlinkRejectsUnknownScope(t *testing.T) {
	withFakeDupesApply(t, failIfDupesApplyCalled(t))
	status, _ := handleDupesHardlink(PageContext{}, []byte(`{"confirm": true, "scope":"not-a-scope"}`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

// --- /dupes/hardlink: success path against a real temp tree -----------------

func TestHandleDupesHardlinkReturns200AndLinksARealDuplicate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// The "home" scope's minSize is a real 1 MiB (dupesMinSize, matching
	// design-dupes.md's own scope table) — a small test file would be
	// silently filtered out before it ever became a scan candidate, the
	// same behavior internal/dupes' own TestScanIgnoresFilesBelowMinSize
	// documents, so this needs real 1 MiB+ content, not a short string.
	content := make([]byte, dupesMinSize)
	for i := range content {
		content[i] = byte(i)
	}
	a := writeFile(t, dir, "a.bin", content)
	b := writeFile(t, dir, "b.bin", content)

	status, body := handleDupesHardlink(PageContext{}, []byte(`{"confirm": true, "scope":"home"}`))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", status, body)
	}
	if !strings.Contains(body, "Hardlinked 1 file") {
		t.Errorf("body missing the linked count: %s", body)
	}
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(fa, fb) {
		t.Error("a.txt and b.txt should be the same file (hardlinked) after apply")
	}
}

// --- /dupes/hardlink: single-flight guard ------------------------------------

func TestHandleDupesHardlinkRejectsAConcurrentSecondCall(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep the scan fast and deterministic, not the real home
	release := make(chan struct{})
	started := make(chan struct{})
	withFakeDupesApply(t, func([]dupes.Group) (int, int64, []string) {
		close(started)
		<-release
		return 0, 0, nil
	})

	statuses := make(chan int, 1)
	go func() {
		status, _ := handleDupesHardlink(PageContext{}, []byte(`{"confirm": true, "scope":"home"}`))
		statuses <- status
	}()
	<-started // the first call now holds dupesApplyMu inside dupesApplyFn

	status2, body2 := handleDupesHardlink(PageContext{}, []byte(`{"confirm": true, "scope":"home"}`))
	close(release)
	status1 := <-statuses

	if status2 != http.StatusConflict {
		t.Errorf("concurrent second call = %d, want 409, body: %s", status2, body2)
	}
	if status1 != http.StatusOK {
		t.Errorf("first call = %d, want 200", status1)
	}
}
