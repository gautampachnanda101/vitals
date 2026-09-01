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
	if err := maybeNotify("", Snapshot{}, crit); err != nil {
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

	if err := maybeNotify(srv.URL, Snapshot{}, diag.Report{}); err != nil {
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

	if err := maybeNotify(srv.URL, snap, warn); err != nil {
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
	if err := maybeNotify(srv.URL, Snapshot{}, warn); err == nil {
		t.Error("expected an error when the webhook endpoint returns 500")
	}
}
