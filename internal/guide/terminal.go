// Package guide renders vitals' own embedded USERGUIDE.md — for a terminal
// (`vitals guide`) and for a browser (`vitals guide --web`). Both renderers
// are hand-written against exactly the Markdown subset the guide actually
// uses (headers, bold, inline code, fenced blocks, bullets, one link style)
// rather than a general CommonMark implementation: vitals is the only thing
// that ever writes this file, so a full parser would just be a second
// dependency (after gopsutil) bought for syntax nothing here uses.
package guide

import (
	"regexp"
	"strings"

	"vitals/internal/ui"
)

var (
	reLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	reCode = regexp.MustCompile("`([^`]+)`")
	reBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
)

// RenderTerminal converts md into ANSI-styled text. Color codes come from
// the shared ui package, so --no-color / NO_COLOR / a non-TTY stdout
// disable styling exactly the same way they already do for every other
// vitals command.
func RenderTerminal(md string) string {
	var b strings.Builder
	inFence := false

	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimRight(line, " ")

		if strings.HasPrefix(strings.TrimSpace(trimmed), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			b.WriteString("    " + ui.Dim + line + ui.Reset + "\n")
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "### "):
			title := strings.TrimPrefix(trimmed, "### ")
			b.WriteString("\n" + ui.Bold + title + ui.Reset + "\n")
		case strings.HasPrefix(trimmed, "## "):
			title := strings.TrimPrefix(trimmed, "## ")
			b.WriteString("\n" + ui.Bold + ui.Cyan + title + ui.Reset + "\n")
		case strings.HasPrefix(trimmed, "# "):
			title := strings.TrimPrefix(trimmed, "# ")
			rule := strings.Repeat("═", len([]rune(title)))
			b.WriteString(ui.Bold + ui.Cyan + title + ui.Reset + "\n" + ui.Cyan + rule + ui.Reset + "\n")
		case strings.HasPrefix(trimmed, "- "):
			b.WriteString("  • " + renderInlineTerminal(strings.TrimPrefix(trimmed, "- ")) + "\n")
		default:
			b.WriteString(renderInlineTerminal(line) + "\n")
		}
	}
	return b.String()
}

// renderInlineTerminal applies inline styling within one line. Order
// matters: links are resolved to their visible text first (a terminal has
// nowhere to send an anchor link), then code spans, then bold — none of
// vitals' own docs nest these, so a fixed pass order is safe.
func renderInlineTerminal(s string) string {
	s = reLink.ReplaceAllString(s, "$1")
	s = reCode.ReplaceAllString(s, ui.Dim+"$1"+ui.Reset)
	s = reBold.ReplaceAllString(s, ui.Bold+"$1"+ui.Reset)
	return s
}
