package dashboard

import (
	"strings"
	"testing"

	"vitals/internal/clean"
)

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
