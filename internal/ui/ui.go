// Package ui provides small terminal formatting helpers shared by every subcommand.
package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// ANSI color codes. Disabled automatically when stdout is not a terminal or
// when NO_COLOR is set (https://no-color.org).
//
// Each color has two representations: a basic 16-color ANSI code (renders
// correctly everywhere) and a 24-bit "truecolor" escape sequence carrying
// the exact hex value site/index.html's hero mockup uses for its fake
// terminal (see the paletteHex* constants below). refreshPalette picks
// between them — truecolor when the terminal advertises support for it
// (supportsTrueColor), the basic code otherwise — so the *real* `vitals
// doctor` output matches the marketing screenshot pixel-for-pixel where
// the terminal can render it, and still looks like normal red/yellow/green
// everywhere else.
var (
	enabled            = colorEnabled()
	trueColorSupported = supportsTrueColor()

	Red    string
	Green  string
	Yellow string
	Cyan   string
	Bold   string
	Dim    string
	Reset  string
)

func init() {
	refreshPalette()
}

// paletteHex* are the hex values from site/index.html's :root custom
// properties that style the marketing hero's fake terminal window
// (--term-crit, --term-warn, --term-accent — see .term-window/.term/.crit/
// .warn-arrow/.prompt in that file's <style> block). They are declared
// once in the base :root and are NOT overridden inside that file's
// `@media (prefers-color-scheme: dark)` block — unlike the page's other
// tokens (--bg, --ink, --ok, --crit, ...), the fake terminal always
// renders in this one dark palette regardless of the visitor's light/dark
// preference, the same way a real terminal window doesn't change color
// scheme when the OS theme does. So there is only one palette to match
// here, not a light/dark pair to choose between — matching the site
// exactly means always using this dark set as the truecolor variant,
// never trying to guess whether the user's real terminal background is
// light or dark (COLORFGBG-style heuristics exist but are unreliable
// enough, and unsupported widely enough, that a wrong guess would make
// output harder to read than just picking one consistent palette).
//
// The site's terminal has no distinct "ok/success" token, so Green reuses
// --term-accent (#6fbfa8) — the same teal used for the mockup's `$`
// prompt and cursor, and the closest thing the palette has to a positive/
// highlight color. Cyan (headers, info bullets) reuses it too: the site's
// terminal only has one non-alert accent hue, so both real-CLI roles map
// to it.
const (
	paletteHexCritR, paletteHexCritG, paletteHexCritB       = 0xe2, 0x69, 0x4a // --term-crit #e2694a
	paletteHexWarnR, paletteHexWarnG, paletteHexWarnB       = 0xd9, 0xa4, 0x41 // --term-warn #d9a441
	paletteHexAccentR, paletteHexAccentG, paletteHexAccentB = 0x6f, 0xbf, 0xa8 // --term-accent #6fbfa8
)

// rgbCode formats a 24-bit truecolor foreground escape sequence, e.g.
// rgbCode(0xe2, 0x69, 0x4a) == "\033[38;2;226;105;74m" for #e2694a.
func rgbCode(r, g, b byte) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

// paletteCode returns "" when color is disabled entirely, the 24-bit
// truecolor sequence for (r,g,b) when the terminal supports it, or the
// basic 16-color fallback otherwise.
func paletteCode(basic string, r, g, b byte) string {
	if !enabled {
		return ""
	}
	if trueColorSupported {
		return rgbCode(r, g, b)
	}
	return basic
}

// refreshPalette (re)computes every exported color var from the current
// enabled/trueColorSupported state. It runs once at package init; tests
// that flip enabled or trueColorSupported directly call it again to see
// the effect (see ui_test.go), the same way DisableColor does.
func refreshPalette() {
	Red = paletteCode("\033[1;31m", paletteHexCritR, paletteHexCritG, paletteHexCritB)
	Green = paletteCode("\033[1;32m", paletteHexAccentR, paletteHexAccentG, paletteHexAccentB)
	Yellow = paletteCode("\033[1;33m", paletteHexWarnR, paletteHexWarnG, paletteHexWarnB)
	Cyan = paletteCode("\033[1;36m", paletteHexAccentR, paletteHexAccentG, paletteHexAccentB)
	Bold = code("\033[1m")
	Dim = code("\033[2m")
	Reset = code("\033[0m")
}

// supportsTrueColor reports whether the terminal advertises full 24-bit
// color support. There is no single universal signal for this, so this
// uses the same conservative heuristic most terminal color libraries
// converge on (e.g. muesli/termenv, Node's supports-color): trust an
// explicit COLORTERM=truecolor or COLORTERM=24bit, and nothing else.
//
// Deliberately NOT treated as a truecolor signal: TERM containing
// "256color" (xterm-256color, screen-256color, tmux-256color, ...) — that
// advertises 256-indexed-color support, not 24-bit truecolor, and the two
// are not interchangeable at the escape-code level. Reading it as
// truecolor would risk visible escape-code garbage on any real
// 256-color-only terminal — a much worse failure than the alternative.
// A terminal that genuinely supports truecolor but doesn't set COLORTERM
// (it happens — some tmux/screen configurations don't pass the variable
// through from the outer terminal) just gets the basic 16-color fallback
// instead of the exact site palette. That's the right tradeoff: a
// slightly-off color on a terminal that could have rendered better is a
// far smaller cost than garbled output on one that can't, so ambiguous
// cases fall back rather than guess.
func supportsTrueColor() bool {
	ct := strings.TrimSpace(os.Getenv("COLORTERM"))
	return strings.EqualFold(ct, "truecolor") || strings.EqualFold(ct, "24bit")
}

// ColorEnabled reports whether styled output is currently active.
func ColorEnabled() bool { return enabled }

// DisableColor forces all styling off regardless of TTY detection or the
// NO_COLOR environment variable. Call it once at startup, before any output.
func DisableColor() {
	enabled = false
	refreshPalette()
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
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return DefaultWrapWidth
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
