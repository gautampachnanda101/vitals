package metrics

import "testing"

func TestResolveAddrDefaultsToLoopbackOnly(t *testing.T) {
	// An empty Addr must never resolve to an all-interfaces bind — a metrics
	// server that ships as a "just run it" static binary shouldn't open a
	// port beyond the local machine unless the operator explicitly asks.
	got := resolveAddr("")
	if got != "127.0.0.1:9100" {
		t.Errorf("resolveAddr(\"\") = %q, want a loopback-only default", got)
	}
}

func TestResolveAddrHonorsExplicitOverride(t *testing.T) {
	// A bare ":PORT" is exactly how you'd ask Prometheus-style tools to bind
	// every interface — that has to stay reachable for anyone who wants it,
	// just never be the default.
	for _, addr := range []string{":9100", "0.0.0.0:9100", "127.0.0.1:9200", "10.0.0.5:9100"} {
		if got := resolveAddr(addr); got != addr {
			t.Errorf("resolveAddr(%q) = %q, want it passed through unchanged", addr, got)
		}
	}
}
