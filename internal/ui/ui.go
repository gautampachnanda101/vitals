// Package ui provides small terminal formatting helpers shared by every subcommand.
package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
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

func colorEnabled() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func code(c string) string {
	if enabled {
		return c
	}
	return ""
}

// Header prints a boxed section title.
func Header(title string) {
	line := strings.Repeat("=", 80)
	fmt.Printf("\n%s%s%s\n", Bold+Cyan, line, Reset)
	fmt.Printf("%s  %s%s\n", Bold+Cyan, title, Reset)
	fmt.Printf("%s%s%s\n\n", Bold+Cyan, line, Reset)
}

// Rule prints a thin divider.
func Rule() { fmt.Println(strings.Repeat("-", 80)) }

// Infof / Warnf / Errf / Okf are colored line printers.
func Infof(format string, a ...any) { fmt.Printf(Cyan+"[*] "+Reset+format+"\n", a...) }
func Okf(format string, a ...any)   { fmt.Printf(Green+"[+] "+Reset+format+"\n", a...) }
func Warnf(format string, a ...any) { fmt.Printf(Yellow+"[!] "+Reset+format+"\n", a...) }
func Errf(format string, a ...any)  { fmt.Fprintf(os.Stderr, Red+"[x] "+Reset+format+"\n", a...) }
func Actionf(format string, a ...any) string {
	return Yellow + fmt.Sprintf(format, a...) + Reset
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
