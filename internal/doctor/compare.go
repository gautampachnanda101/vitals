package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"vitals/internal/diag"
)

// LoadJSONEnvelope reads and parses a --output-saved JSON envelope — the
// counterpart to WriteJSONReport, used by --compare to load two prior runs.
func LoadJSONEnvelope(path string) (JSONEnvelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return JSONEnvelope{}, err
	}
	var env JSONEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return JSONEnvelope{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return env, nil
}

// CompareResult is what changed between two saved reports.
type CompareResult struct {
	OldVerdict string
	NewVerdict string
	Appeared   []string // finding titles present only in the newer report
	Resolved   []string // finding titles present only in the older report
}

// compareEnvelopes diffs two envelopes by finding title — the natural
// question before/after a deploy or a suspected regression: what's new,
// what went away, and did the overall verdict change.
func compareEnvelopes(old, newer JSONEnvelope) CompareResult {
	oldTitles := titleSet(old.Findings)
	newTitles := titleSet(newer.Findings)

	var appeared, resolved []string
	for t := range newTitles {
		if !oldTitles[t] {
			appeared = append(appeared, t)
		}
	}
	for t := range oldTitles {
		if !newTitles[t] {
			resolved = append(resolved, t)
		}
	}
	sort.Strings(appeared)
	sort.Strings(resolved)

	return CompareResult{OldVerdict: old.Verdict, NewVerdict: newer.Verdict, Appeared: appeared, Resolved: resolved}
}

func titleSet(findings []diag.Finding) map[string]bool {
	m := make(map[string]bool, len(findings))
	for _, f := range findings {
		m[f.Title] = true
	}
	return m
}

// RenderCompare diffs two saved envelopes and formats the result as
// human-readable text — the `vitals doctor --compare old.json new.json` path.
func RenderCompare(old, newer JSONEnvelope) string {
	return renderCompare(compareEnvelopes(old, newer))
}

// renderCompare formats a CompareResult as human-readable text.
func renderCompare(c CompareResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "verdict: %s -> %s\n", c.OldVerdict, c.NewVerdict)
	if len(c.Appeared) == 0 && len(c.Resolved) == 0 {
		b.WriteString("no findings changed\n")
		return b.String()
	}
	for _, t := range c.Appeared {
		fmt.Fprintf(&b, "  + %s\n", t)
	}
	for _, t := range c.Resolved {
		fmt.Fprintf(&b, "  - %s\n", t)
	}
	return b.String()
}
