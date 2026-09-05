// Package ui provides small terminal formatting helpers shared by every subcommand.
package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// ANSI color codes. Disabled automatically when stdout is not a terminal or
// when NO_COLOR is set (https://no-color.org).
var (
	enabled = colorEnabled()

	Red    = code("\033[1;31m")
	Green  = code("\033[1;32m")
	Yellow = code("\033[1;33m")
	Cyan   = code("\033[1;36m")
	Bold   = code("\033[1m")
	Dim    = code("\033[2m")
	Reset  = code("\033[0m")
)

// ColorEnabled reports whether styled output is currently active.
func ColorEnabled() bool { return enabled }

// DisableColor forces all styling off regardless of TTY detection or the
// NO_COLOR environment variable. Call it once at startup, before any output.
func DisableColor() {
	enabled = false
	Red, Green, Yellow, Cyan, Bold, Dim, Reset = "", "", "", "", "", "", ""
}

// colorEnabled decides whether to emit ANSI codes at all. Two real
// cross-platform gaps this closes over the previous hand-rolled check
// (a bare os.Stdout.Stat() mode-bit test): (1) that check never
// recognized a real terminal on Windows, where console handles don't
// set ModeCharDevice the same way, and isatty.IsTerminal/
// IsCygwinTerminal are the actual, widely-used way to detect one there
// (also covers MSYS/Git-Bash's Cygwin-style pty, which IsTerminal alone
// misses); and (2) even a real Windows terminal needs
// ENABLE_VIRTUAL_TERMINAL_PROCESSING switched on before it will render
// raw ANSI escape codes as color instead of printing them literally —
// EnableColorsStdout does that once, in place, on os.Stdout's existing
// handle, which is why every existing fmt.Printf(ui.Bold+...+ui.Reset)
// call site elsewhere in this codebase needed no changes: nothing wraps
// or replaces os.Stdout, the console's own rendering mode changes
// instead. Both calls are no-ops on macOS/Linux.
func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	colorable.EnableColorsStdout(nil)
	fd := os.Stdout.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func code(c string) string {
	if enabled {
		return c
	}
	return ""
}

// Header prints a boxed section title.
// Header prints a section title: bold and colored, with a thin rule sized to
// the title's own width rather than a fixed banner width — it can never wrap
// a narrow terminal, because there is nothing in it wider than the title.
func Header(title string) {
	rule := strings.Repeat("─", len([]rune(title)))
	fmt.Printf("\n%s%s%s\n%s%s%s\n\n", Bold+Cyan, title, Reset, Cyan, rule, Reset)
}

// Rule prints a thin divider.
func Rule() { fmt.Println(strings.Repeat("-", 80)) }

// Infof / Warnf / Errf / Okf are colored line printers. The glyphs
// (●/✓/⚠️/✗) replace an earlier "[*]"/"[+]"/"[!]"/"[x]" bracket-tag
// convention that read as dated next to the rest of vitals' output
// (box-drawing rules, →, —); vitals already assumes a UTF-8-capable
// terminal elsewhere (Header's ─ rule, PrintFindings' →, summaryLine's
// resource emoji), so this adds no new compatibility risk. Warnf's ⚠️
// carries the emoji variation selector (unlike the others, plain
// text-presentation glyphs) specifically because the bare warning sign
// rendered too faint/unclear to read as a warning at a glance.
func Infof(format string, a ...any) { fmt.Printf(Cyan+"● "+Reset+format+"\n", a...) }
func Okf(format string, a ...any)   { fmt.Printf(Green+"✓ "+Reset+format+"\n", a...) }
func Warnf(format string, a ...any) { fmt.Printf(Yellow+"⚠️ "+Reset+format+"\n", a...) }
func Errf(format string, a ...any)  { fmt.Fprintf(os.Stderr, Red+"✗ "+Reset+format+"\n", a...) }
func Actionf(format string, a ...any) string {
	return Yellow + fmt.Sprintf(format, a...) + Reset
}

// DefaultWrapWidth is the column width TermWidth falls back to when the
// real terminal width can't be determined — output is redirected to a
// file or pipe, or the query itself fails. A moderately conservative
// guess for exactly those non-interactive cases; TermWidth is what
// callers should actually use for wrapping.
const DefaultWrapWidth = 92

// TermWidth returns stdout's current column width, or DefaultWrapWidth
// when it can't be determined. Call it once per render, not per line —
// findings/output printed together should wrap consistently even if
// something changed mid-print, which is unlikely but free to guarantee.
func TermWidth() int {
	w, _, _ := TermSize()
	return w
}

// DefaultTermHeight is the row count TermSize falls back to when the real
// terminal height can't be determined — the classic 80x24 default's
// height half.
const DefaultTermHeight = 24

// TermSize returns stdout's current (columns, rows) and whether a real
// terminal size was read. On failure — output redirected, or the query
// itself failing on a real TTY — it returns (DefaultWrapWidth,
// DefaultTermHeight, false) so a caller can tell "genuine size" from
// "fell back" and choose to render unbounded rather than trust a guessed
// height.
func TermSize() (cols, rows int, ok bool) {
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 && h > 0 {
		return w, h, true
	}
	return DefaultWrapWidth, DefaultTermHeight, false
}

