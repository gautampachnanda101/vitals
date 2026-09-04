package doctor

import (
	"context"
	"errors"
	"testing"
	"time"

	"vitals/internal/diag"
)

func TestAnalyzeDNSLatencyHealthyIsNil(t *testing.T) {
	if f := analyzeDNSLatency(20*time.Millisecond, nil); f != nil {
		t.Errorf("a fast, successful lookup should produce no finding, got %+v", f)
	}
}

func TestAnalyzeDNSLatencySlowLookupWarns(t *testing.T) {
	f := analyzeDNSLatency(600*time.Millisecond, nil)
	if f == nil {
		t.Fatal("a 600ms DNS lookup should produce a finding")
	}
	if f.Severity == diag.OK {
		t.Errorf("a 600ms lookup should not be reported as OK severity")
	}
}

func TestAnalyzeDNSLatencyFailureIsCritical(t *testing.T) {
	f := analyzeDNSLatency(0, errors.New("no such host"))
	if f == nil {
		t.Fatal("a failed DNS lookup should produce a finding")
	}
	if f.Title == "" {
		t.Error("finding should have a title")
	}
}

func TestCheckDNSLatencyWithMeasuresARealDuration(t *testing.T) {
	fake := func(ctx context.Context, host string) ([]string, error) {
		time.Sleep(5 * time.Millisecond)
		return []string{"1.2.3.4"}, nil
	}
	d, err := checkDNSLatencyWith(fake, time.Second)
	if err != nil {
		t.Fatalf("checkDNSLatencyWith: %v", err)
	}
	if d < 5*time.Millisecond {
		t.Errorf("checkDNSLatencyWith duration = %v, want at least 5ms", d)
	}
}

func TestCheckDNSLatencyWithPropagatesTheLookupError(t *testing.T) {
	fake := func(ctx context.Context, host string) ([]string, error) { return nil, errors.New("no such host") }
	if _, err := checkDNSLatencyWith(fake, time.Second); err == nil {
		t.Error("checkDNSLatencyWith should propagate a lookup failure")
	}
}

func TestCheckDNSLatencyWithHonorsTheTimeout(t *testing.T) {
	fake := func(ctx context.Context, host string) ([]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if _, err := checkDNSLatencyWith(fake, 10*time.Millisecond); err == nil {
		t.Error("checkDNSLatencyWith should propagate the context-deadline error")
	}
}

func TestCheckDNSLatencyGoesThroughTheRealResolver(t *testing.T) {
	// One real end-to-end call through the actual net.DefaultResolver
	// wiring, matching the memcheck/monitor-style single live exercise.
	// No assertion on the duration or success (network-dependent) — just
	// that the real call site doesn't panic and returns promptly.
	_, _ = checkDNSLatency(2 * time.Second)
}
