package doctor

import (
	"context"
	"fmt"
	"net"
	"time"

	"vitals/internal/diag"
)

// dnsSlowThreshold is well above a healthy lookup (typically single-digit to
// low double-digit milliseconds against any real resolver) but well below
// where a person would already suspect DNS specifically, rather than "the
// internet" generally.
const dnsSlowThreshold = 500 * time.Millisecond

// dnsProbeHost is resolved to measure resolver latency. A hostname, not a
// literal IP, so the resolver actually does DNS work rather than returning
// instantly from its own loopback shortcut.
const dnsProbeHost = "www.google.com"

// checkDNSLatency resolves dnsProbeHost and times it — separating "DNS is
// slow" from "my link is slow", which raw throughput numbers can't do on
// their own, without needing the raw-socket privileges an ICMP ping would.
func checkDNSLatency(timeout time.Duration) (time.Duration, error) {
	return checkDNSLatencyWith(net.DefaultResolver.LookupHost, timeout)
}

// checkDNSLatencyWith is checkDNSLatency's testable core: lookupHost is
// injected (net.DefaultResolver.LookupHost in production) so a test can
// substitute a fake with a controlled delay or error, instead of a real
// network lookup.
func checkDNSLatencyWith(lookupHost func(ctx context.Context, host string) ([]string, error), timeout time.Duration) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	_, err := lookupHost(ctx, dnsProbeHost)
	return time.Since(start), err
}

// analyzeDNSLatency turns a DNS timing result into a finding, or nil when
// resolution was fast and successful. Pure, so it's testable without a real
// network lookup.
func analyzeDNSLatency(d time.Duration, err error) *diag.Finding {
	if err != nil {
		return &diag.Finding{
			Severity: diag.Warn,
			Title:    "DNS resolution is failing",
			Detail:   fmt.Sprintf("could not resolve %s: %v — name lookups, not raw connectivity, are the problem", dnsProbeHost, err),
			Fixes:    []string{"try a public resolver (1.1.1.1 / 8.8.8.8)", "check the VPN/DNS settings if one is active"},
		}
	}
	if d >= dnsSlowThreshold {
		return &diag.Finding{
			Severity: diag.Warn,
			Title:    "DNS resolution is slow",
			Detail:   fmt.Sprintf("resolving %s took %s — pages/connections will feel slow to start even on a fast link", dnsProbeHost, d.Round(time.Millisecond)),
			Fixes:    []string{"try a public resolver (1.1.1.1 / 8.8.8.8)", "restart the router if this is new"},
		}
	}
	return nil
}