// Wrap breaks text into lines no wider than width, breaking only at word
// boundaries — a word longer than width is never split, so it can still
// overflow on its own. Call it on plain, uncoloured text before applying
// any ANSI wrapping (Key, Actionf, ...): width counts runes, not a
// display width adjusted for invisible escape codes.
func Wrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	if width <= 0 {
		return []string{strings.Join(words, " ")}
	}
	lines := []string{words[0]}
	for _, w := range words[1:] {
		last := lines[len(lines)-1]
		if len([]rune(last))+1+len([]rune(w)) > width {
			lines = append(lines, w)
		} else {
			lines[len(lines)-1] = last + " " + w
		}
	}
	return lines
}

// Key renders a field label (dim, so values stand out beside it).
func Key(s string) string { return Dim + s + Reset }

// Emph renders a value with bold emphasis.
func Emph(s string) string { return Bold + s + Reset }

// colorFor returns the ANSI colour for a value where higher means worse:
// green below warn, yellow from warn, red from crit.
func colorFor(v, warn, crit float64) string {
	switch {
	case v >= crit:
		return Red
	case v >= warn:
		return Yellow
	default:
		return Green
	}
}

// Grade wraps text in green / yellow / red according to where v sits relative
// to the warn and crit thresholds (higher v = worse). Use it for percentages,
// temperatures, latencies — anything with a "too high" direction.
func Grade(text string, v, warn, crit float64) string {
	return colorFor(v, warn, crit) + text + Reset
}

// GradeWidth is Grade for a fixed-width table column: it right-pads text to
// width *before* wrapping it in color, then colors it. Doing it the other
// way — coloring first, padding via an outer %Ns — counts the invisible
// ANSI escape bytes toward the width, so the field never gets padded at
// all once its own color codes already exceed the declared width. Text
// longer than width is never truncated, only left as-is.
func GradeWidth(width int, text string, v, warn, crit float64) string {
	return Grade(fmt.Sprintf("%*s", width, text), v, warn, crit)
}

// GradeLow is Grade for values where lower means worse (free space, battery %,
// headroom): green above warn, yellow at/below warn, red at/below crit.
func GradeLow(text string, v, warn, crit float64) string {
	c := Green
	switch {
	case v <= crit:
		c = Red
	case v <= warn:
		c = Yellow
	}
	return c + text + Reset
}

// ansiRE matches CSI escape sequences (color codes, cursor movement, screen clears).
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI escape sequences from s. Used to compare rendered
// output against golden files without color noise.
func StripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// escSeqRE matches the escape sequences a hostile string could carry to
// drive the terminal: a full CSI sequence (`\x1b[ ... final-byte`,
// intermediate bytes allowed), an OSC sequence (`\x1b] ... BEL` or
// `\x1b] ... \x1b\`, e.g. window-title and OSC-8 hyperlinks), and a
// bare two-byte escape (`\x1bX`) including the C1 CSI byte `\x9b`.
var escSeqRE = regexp.MustCompile(`\x1b\[[0-\?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x9b[0-\?]*[ -/]*[@-~]|\x1b[@-Z\\-_]`)

// Sanitize makes an externally-sourced string safe to print to a
// terminal. It is the single choke point for anything vitals didn't
// author itself — process names and command lines, file and mount
// paths, usernames, reverse-DNS peer hosts, config-file values — none of
// which vitals controls and any of which a hostile local process can
// stuff with terminal-driving escape sequences (cursor moves that
// repaint vitals' own verdict, OSC-8 phishing hyperlinks, title
// rewrites).
//
// It strips: every escape/CSI/OSC sequence (escSeqRE); every remaining
// C0 control byte except that a tab becomes a single space; DEL; and the
// lone C1 range (\x80-\x9f). Printable text — including non-ASCII,
// combining marks, and emoji — is left untouched; callers that also need
// a display-width bound apply Truncate on top.
func Sanitize(s string) string {
	s = escSeqRE.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++ // drop an invalid byte (a raw C1 like 0x9b lands here)
			continue
		}
		i += size
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r == 0x7f, r < 0x20, r >= 0x80 && r <= 0x9f:
			// drop: C0 controls, DEL, the C1 range
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// GradeSeverity colours text by a diag severity word ("ok"/"warning"/
// "critical", i.e. diag.Severity.String()). The one place a severity is
// mapped to a terminal colour — the verdict banner, PrintFindings, and
// the console at-a-glance view (roadmap 011) all go through here rather
// than open-coding the switch. "ok" and anything unrecognised get no
// colour (the default terminal foreground already reads as "fine").
func GradeSeverity(severityWord, text string) string {
	switch severityWord {
	case "warning":
		return Yellow + text + Reset
	case "critical":
		return Red + text + Reset
	default:
		return text
	}
}

// Truncate shortens s to at most n runes, appending a single-character ellipsis
// when it has to cut. It is rune-aware, so multibyte text is never split
// mid-character. n <= 0 yields an empty string; n == 1 yields the first rune.
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return string(r[:1])
	}
	return string(r[:n-1]) + "…"
}

// HumanBytes renders a byte count as a human-readable string.
func HumanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
