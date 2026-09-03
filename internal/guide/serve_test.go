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
