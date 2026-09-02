package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"vitals/internal/diag"
)

const webhookTimeout = 5 * time.Second

// shouldNotify reports whether a report is worth paging someone about — a
// healthy run has nothing to say and would just be noise.
func shouldNotify(r diag.Report) bool {
	return r.Worst() != diag.OK
}

// validateWebhookURL rejects anything that isn't a plain HTTP(S) request to
// an explicit target. --webhook always carries a URL the invoking user (or
// whatever automation calls vitals on their behalf) supplies directly —
// there is no scheme/host validation upstream of this, so it has to happen
// here. allowInsecure permits plain http:// for local testing/relays;
// https is otherwise required, since the classic SSRF payload against a
// tool like this (a POST to a cloud metadata endpoint) is always http.
func validateWebhookURL(rawURL string, allowInsecure bool) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid --webhook URL: %w", err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowInsecure {
			return nil, fmt.Errorf("--webhook %q uses plain http — pass --webhook-allow-insecure to allow it (e.g. for a local relay)", rawURL)
		}
	default:
		return nil, fmt.Errorf("--webhook %q must be http or https, got scheme %q", rawURL, u.Scheme)
	}
	return u, nil
}

// isDisallowedWebhookIP reports whether ip is a loopback, private, or
// link-local address — including 169.254.169.254, the AWS/GCP/Azure/
// DigitalOcean instance-metadata address and the single most common
// concrete SSRF target for a tool that POSTs to an operator-supplied URL.
func isDisallowedWebhookIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// webhookDialContext blocks connections to loopback/private/link-local
// addresses unless allowInsecure is set. It checks the address net/http is
// actually about to dial — after DNS resolution — so a public hostname
// that resolves to a private IP (DNS rebinding) is caught too, not just a
// literal private IP typed into --webhook.
func webhookDialContext(allowInsecure bool) func(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: webhookTimeout}
	if allowInsecure {
		return dialer.DialContext
	}
	dialer.Control = func(_, address string, c syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip != nil && isDisallowedWebhookIP(ip) {
			return fmt.Errorf("refusing to send --webhook to %s (loopback/private/link-local) — pass --webhook-allow-insecure to override", ip)
		}
		return nil
	}
	return dialer.DialContext
}

// maybeNotify POSTs the JSON envelope for (s, r) to url as
// application/json, but only when both a URL is configured and the report
// actually needs attention — a webhook that fires on every healthy run
// trains its recipient to ignore it. allowInsecure permits plain http and
// loopback/private/link-local targets; both are refused by default.
func maybeNotify(rawURL string, s Snapshot, r diag.Report, allowInsecure bool) error {
	if rawURL == "" || !shouldNotify(r) {
		return nil
	}
	if _, err := validateWebhookURL(rawURL, allowInsecure); err != nil {
		return err
	}
	body, err := json.Marshal(JSONReport(s, r))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: &http.Transport{DialContext: webhookDialContext(allowInsecure)}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s returned %s", rawURL, resp.Status)
	}
	return nil
}
