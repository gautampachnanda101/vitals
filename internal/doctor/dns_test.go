package doctor

import (
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
