package llm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/mem"

	"vitals/internal/gpu"
	"vitals/internal/ui"
)

// quant is one quantisation level and its effective bytes-per-weight, including
// the per-block scales/zeros that llama.cpp stores alongside the weights.
type quant struct {
	name string
	bpw  float64
}

// Ordered largest (highest quality) to smallest.
var quants = []quant{
	{"F16", 2.00},
	{"Q8_0", 1.09},
	{"Q6_K", 0.82},
	{"Q5_K_M", 0.69},
	{"Q4_K_M", 0.60},
	{"Q4_0", 0.56},
	{"Q3_K_M", 0.49},
	{"Q2_K", 0.40},
}

// kvOverhead pads the raw weight bytes for the KV cache and activations at a
// typical few-thousand-token context. It is a heuristic, not exact.
const kvOverhead = 1.12

var paramSizeRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*b\b`)

// namedSizes maps common non-numeric size labels to a parameter count (billions).
var namedSizes = map[string]float64{
	"mini": 3.8, "small": 7, "medium": 14, "large": 34, "xl": 70,
	"tiny": 1.1, "nano": 0.5,
}

// paramsFromName extracts a parameter count in billions from an Ollama-style
// model name ("qwen2.5:32b" -> 32, "phi3:mini" -> 3.8).
func paramsFromName(name string) (float64, bool) {
	if m := paramSizeRe.FindStringSubmatch(name); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
			return v, true
		}
	}
	lower := strings.ToLower(name)
	for label, v := range namedSizes {
		if strings.Contains(lower, label) {
			return v, true
		}
	}
	return 0, false
}

// QuantFit is one quant option sized against a VRAM budget.
type QuantFit struct {
	Quant     string  `json:"quant"`
	Bytes     int64   `json:"bytes"`
	Fits      bool    `json:"fits"`
	HeadroomB int64   `json:"headroom_bytes"` // vram - bytes; negative means it spills
	SpillPct  float64 `json:"spill_percent"`  // 0 when it fits
}

// quantBytes estimates the on-device footprint of a model at a quant level.
func quantBytes(paramsB, bpw float64) int64 {
	return int64(paramsB * 1e9 * bpw * kvOverhead)
}

// fitAll sizes every quant against vramBytes, largest quality first.
func fitAll(paramsB float64, vramBytes int64) []QuantFit {
	out := make([]QuantFit, 0, len(quants))
	for _, q := range quants {
		b := quantBytes(paramsB, q.bpw)
		f := QuantFit{Quant: q.name, Bytes: b, HeadroomB: vramBytes - b}
		f.Fits = f.HeadroomB >= 0
		if !f.Fits && b > 0 {
			f.SpillPct = float64(b-vramBytes) / float64(b) * 100
		}
		out = append(out, f)
	}
	return out
}

// recommend returns the name of the largest quant that fully fits, or "" if none.
func recommend(fits []QuantFit) string {
	for _, f := range fits { // fits is ordered largest-first
		if f.Fits {
			return f.Quant
		}
	}
	return ""
}

// vramBudget returns the VRAM available for a model and a human label for it.
// On a discrete GPU that is the card's VRAM; on Apple silicon it is the portion
// of unified memory the GPU may use (~75% by default).
func vramBudget() (int64, string) {
	for _, d := range gpu.Probe() {
		if d.MemTotalB > 0 {
			return int64(d.MemTotalB), fmt.Sprintf("%s, %s VRAM", d.Name, ui.HumanBytes(int64(d.MemTotalB)))
		}
	}
	if vm, err := mem.VirtualMemory(); err == nil && vm.Total > 0 {
		budget := int64(float64(vm.Total) * 0.75)
		return budget, fmt.Sprintf("Apple unified memory, ~%s usable by the GPU", ui.HumanBytes(budget))
	}
	return 0, "unknown"
}

// RunFit implements `vitals llm fit <model>`.
func RunFit(model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("usage: vitals llm fit <model>   (e.g. vitals llm fit qwen2.5:32b)")
	}
	paramsB, ok := paramsFromName(model)
	if !ok {
		return fmt.Errorf("could not infer a parameter count from %q — include the size, e.g. %q", model, model+":8b")
	}
	vram, label := vramBudget()
	if vram <= 0 {
		return fmt.Errorf("could not determine available VRAM")
	}

	fits := fitAll(paramsB, vram)
	best := recommend(fits)

	ui.Header("LLM FIT")
	fmt.Printf("  model   : %s  (~%.0fB parameters)\n", model, paramsB)
	fmt.Printf("  budget  : %s\n\n", label)
	fmt.Printf("  %-8s %-12s %s\n", "QUANT", "EST. SIZE", "FITS FULLY?")
	ui.Rule()
	for _, f := range fits {
		mark := ""
		var verdict string
		switch {
		case f.Fits && f.Quant == best:
			verdict = ui.Green + "YES" + ui.Reset
			mark = "  ← recommended"
		case f.Fits:
			verdict = "yes"
		case f.SpillPct > 0:
			verdict = ui.Actionf("no (spills ~%.0f%%)", f.SpillPct)
		default:
			verdict = ui.Actionf("no")
		}
		fmt.Printf("  %-8s %-12s %s%s\n", f.Quant, ui.HumanBytes(f.Bytes), verdict, mark)
	}
	fmt.Println()
	if best == "" {
		ui.Warnf("no quant of %s fits this GPU fully — even Q2_K spills. Use a smaller model or add VRAM.", model)
		return nil
	}
	headroom := int64(0)
	for _, f := range fits {
		if f.Quant == best {
			headroom = f.HeadroomB
		}
	}
	ui.Okf("pull %s at %s — fits with %s headroom", model, strings.ToLower(best), ui.HumanBytes(headroom))
	return nil
}
