package dashboard

import (
	"net/http"
	"strings"
	"testing"

	"vitals/internal/clean"
)

// withFakeCleanApply swaps cleanApplyFn (the injected live call
// handleCleanApply makes) for fn, restored after the test. clean.Apply
// itself is never safe to call from an automated test in non-dry-run
// mode on any OS — internal/clean/clean_test.go's own
// TestApplyDryRunNeverMutatesAndReturnsAStructuredResult documents why:
// cleanLinux's /var/tmp and /tmp purge ignores the home parameter
// entirely, and cleanDevCaches unconditionally shells out to real
// package managers (docker, npm, pnpm, pip) if they're on PATH. This
// fake is what makes handleCleanApply's own logic (parsing, the
// single-flight guard, rendering) testable without ever running a real,
// deleting clean.Apply call.
func withFakeCleanApply(t *testing.T, fn func(string, clean.Options) clean.Result) {
	t.Helper()
	old := cleanApplyFn
	cleanApplyFn = fn
	t.Cleanup(func() { cleanApplyFn = old })
}

func TestRenderCleanPreviewShowsTheTotalAndEachEntry(t *testing.T) {
	entries := []clean.CacheEntry{
		{Path: "/home/x/.cache", Bytes: 1024},
		{Path: "/home/x/Library/Caches", Bytes: 2048},
	}
	out := renderCleanPreview(entries, 3072, true)
	for _, want := range []string{"/home/x/.cache", "/home/x/Library/Caches"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCleanPreview missing entry %q, got: %s", want, out)
		}
	}
	if !strings.Contains(out, "Would reclaim") {
		t.Errorf("renderCleanPreview should show the total, got: %s", out)
	}
}

func TestRenderCleanPreviewEscapesACraftedPath(t *testing.T) {
	// A cache directory's own name is real filesystem data (usernames,
	// profile directory names) this handler doesn't control — the same
	// class of untrusted-ish string the review flagged, so it must go
	// through html/template like every other render function in this
	// package, not a hand-written string concat.
	entries := []clean.CacheEntry{{Path: "<script>alert(1)</script>", Bytes: 100}}
	out := renderCleanPreview(entries, 100, true)
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("renderCleanPreview did not escape a crafted path, got: %s", out)
	}
}

func TestRenderCleanPreviewNotesAnIncompleteMeasurement(t *testing.T) {
	out := renderCleanPreview(nil, 0, false)
	if !strings.Contains(strings.ToLower(out), "incomplete") {
		t.Errorf("renderCleanPreview should note an incomplete measurement, got: %s", out)
	}
}

func TestRenderCleanPreviewEmptyIsFriendly(t *testing.T) {
	out := renderCleanPreview(nil, 0, true)
	if !strings.Contains(strings.ToLower(out), "nothing measurable") {
		t.Errorf("renderCleanPreview with no entries should say so, got: %s", out)
	}
}

func TestHandleCleanPreviewReturns200WithARenderedBody(t *testing.T) {
	// Exercises the real ReclaimableSummary against this machine's
	// actual home directory — read-only measurement, never deletes
	// anything, matching how ReclaimableSummary's own package is
	// otherwise only tested through its pure measureDirs core.
	status, body := handleCleanPreview(PageContext{}, nil)
	if status != 200 {
		t.Errorf("status = %d, want 200", status)
	}
	if !strings.Contains(body, "Would reclaim") {
		t.Errorf("body missing the rendered total: %s", body)
	}
}

func TestRenderCleanPageHasTheButtonAndResultContainer(t *testing.T) {
	out := renderCleanPage(PageContext{})
	for _, want := range []string{`id="clean-preview-btn"`, `id="clean-preview-result"`, "/clean/preview"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCleanPage missing %q, got: %s", want, out)
		}
	}
}

