package guide

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowedHostsOnlyPassesThroughAnAllowedHost(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := allowedHostsOnly(inner, "127.0.0.1:9999", "localhost:9999")

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9999/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !called {
		t.Error("request with an allowed Host header should reach the wrapped handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (httptest.NewRecorder defaults to 200 if unset by the handler)", rec.Code)
	}
}

func TestAllowedHostsOnlyAcceptsLocalhostAlias(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := allowedHostsOnly(inner, "127.0.0.1:9999", "localhost:9999")

	req := httptest.NewRequest(http.MethodGet, "http://localhost:9999/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !called {
		t.Error("the localhost alias for the same port should also be allowed")
	}
}

func TestAllowedHostsOnlyRejectsADifferentHost(t *testing.T) {
	// This is the actual DNS-rebinding defense: an external page rebinds
	// its own origin's DNS to 127.0.0.1, then fetch()es this server — the
	// request still arrives with the ATTACKER's original hostname in the
	// Host header (not 127.0.0.1), which is exactly what this must catch.
	// Binding to 127.0.0.1 alone does nothing to stop this; net/http does
	// not validate Host by default.
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := allowedHostsOnly(inner, "127.0.0.1:9999", "localhost:9999")

	req := httptest.NewRequest(http.MethodGet, "http://evil.example:9999/", nil)
	req.Host = "evil.example:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if called {
		t.Error("a request with an unrecognized Host header reached the wrapped handler — DNS rebinding is not blocked")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAllowedHostsOnlyRejectsTheRightPortWrongHost(t *testing.T) {
	// Same port, different host name — still must be rejected. A naive
	// "does the port match" check would wrongly let this through.
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := allowedHostsOnly(inner, "127.0.0.1:9999", "localhost:9999")

	req := httptest.NewRequest(http.MethodGet, "http://attacker.test:9999/", nil)
	req.Host = "attacker.test:9999"
	h.ServeHTTP(httptest.NewRecorder(), req)

	if called {
		t.Error("matching port with a different host must still be rejected")
	}
}

// callSameOriginOnly builds a sameOriginOnly handler with the same
// allowed-hosts set allowedHostsOnly's own tests use, and runs one
// request through it, returning whether the inner handler was reached.
func callSameOriginOnly(t *testing.T, method, origin, secFetchSite string) (called bool, status int) {
	t.Helper()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := sameOriginOnly(inner, "127.0.0.1:9999", "localhost:9999")

	req := httptest.NewRequest(method, "http://127.0.0.1:9999/clean/apply", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if secFetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", secFetchSite)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return called, rec.Code
}

func TestSameOriginOnlyIgnoresGETAndHEADEntirely(t *testing.T) {
	// Read-only routes are already covered by allowedHostsOnly alone —
	// sameOriginOnly only guards mutating requests, so a cross-origin GET
	// (which allowedHostsOnly's own Host check already handles) must not
	// be rejected here even with attacker-shaped headers.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		called, status := callSameOriginOnly(t, method, "https://evil.example", "cross-site")
		if !called || status != http.StatusOK {
			t.Errorf("%s: called=%v status=%d, want called=true status=200 — sameOriginOnly must not touch GET/HEAD", method, called, status)
		}
	}
}

func TestSameOriginOnlyAcceptsAMatchingOriginOnEitherAllowedHost(t *testing.T) {
	for _, origin := range []string{"http://127.0.0.1:9999", "http://localhost:9999"} {
		called, status := callSameOriginOnly(t, http.MethodPost, origin, "same-origin")
		if !called || status != http.StatusOK {
			t.Errorf("origin %q: called=%v status=%d, want called=true status=200 — this is the exact bug found by review (only 127.0.0.1 was checked, not localhost)", origin, called, status)
		}
	}
}

func TestSameOriginOnlyRejectsAMismatchedOrigin(t *testing.T) {
	called, status := callSameOriginOnly(t, http.MethodPost, "https://evil.example", "")
	if called {
		t.Error("a mismatched Origin reached the wrapped handler")
	}
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
}

func TestSameOriginOnlyRejectsCrossSiteSecFetchSiteEvenWithNoOrigin(t *testing.T) {
	// Sec-Fetch-Site is browser-controlled and cannot be forged by page
	// script — reject on it alone even if Origin happens to be absent.
	called, _ := callSameOriginOnly(t, http.MethodPost, "", "cross-site")
	if called {
		t.Error("Sec-Fetch-Site: cross-site reached the wrapped handler")
	}
}

func TestSameOriginOnlyAllowsSecFetchSiteNone(t *testing.T) {
	// "none" means the browser has no initiating document at all (typed
	// URL, bookmark) — not something a hostile page can produce.
	called, status := callSameOriginOnly(t, http.MethodPost, "", "none")
	if !called || status != http.StatusOK {
		t.Errorf("called=%v status=%d, want called=true status=200 for Sec-Fetch-Site: none", called, status)
	}
}

func TestSameOriginOnlyAllowsBothHeadersAbsent(t *testing.T) {
	// A non-browser same-machine caller (e.g. curl) sends neither header
	// — already the same privilege tier as running `vitals clean`
	// directly, per this design's own threat model (see
	// docs/roadmap/items/005-dashboard-write-actions/design.md §1).
	called, status := callSameOriginOnly(t, http.MethodPost, "", "")
	if !called || status != http.StatusOK {
		t.Errorf("called=%v status=%d, want called=true status=200 when both headers are absent", called, status)
	}
}
