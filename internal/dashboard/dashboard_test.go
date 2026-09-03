package dashboard

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestLoopbackAddrForcesLoopbackHost(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty keeps the ephemeral default", "", "", false},
		{"host:port keeps only the port", "0.0.0.0:8080", "127.0.0.1:8080", false},
		{"bare :port keeps the port", ":9100", "127.0.0.1:9100", false},
		{"already loopback stays loopback", "127.0.0.1:5000", "127.0.0.1:5000", false},
		{"malformed addr errors", "not-an-addr", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := loopbackAddr(c.in)
			if c.wantErr {
				if err == nil {
					t.Fatalf("loopbackAddr(%q) = %q, nil; want an error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("loopbackAddr(%q) unexpected error: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("loopbackAddr(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRouteRendersTheOverviewAtRoot(t *testing.T) {
	withRegistry(t, []Module{
		{Slug: "", NavLabel: "Overview", Available: Always, Render: func(PageContext) string { return "OVERVIEW-BODY" }},
	})
	status, body := route("/", PageContext{})
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if !strings.Contains(body, "OVERVIEW-BODY") {
		t.Errorf("body missing rendered content: %s", body)
	}
	if !strings.Contains(body, `aria-current="page"`) {
		t.Errorf("overview nav link should be marked current: %s", body)
	}
}

func TestRouteReturns404ForAnUnknownSlug(t *testing.T) {
	withRegistry(t, []Module{{Slug: "cpu", NavLabel: "CPU", Available: Always, Render: func(PageContext) string { return "" }}})
	status, body := route("/nope", PageContext{})
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	if !strings.Contains(body, "not found") {
		t.Errorf("body should say not found: %s", body)
	}
}

func TestRouteRendersUnavailablePageWithItsReasonNotA404(t *testing.T) {
	withRegistry(t, []Module{{
		Slug: "gpu", NavLabel: "GPU", UnavailableReason: "no GPU detected",
		Available: func(PageContext) bool { return false },
		Render:    func(PageContext) string { return "should not be called" },
	}})
	status, body := route("/gpu", PageContext{})
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200 (unavailable is not a 404)", status)
	}
	if !strings.Contains(body, "no GPU detected") {
		t.Errorf("body should include the specific reason: %s", body)
	}
	if strings.Contains(body, "should not be called") {
		t.Error("Render must not be called for an unavailable module")
	}
}

func TestRouteFallsBackToAGenericReasonWhenNoneIsSet(t *testing.T) {
	withRegistry(t, []Module{{
		Slug: "x", NavLabel: "X",
		Available: func(PageContext) bool { return false },
		Render:    func(PageContext) string { return "" },
	}})
	_, body := route("/x", PageContext{})
	if !strings.Contains(body, "not available") {
		t.Errorf("body should carry a generic fallback reason: %s", body)
	}
}

func TestRouteCallsPrepareBeforeRender(t *testing.T) {
	withRegistry(t, []Module{{
		Slug: "advice", NavLabel: "Advice", Available: Always,
		Prepare: func(ctx *PageContext) error { ctx.AdviceReply = "prepared"; return nil },
		Render:  func(ctx PageContext) string { return "reply=" + ctx.AdviceReply },
	}})
	_, body := route("/advice", PageContext{})
	if !strings.Contains(body, "reply=prepared") {
		t.Errorf("Render should see what Prepare set on ctx: %s", body)
	}
}

func TestRoutePrepareErrorRendersAnErrorPageInsteadOfPanicking(t *testing.T) {
	withRegistry(t, []Module{{
		Slug: "x", NavLabel: "X", Available: Always,
		Prepare: func(*PageContext) error { return errors.New("boom") },
		Render:  func(PageContext) string { return "should not render" },
	}})
	status, body := route("/x", PageContext{})
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
	if !strings.Contains(body, "boom") {
		t.Errorf("body should surface the Prepare error: %s", body)
	}
}

// withWriteRegistry swaps the package-level write-action registry for the
// duration of a test, mirroring withRegistry's swap-and-restore of the
// read-only Module registry (module_test.go).
func withWriteRegistry(t *testing.T, actions []WriteAction) {
	t.Helper()
	old := writeActions
	writeActions = append([]WriteAction(nil), actions...)
	t.Cleanup(func() { writeActions = old })
}

func TestRegisterWritePanicsOnDuplicatePath(t *testing.T) {
	withWriteRegistry(t, nil)
	RegisterWrite(WriteAction{Path: "/clean/apply"})

	defer func() {
		if recover() == nil {
			t.Error("RegisterWrite should panic on a duplicate path, it did not")
		}
	}()
	RegisterWrite(WriteAction{Path: "/clean/apply"})
}

func TestRouteWriteReturns404ForAnUnregisteredPath(t *testing.T) {
	withWriteRegistry(t, nil)
	status, _ := routeWrite("/nope", nil, PageContext{})
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestRouteWriteReturns404WhenUnavailable(t *testing.T) {
	// 404, not some other status: an unavailable write action shouldn't
	// leak whether the path exists at all — same posture as a module a
	// machine doesn't offer being indistinguishable from one that was
	// never registered, from a strict security standpoint, though route
	// (the read side) is more forgiving since it explains why. Write
	// actions have no page to explain on, so 404 is simplest and safest.
	withWriteRegistry(t, []WriteAction{{
		Path:      "/gpu-only-action",
		Available: func(PageContext) bool { return false },
		Handler:   func(PageContext, []byte) (int, string) { return http.StatusOK, "should not run" },
	}})
	status, body := routeWrite("/gpu-only-action", nil, PageContext{})
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	if strings.Contains(body, "should not run") {
		t.Error("Handler must not be called when Available returns false")
	}
}

func TestRouteWriteNilAvailableMeansAlwaysAvailable(t *testing.T) {
	withWriteRegistry(t, []WriteAction{{
		Path:    "/clean/preview",
		Handler: func(PageContext, []byte) (int, string) { return http.StatusOK, "ran" },
	}})
	status, body := routeWrite("/clean/preview", nil, PageContext{})
	if status != http.StatusOK || body != "ran" {
		t.Errorf("status=%d body=%q, want 200/\"ran\" — nil Available should mean always-available", status, body)
	}
}

func TestRouteWriteCallsHandlerWithTheRawBody(t *testing.T) {
	var gotBody []byte
	withWriteRegistry(t, []WriteAction{{
		Path: "/clean/apply",
		Handler: func(_ PageContext, body []byte) (int, string) {
			gotBody = body
			return http.StatusOK, "applied"
		},
	}})
	status, body := routeWrite("/clean/apply", []byte(`{"confirm":true}`), PageContext{})
	if status != http.StatusOK || body != "applied" {
		t.Errorf("status=%d body=%q, want 200/\"applied\"", status, body)
	}
	if string(gotBody) != `{"confirm":true}` {
		t.Errorf("Handler received body %q, want the raw request body verbatim", gotBody)
	}
}
