package llm

import (
	"strings"
	"testing"
)

func TestParamsFromName(t *testing.T) {
	cases := map[string]float64{
		"qwen2.5:32b":        32,
		"llama3.1:8b":        8,
		"llama3.1:70b":       70,
		"deepseek-r1:1.5b":   1.5,
		"mistral-large:123b": 123,
		"phi3:mini":          3.8,
		"gemma:tiny":         1.1,
	}
	for name, want := range cases {
		got, ok := paramsFromName(name)
		if !ok || got != want {
			t.Errorf("paramsFromName(%q) = %v, %v; want %v", name, got, ok, want)
		}
	}
	if _, ok := paramsFromName("some-model-no-size"); ok {
		t.Error("expected no param count for a name without a size")
	}
}

func TestQuantBytesOrdering(t *testing.T) {
	// F16 must be the largest, Q2_K the smallest, monotonically decreasing.
	prev := int64(1 << 62)
	for _, q := range quants {
		b := quantBytes(8, q.bpw)
		if b <= 0 {
			t.Fatalf("quant %s produced non-positive size", q.name)
		}
		if b >= prev {
			t.Errorf("quant %s (%d) not smaller than the previous (%d)", q.name, b, prev)
		}
		prev = b
	}
}

func TestFitAll(t *testing.T) {
	// 32B model against a 24 GB card.
	const vram = int64(24) << 30
	fits := fitAll(32, vram)
	if len(fits) != len(quants) {
		t.Fatalf("want %d rows, got %d", len(quants), len(fits))
	}

	byName := map[string]QuantFit{}
	for _, f := range fits {
		byName[f.Quant] = f
	}
	if byName["F16"].Fits {
		t.Error("F16 of a 32B model must not fit in 24 GB")
	}
	if !byName["Q2_K"].Fits {
		t.Errorf("Q2_K of a 32B model should fit in 24 GB (size %d)", byName["Q2_K"].Bytes)
	}
	if f := byName["F16"]; f.SpillPct <= 0 || f.HeadroomB >= 0 {
		t.Errorf("F16 should report a spill and negative headroom: %+v", f)
	}

	// recommend picks the largest that fits and it must actually fit.
	rec := recommend(fits)
	if rec == "" || !byName[rec].Fits {
		t.Fatalf("recommend returned %q which does not fit", rec)
	}
	// Nothing larger than the recommendation may fit.
	for _, f := range fits {
		if f.Quant == rec {
			break
		}
		if f.Fits {
			t.Errorf("%s fits but is larger than the recommended %s", f.Quant, rec)
		}
	}
}

func TestFitAllTinyModelEverythingFits(t *testing.T) {
	fits := fitAll(1, int64(24)<<30)
	for _, f := range fits {
		if !f.Fits {
			t.Errorf("1B model: %s should fit in 24 GB", f.Quant)
		}
	}
	if recommend(fits) != "F16" {
		t.Errorf("1B model in 24 GB should recommend F16, got %q", recommend(fits))
	}
}

func TestFitAllHugeModelNothingFits(t *testing.T) {
	fits := fitAll(671, int64(24)<<30)
	if recommend(fits) != "" {
		t.Errorf("671B model must not fit a 24 GB card, got %q", recommend(fits))
	}
}

func TestRunFitRejectsAnEmptyModelName(t *testing.T) {
	if err := RunFit("  "); err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Errorf("RunFit(empty) = %v, want a usage error", err)
	}
}

func TestRunFitRejectsAModelWithNoInferableSize(t *testing.T) {
	if err := RunFit("mystery-model"); err == nil || !strings.Contains(err.Error(), "could not infer") {
		t.Errorf("RunFit(no size in name) = %v, want a could-not-infer error", err)
	}
}

func TestRunFitGoesThroughTheRealVRAMBudgetEndToEnd(t *testing.T) {
	// One real end-to-end call through vramBudget()'s actual gpu.Probe/
	// mem.VirtualMemory wiring — those are already ~100% tested in their
	// own packages, so this proves RunFit's own orchestration (paramsFromName
	// -> vramBudget -> fitAll -> recommend -> print) completes without
	// error on a real machine, not any specific fit result.
	out := captureStdout(t, func() {
		if err := RunFit("qwen2.5:7b"); err != nil {
			t.Fatalf("RunFit: %v", err)
		}
	})
	if !strings.Contains(out, "LLM FIT") {
		t.Errorf("RunFit produced no report, got:\n%s", out)
	}
}