func TestCleanModuleIsRegistered(t *testing.T) {
	// Runs against the REAL registry, same pattern
	// TestModulesRegisterThemselvesWithDistinctSlugs uses.
	m, exists, available := findModule("clean", PageContext{})
	if !exists {
		t.Fatal("clean module should be registered")
	}
	if !available {
		t.Error("clean module should always be available")
	}
	if m.NavLabel != "Clean" {
		t.Errorf("NavLabel = %q, want Clean", m.NavLabel)
	}
}

func TestCleanPreviewWriteActionIsRegistered(t *testing.T) {
	// Runs against the REAL registry (populated by modules_clean.go's
	// own init()), same pattern TestModulesRegisterThemselvesWithDistinctSlugs
	// uses for the read-only registry.
	found := false
	for _, a := range writeActions {
		if a.Path == "/clean/preview" {
			found = true
		}
	}
	if !found {
		t.Error("/clean/preview should be registered as a WriteAction")
	}
}

func TestCleanApplyWriteActionIsRegistered(t *testing.T) {
	found := false
	for _, a := range writeActions {
		if a.Path == "/clean/apply" {
			found = true
		}
	}
	if !found {
		t.Error("/clean/apply should be registered as a WriteAction")
	}
}

// --- /clean/apply: confirm-body validation ---------------------------------
// Each of these must reject with 400 before clean.Apply is ever called —
// the fake below fails the test if it's invoked at all, per
// design.md §7's "before clean.Apply is ever called" requirement.

func failIfCleanApplyCalled(t *testing.T) func(string, clean.Options) clean.Result {
	return func(string, clean.Options) clean.Result {
		t.Fatal("clean.Apply must not be called when confirm validation fails")
		return clean.Result{}
	}
}

func TestHandleCleanApplyRejectsMissingBody(t *testing.T) {
	withFakeCleanApply(t, failIfCleanApplyCalled(t))
	status, body := handleCleanApply(PageContext{}, nil)
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "confirm") {
		t.Errorf("body should explain the confirm requirement, got: %s", body)
	}
}

