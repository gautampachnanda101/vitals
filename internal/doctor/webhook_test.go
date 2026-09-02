package doctor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vitals/internal/diag"
)

func TestShouldNotifySkipsHealthyReports(t *testing.T) {
	if shouldNotify(diag.Report{}) {
		t.Error("a healthy (empty) report should not trigger a notification")
	}
}

func TestShouldNotifyFiresOnWarnOrCritical(t *testing.T) {
	var warn diag.Report
	warn.Add(diag.Finding{Severity: diag.Warn, Title: "x"})
	if !shouldNotify(warn) {
		t.Error("a warning report should trigger a notification")
	}

	var crit diag.Report
	crit.Add(diag.Finding{Severity: diag.Critical, Title: "y"})
	if !shouldNotify(crit) {
		t.Error("a critical report should trigger a notification")
	}
}

func TestMaybeNotifySkipsAnEmptyURL(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	var crit diag.Report
	crit.Add(diag.Finding{Severity: diag.Critical, Title: "z"})
	if err := maybeNotify("", Snapshot{}, crit, false); err != nil {
		t.Fatalf("maybeNotify with no URL should be a no-op, got %v", err)
	}
	if called {
		t.Error("no URL configured — the server should never have been called")
	}
}

func TestMaybeNotifySkipsAHealthyReportEvenWithAURLConfigured(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	if err := maybeNotify(srv.URL, Snapshot{}, diag.Report{}, true); err != nil {
		t.Fatalf("maybeNotify on a healthy report should be a no-op, got %v", err)
	}
	if called {
		t.Error("a healthy report should never page anyone")
	}
}

func TestMaybeNotifyPostsTheEnvelopeOnAWarning(t *testing.T) {
	var gotMethod, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var warn diag.Report
	warn.Add(diag.Finding{Severity: diag.Warn, Title: "disk nearly full"})
	snap := Snapshot{CPU: CPU{UsedPct: 10}}

	// httptest.Server is a plain-http loopback address — exactly the case
	// --webhook-allow-insecure exists for (local testing, a local relay).
	if err := maybeNotify(srv.URL, snap, warn, true); err != nil {
		t.Fatalf("maybeNotify: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	var generic map[string]any
	if err := json.Unmarshal(gotBody, &generic); err != nil {
		t.Fatalf("posted body is not valid JSON: %v", err)
	}
	if generic["verdict"] != "warning" {
		t.Errorf("posted body verdict = %v, want %q", generic["verdict"], "warning")
	}
	if !strings.Contains(string(gotBody), "disk nearly full") {
		t.Errorf("posted body missing the finding title: %s", gotBody)
	}
}

func TestMaybeNotifyReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var warn diag.Report
	warn.Add(diag.Finding{Severity: diag.Warn, Title: "x"})
	if err := maybeNotify(srv.URL, Snapshot{}, warn, true); err == nil {
		t.Error("expected an error when the webhook endpoint returns 500")
	}
}

func TestMaybeNotifyRejectsUnknownSchemesEvenWithAllowInsecure(t *testing.T) {
	var warn diag.Report
	warn.Add(diag.Finding{Severity: diag.Warn, Title: "x"})
	for _, u := range []string{"ftp://example.com/hook", "file:///etc/passwd", "javascript:alert(1)"} {
		if err := maybeNotify(u, Snapshot{}, warn, true); err == nil {
			t.Errorf("maybeNotify(%q) should reject a non-HTTP(S) scheme", u)
		}
	}
}

func TestMaybeNotifyRejectsPlainHTTPWithoutAllowInsecure(t *testing.T) {
	// Plain http:// to an arbitrary host is exactly how an SSRF payload
	// against something like the cloud metadata endpoint looks. Refuse it
	// by default; --webhook-allow-insecure is the explicit opt-in.
	var warn diag.Report
	warn.Add(diag.Finding{Severity: diag.Warn, Title: "x"})
	err := maybeNotify("http://example.com/hook", Snapshot{}, warn, false)
	if err == nil {
		t.Fatal("expected plain http:// to be rejected without --webhook-allow-insecure")
	}
	if !strings.Contains(err.Error(), "allow-insecure") {
		t.Errorf("error should point at --webhook-allow-insecure, got: %v", err)
	}
}

func TestMaybeNotifyRejectsLoopbackAndPrivateAddressesWithoutAllowInsecure(t *testing.T) {
	// The receiving server binds loopback, which is exactly what SSRF
	// against 127.0.0.1 or an internal service looks like from vitals'
	// side — refusing it must happen before any request reaches the
	// handler, not just be reported as a failure after the fact.
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	var warn diag.Report
	warn.Add(diag.Finding{Severity: diag.Warn, Title: "x"})
	if err := maybeNotify(srv.URL, Snapshot{}, warn, false); err == nil {
		t.Error("expected a loopback target to be rejected without --webhook-allow-insecure")
	}
	if called {
		t.Error("the handler should never have been reached — the dial itself must be refused")
	}
}

func TestMaybeNotifyRejectsCloudMetadataAddressWithoutAllowInsecure(t *testing.T) {
	// 169.254.169.254 is the AWS/GCP/Azure/DigitalOcean instance-metadata
	// address — the single most common concrete SSRF payload against a
	// tool that POSTs to an operator-supplied URL. It must never be
	// reachable via --webhook by default, even over https.
	var warn diag.Report
	warn.Add(diag.Finding{Severity: diag.Warn, Title: "x"})
	if err := maybeNotify("https://169.254.169.254/latest/meta-data/", Snapshot{}, warn, false); err == nil {
		t.Error("expected the cloud metadata address to be rejected without --webhook-allow-insecure")
	}
}
