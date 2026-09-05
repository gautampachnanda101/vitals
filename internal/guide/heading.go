package guide

import "strings"

// headingLevel reports the Markdown heading level (1-3) of trimmed and its
// text with the "#" prefix stripped, or ok=false when trimmed isn't a
// heading line at all. The single place both RenderTerminal and
// RenderHTML/RenderFragment ask "is this line a heading, and what level" —
// sharing this, rather than each hand-writing its own "### "/"## "/"# "
// prefix chain, is what makes headingEmphasis below actually load-bearing:
// a level either two functions agree exists (this one) and agree on how to
// show (headingEmphasis), not two independent judgment calls that can
// quietly drift, which is exactly what happened before this file existed —
// "###" rendered bold-only (no color) in the terminal while "##"/"#" both
// got color, an inconsistency nothing had enforced across two hand-written
// switches.
func headingLevel(trimmed string) (level int, text string, ok bool) {
	switch {
	case strings.HasPrefix(trimmed, "### "):
		return 3, strings.TrimPrefix(trimmed, "### "), true
	case strings.HasPrefix(trimmed, "## "):
		return 2, strings.TrimPrefix(trimmed, "## "), true
	case strings.HasPrefix(trimmed, "# "):
		return 1, strings.TrimPrefix(trimmed, "# "), true
	default:
		return 0, "", false
	}
}

// headingEmphasis is the single source of truth for how much visual weight
// a heading level gets — bold, colored (vitals' accent color, whatever
// form that takes in the current medium: ui.Cyan in a terminal, the
// dashboard's --accent/guide's #2b5d53 in HTML), and, for the top level
// only, an underline rule beneath the title. Both renderers in this
// package look a level up here instead of hardcoding it, so adding a
// level, or changing which levels get emphasis, is a one-place edit that
// both outputs pick up automatically — never again two independently
// hand-tuned per-level cases that can disagree.
type headingEmphasisSpec struct {
	Bold  bool
	Color bool
	Rule  bool // an underline rule below the title — level 1 only
}

func headingEmphasis(level int) headingEmphasisSpec {
	if level == 1 {
		return headingEmphasisSpec{Bold: true, Color: true, Rule: true}
	}
	// Every other level (2, 3, and any future deeper level this package's
	// Markdown subset grows to support) shares one treatment: bold and
	// colored, no rule. Deeper visual hierarchy than that (e.g. a level 3
	// that's colored but not bold) isn't a distinction this package's
	// content has ever needed — add one here, deliberately, if it does.
	return headingEmphasisSpec{Bold: true, Color: true}
}