func TestHandleCleanApplyRejectsFalseConfirm(t *testing.T) {
	withFakeCleanApply(t, failIfCleanApplyCalled(t))
	status, _ := handleCleanApply(PageContext{}, []byte(`{"confirm": false}`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestHandleCleanApplyRejectsMalformedJSON(t *testing.T) {
	withFakeCleanApply(t, failIfCleanApplyCalled(t))
	status, _ := handleCleanApply(PageContext{}, []byte(`{not json`))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestHandleCleanApplyIgnoresUnknownExtraFields(t *testing.T) {
	// encoding/json's default: an unknown field is ignored, not rejected —
	// design.md §7 deliberately avoids a strict-schema requirement for a
	// single boolean.
	withFakeCleanApply(t, func(string, clean.Options) clean.Result { return clean.Result{} })
	status, _ := handleCleanApply(PageContext{}, []byte(`{"confirm": true, "extra": "field"}`))
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 (unknown fields should be ignored)", status)
	}
}

// --- /clean/apply: success path ---------------------------------------------

func TestHandleCleanApplyReturns200WithARenderedResultOnConfirm(t *testing.T) {
	withFakeCleanApply(t, func(home string, opts clean.Options) clean.Result {
		if !opts.Assume {
			t.Errorf("Options.Assume = false, want true")
		}
		if opts.DryRun {
			t.Errorf("Options.DryRun = true, want false — apply must actually delete")
		}
		return clean.Result{
			FreedBytes: 4096,
			Records:    []clean.PurgeRecord{{Dir: "/home/x/.cache", Bytes: 4096, Entries: 3}},
		}
	})
	status, body := handleCleanApply(PageContext{}, []byte(`{"confirm": true}`))
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(body, "/home/x/.cache") {
		t.Errorf("body missing the rendered record: %s", body)
	}
}

// --- /clean/apply: single-flight guard --------------------------------------

func TestHandleCleanApplyRejectsAConcurrentSecondCall(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	withFakeCleanApply(t, func(string, clean.Options) clean.Result {
		close(started)
		<-release
		return clean.Result{}
	})

	statuses := make(chan int, 1)
	go func() {
		status, _ := handleCleanApply(PageContext{}, []byte(`{"confirm": true}`))
		statuses <- status
	}()
	<-started // the first call now holds cleanApplyMu inside cleanApplyFn

	status2, body2 := handleCleanApply(PageContext{}, []byte(`{"confirm": true}`))
	close(release)
	status1 := <-statuses

	if status2 != http.StatusConflict {
		t.Errorf("concurrent second call = %d, want 409, body: %s", status2, body2)
	}
	if status1 != http.StatusOK {
		t.Errorf("first call = %d, want 200", status1)
	}
}

func TestHandleCleanApplyAllowsASecondCallAfterTheFirstFinishes(t *testing.T) {
	// A second apply should eventually be allowed to run, just never
	// concurrently with a first one still in flight (design.md §7).
	withFakeCleanApply(t, func(string, clean.Options) clean.Result { return clean.Result{} })

	status1, _ := handleCleanApply(PageContext{}, []byte(`{"confirm": true}`))
	if status1 != http.StatusOK {
		t.Fatalf("first call = %d, want 200", status1)
	}
	status2, _ := handleCleanApply(PageContext{}, []byte(`{"confirm": true}`))
	if status2 != http.StatusOK {
		t.Errorf("second, non-concurrent call = %d, want 200", status2)
	}
}

// --- renderCleanApplyResult ---------------------------------------------------

func TestRenderCleanApplyResultShowsTheTotalAndEachRecord(t *testing.T) {
	result := clean.Result{
		FreedBytes: 3072,
		Records: []clean.PurgeRecord{
			{Dir: "/home/x/.cache", Bytes: 1024, Entries: 2},
			{Dir: "/home/x/Library/Caches", Bytes: 2048, Entries: 5},
		},
	}
	out := renderCleanApplyResult(result)
	for _, want := range []string{"/home/x/.cache", "/home/x/Library/Caches"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCleanApplyResult missing entry %q, got: %s", want, out)
		}
	}
	if !strings.Contains(out, "Freed") {
		t.Errorf("renderCleanApplyResult should show the total, got: %s", out)
	}
}

func TestRenderCleanApplyResultEscapesACraftedPath(t *testing.T) {
	// A purged directory's own path is real filesystem data this handler
	// doesn't control, same class of untrusted-ish string as
	// renderCleanPreview's TestRenderCleanPreviewEscapesACraftedPath —
	// must go through html/template, not a hand-written string concat.
	result := clean.Result{
		FreedBytes: 100,
		Records:    []clean.PurgeRecord{{Dir: "<script>alert(1)</script>", Bytes: 100, Entries: 1}},
	}
	out := renderCleanApplyResult(result)
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("renderCleanApplyResult did not escape a crafted path, got: %s", out)
	}
}

func TestRenderCleanApplyResultEmptyIsFriendly(t *testing.T) {
	out := renderCleanApplyResult(clean.Result{})
	if !strings.Contains(strings.ToLower(out), "nothing") {
		t.Errorf("renderCleanApplyResult with no records should say so, got: %s", out)
	}
}

// --- clean page: Apply button ------------------------------------------------

func TestRenderCleanPageHasTheApplyButtonHiddenUntilPreviewRenders(t *testing.T) {
	out := renderCleanPage(PageContext{})
	for _, want := range []string{`id="clean-apply-btn"`, "/clean/apply", `id="clean-apply-result"`} {
		if !strings.Contains(out, want) {
			t.Errorf("renderCleanPage missing %q, got: %s", want, out)
		}
	}
	// The Apply button must start hidden — it only appears once a preview
	// response has rendered (design.md §7's client-side UX section).
	if !strings.Contains(out, `id="clean-apply-btn" hidden`) {
		t.Errorf("renderCleanPage's Apply button should start hidden, got: %s", out)
	}
}
