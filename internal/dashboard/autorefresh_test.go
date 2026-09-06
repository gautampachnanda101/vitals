package dashboard

import (
	"strings"
	"testing"
)

func TestLayoutLiveEmitsTheRefreshHookAndToggle(t *testing.T) {
	live := layoutLive("CPU", "cpu", "1.0", nil, "BODY", true)
	if !strings.Contains(live, `id="vitals-main" data-live="1"`) {
		t.Errorf("a live page should mark <main> data-live=\"1\":\n%s", live)
	}
	if !strings.Contains(live, `id="vitals-refresh-toggle"`) {
		t.Error("a live page should show the auto-refresh toggle in the footer")
	}
	if !strings.Contains(live, "location.pathname") || !strings.Contains(live, "DOMParser") {
		t.Error("the refresh script should be present on a live page")
	}
	// interval is templated in from liveRefreshSeconds, not hardcoded
	// (html/template pads the JS numeric literal, so match loosely)
	if !strings.Contains(live, "PERIOD=") || !strings.Contains(live, "10") || !strings.Contains(live, "*1000") {
		t.Errorf("refresh period should be liveRefreshSeconds*1000ms:\n%s", live)
	}

	static := layoutLive("Clean", "clean", "1.0", nil, "BODY", false)
	if strings.Contains(static, `data-live="1"`) {
		t.Errorf("an interactive page must NOT be marked live:\n%s", static)
	}
	if strings.Contains(static, `id="vitals-refresh-toggle"`) {
		t.Error("a non-live page should not show the toggle")
	}
	// the script tag is still in the document but is inert without data-live —
	// that's fine; it early-returns. We only assert the opt-out marker.
}

func TestRouteMarksReadOnlyPagesLiveAndInteractiveOnesNot(t *testing.T) {
	withRegistry(t, []Module{
		{Slug: "", NavLabel: "Overview", Available: Always, Render: func(PageContext) string { return "OV" }},
		{Slug: "cpu", NavLabel: "CPU", Available: Always, Render: func(PageContext) string { return "CPU" }},
		{Slug: "clean", NavLabel: "Clean", Available: Always, Render: func(PageContext) string { return "CLEAN" }},
		{Slug: "dupes", NavLabel: "Duplicates", Available: Always, Render: func(PageContext) string { return "DUP" }},
	})

	for _, p := range []string{"/", "/cpu"} {
		if _, body := route(p, PageContext{}); !strings.Contains(body, `data-live="1"`) {
			t.Errorf("read-only page %s should auto-refresh", p)
		}
	}
	for _, p := range []string{"/clean", "/dupes"} {
		if _, body := route(p, PageContext{}); strings.Contains(body, `data-live="1"`) {
			t.Errorf("interactive page %s must not auto-refresh", p)
		}
	}
	// 404 is not a live page
	if _, body := route("/nope", PageContext{}); strings.Contains(body, `data-live="1"`) {
		t.Error("the 404 page should not auto-refresh")
	}
}

func TestNoLiveRefreshCoversTheInteractivePages(t *testing.T) {
	for _, slug := range []string{"clean", "dupes"} {
		if !noLiveRefresh[slug] {
			t.Errorf("%q carries interactive client state and must be in noLiveRefresh", slug)
		}
	}
	if noLiveRefresh["cpu"] || noLiveRefresh[""] {
		t.Error("read-only pages must not be in noLiveRefresh")
	}
}
