package ui

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout swaps both os.Stdout and os.Stderr for the duration of f
// and returns everything written to either — Errf specifically writes to
// stderr. The pipe is drained concurrently, not after f returns: Windows'
// small default anonymous-pipe buffer deadlocks a synchronous write larger
// than it (this bit main_test.go's guide/schema output, tens of KB, though
// nothing in this package prints that much). Matches the pattern used
// across this codebase's other print-directly packages (internal/monitor,
// internal/memcheck, ...).
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w

	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()

	f()
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-done
}

func TestHeaderPrintsTheTitleWithARuleUnderIt(t *testing.T) {
	out := StripANSI(captureStdout(t, func() { Header("DISK") }))
	if !strings.Contains(out, "DISK") || !strings.Contains(out, "────") {
		t.Errorf("Header(\"DISK\") = %q, want the title and a matching-length rule", out)
	}
}

func TestRulePrintsAnEightyCharLine(t *testing.T) {
	out := strings.TrimSpace(captureStdout(t, func() { Rule() }))
	if len(out) != 80 {
		t.Errorf("Rule() printed a %d-char line, want 80", len(out))
	}
}

func TestInfofOkfWarnfPrintToStdout(t *testing.T) {
	for _, f := range []func(string, ...any){Infof, Okf, Warnf} {
		out := StripANSI(captureStdout(t, func() { f("value=%d", 42) }))
		if !strings.Contains(out, "value=42") {
			t.Errorf("printer did not include the formatted message, got %q", out)
		}
	}
}

func TestErrfPrintsToStderr(t *testing.T) {
	out := StripANSI(captureStdout(t, func() { Errf("boom %s", "now") }))
	if !strings.Contains(out, "boom now") {
		t.Errorf("Errf message missing, got %q", out)
	}
}

func TestColorEnabledRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorEnabled() {
		t.Error("colorEnabled() should be false whenever NO_COLOR is set, regardless of value or TTY state")
	}
}

func TestKeyAndEmphWrapWithoutChangingTheText(t *testing.T) {
	if got := StripANSI(Key("hello")); got != "hello" {
		t.Errorf("Key(\"hello\") stripped = %q, want hello unchanged", got)
	}
	if got := StripANSI(Emph("hello")); got != "hello" {
		t.Errorf("Emph(\"hello\") stripped = %q, want hello unchanged", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1099511627776, "1.00 TB"},
		{-1, "-1 B"},
	}
	for _, c := range cases {
		if got := HumanBytes(c.in); got != c.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"exactly at limit", "hello", 5, "hello"},
		{"longer ascii", "hello world", 5, "hell…"},
		{"multibyte stays valid", "héllo wörld", 5, "héll…"},
		{"cjk", "日本語テスト", 3, "日本…"},
		{"n is one", "abc", 1, "a"},
		{"n is zero", "abc", 0, ""},
		{"empty input", "", 5, ""},
		{"negative n", "abc", -2, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Truncate(c.s, c.n); got != c.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
			}
			for _, r := range Truncate(c.s, c.n) {
				if r == '�' {
					t.Errorf("Truncate(%q, %d) produced an invalid rune", c.s, c.n)
				}
			}
		})
	}
}

func TestDisableColor(t *testing.T) {
	// Save and restore package styling state around the test.
	saved := []*string{&Red, &Green, &Yellow, &Cyan, &Bold, &Dim, &Reset}
	orig := make([]string, len(saved))
	for i, p := range saved {
		orig[i] = *p
	}
	origEnabled := enabled
	t.Cleanup(func() {
		for i, p := range saved {
			*p = orig[i]
		}
		enabled = origEnabled
	})

	Red = "\033[1;31m"
	enabled = true
	DisableColor()

	if enabled {
		t.Error("enabled should be false after DisableColor")
	}
	for _, p := range saved {
		if *p != "" {
			t.Errorf("style code %q not cleared", *p)
		}
	}
	if got := Actionf("x"); got != "x" {
		t.Errorf("Actionf still wraps in color: %q", got)
	}
}

func TestGrade(t *testing.T) {
	// Colours differ by band; text is always preserved verbatim after StripANSI.
	lo := Grade("40%", 40, 60, 85)
	mid := Grade("70%", 70, 60, 85)
	hi := Grade("90%", 90, 60, 85)
	if StripANSI(lo) != "40%" || StripANSI(mid) != "70%" || StripANSI(hi) != "90%" {
		t.Fatalf("Grade mangled the text: %q %q %q", StripANSI(lo), StripANSI(mid), StripANSI(hi))
	}
	if ColorEnabled() {
		if lo == mid || mid == hi || lo == hi {
			t.Errorf("Grade bands not distinct: %q %q %q", lo, mid, hi)
		}
		if !strings.Contains(lo, "32m") || !strings.Contains(mid, "33m") || !strings.Contains(hi, "31m") {
			t.Errorf("Grade colours wrong: lo=%q mid=%q hi=%q", lo, mid, hi)
		}
	}
}

func TestGradeLow(t *testing.T) {
	full := GradeLow("80%", 80, 20, 8)
	low := GradeLow("15%", 15, 20, 8)
	crit := GradeLow("5%", 5, 20, 8)
	if StripANSI(full) != "80%" || StripANSI(low) != "15%" || StripANSI(crit) != "5%" {
		t.Fatal("GradeLow mangled the text")
	}
	if ColorEnabled() && (full == low || low == crit) {
		t.Errorf("GradeLow bands not distinct: %q %q %q", full, low, crit)
	}
}

func TestGradeWidthPadsBeforeColoring(t *testing.T) {
	// The bug this guards against: Grade(fmt.Sprintf("%8s", ...)) would be
	// wrong the other way around — coloring first, then padding via an
	// outer %8s, counts the invisible ANSI bytes toward the width and adds
	// no visible padding at all. GradeWidth must pad the plain text first.
	got := GradeWidth(8, "59%", 59, 85, 95)
	if visible := StripANSI(got); visible != "     59%" {
		t.Errorf("GradeWidth(8, %q, ...) visible text = %q, want %q", "59%", visible, "     59%")
	}
}

func TestGradeWidthNeverTruncates(t *testing.T) {
	got := GradeWidth(3, "12345%", 12345, 85, 95)
	if visible := StripANSI(got); visible != "12345%" {
		t.Errorf("GradeWidth should never cut text short even under its target width, got %q", visible)
	}
}

func TestStripANSI(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain text", "plain text"},
		{"\033[1;31mred\033[0m", "red"},
		{"\033[H\033[2Jcleared", "cleared"},
		{"a\033[32mb\033[0mc", "abc"},
		{"", ""},
	}
	for _, c := range cases {
		if got := StripANSI(c.in); got != c.want {
			t.Errorf("StripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWrapBreaksOnlyAtWordBoundaries(t *testing.T) {
	// The exact real-world case this fixes: a long fix/detail line that
	// used to overflow the terminal edge entirely unwrapped.
	text := "quit or restart Google Chrome Helper (Renderer) (pid 17751) — the largest consumer, or run `vitals memhogs` for the full list"
	lines := Wrap(text, 40)
	for _, l := range lines {
		if n := len([]rune(l)); n > 40 {
			t.Errorf("line %q is %d runes, want <= 40", l, n)
		}
	}
	if strings.Join(lines, " ") != text {
		t.Errorf("Wrap should not drop or reorder words: got %q, want %q", strings.Join(lines, " "), text)
	}
}

func TestWrapNeverSplitsAWordLongerThanWidth(t *testing.T) {
	lines := Wrap("a-word-way-longer-than-the-width short", 5)
	if lines[0] != "a-word-way-longer-than-the-width" {
		t.Errorf("Wrap should keep an over-width word whole on its own line, got %q", lines[0])
	}
}

func TestWrapHandlesEmptyAndSingleWordInput(t *testing.T) {
	if got := Wrap("", 40); got != nil {
		t.Errorf("Wrap(\"\") = %v, want nil", got)
	}
	if got := Wrap("one", 40); len(got) != 1 || got[0] != "one" {
		t.Errorf("Wrap(single word) = %v, want [\"one\"]", got)
	}
}

func TestWrapWithNonPositiveWidthReturnsOneLine(t *testing.T) {
	got := Wrap("some words here", 0)
	if len(got) != 1 || got[0] != "some words here" {
		t.Errorf("Wrap(width<=0) = %v, want a single unwrapped line", got)
	}
}

func TestTermWidthFallsBackWhenNotATerminal(t *testing.T) {
	// go test's stdout is never a real terminal, so this exercises the
	// fallback path deterministically — the "real detected width" branch
	// needs an attached TTY this test environment doesn't have.
	if got := TermWidth(); got != DefaultWrapWidth {
		t.Errorf("TermWidth() = %d, want the DefaultWrapWidth fallback %d when stdout isn't a terminal", got, DefaultWrapWidth)
	}
}

// withPaletteState forces enabled/trueColorSupported to the given values
// for the duration of f, recomputing the exported color vars (Red, Green,
// ...) before and restoring everything — including the vars themselves —
// after. This is the same save/restore-around-mutation shape TestDisableColor
// already uses for `enabled`, extended to cover the new truecolor switch.
func withPaletteState(t *testing.T, colorEnabledState, trueColor bool, f func()) {
	t.Helper()
	saved := []*string{&Red, &Green, &Yellow, &Cyan, &Bold, &Dim, &Reset}
	orig := make([]string, len(saved))
	for i, p := range saved {
		orig[i] = *p
	}
	origEnabled, origTrueColor := enabled, trueColorSupported
	t.Cleanup(func() {
		enabled, trueColorSupported = origEnabled, origTrueColor
		for i, p := range saved {
			*p = orig[i]
		}
	})

	enabled, trueColorSupported = colorEnabledState, trueColor
	refreshPalette()
	f()
}

func TestTrueColorForcedOnProducesExactSiteHexEscapeSequences(t *testing.T) {
	// Golden-byte assertions: these are the literal 24-bit escape sequences
	// for site/index.html's --term-crit (#e2694a), --term-warn (#d9a441)
	// and --term-accent (#6fbfa8) hex values, in the
	// "\033[38;2;R;G;Bm" truecolor format. Written out in full (not built
	// from the same rgbCode helper the production code uses) so a bug in
	// rgbCode's formatting can't cancel itself out against the test.
	withPaletteState(t, true, true, func() {
		cases := []struct {
			name string
			got  string
			want string
		}{
			{"Red = --term-crit #e2694a", Red, "\033[38;2;226;105;74m"},
			{"Yellow = --term-warn #d9a441", Yellow, "\033[38;2;217;164;65m"},
			{"Green = --term-accent #6fbfa8", Green, "\033[38;2;111;191;168m"},
			{"Cyan = --term-accent #6fbfa8", Cyan, "\033[38;2;111;191;168m"},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
			}
		}
		// Bold/Dim/Reset carry no color and are unaffected by truecolor.
		if Bold != "\033[1m" || Dim != "\033[2m" || Reset != "\033[0m" {
			t.Errorf("Bold/Dim/Reset changed under truecolor: %q %q %q", Bold, Dim, Reset)
		}
	})
}

func TestTrueColorForcedOffFallsBackToBasicSixteenColorCodes(t *testing.T) {
	withPaletteState(t, true, false, func() {
		cases := []struct {
			name string
			got  string
			want string
		}{
			{"Red", Red, "\033[1;31m"},
			{"Green", Green, "\033[1;32m"},
			{"Yellow", Yellow, "\033[1;33m"},
			{"Cyan", Cyan, "\033[1;36m"},
		}
		for _, c := range cases {
			if c.got != c.want {
				t.Errorf("%s: got %q, want the basic ANSI code %q", c.name, c.got, c.want)
			}
			if strings.Contains(c.got, "38;2;") {
				t.Errorf("%s: fell back to basic ANSI but still contains a truecolor escape: %q", c.name, c.got)
			}
		}
	})
}

func TestColorDisabledWinsOverTrueColor(t *testing.T) {
	// enabled=false must beat trueColorSupported=true: NO_COLOR / a
	// non-terminal stdout must never emit any escape sequence, truecolor
	// included. This is a color-policy invariant, not something this
	// change is allowed to touch.
	withPaletteState(t, false, true, func() {
		for name, got := range map[string]string{
			"Red": Red, "Green": Green, "Yellow": Yellow, "Cyan": Cyan,
			"Bold": Bold, "Dim": Dim, "Reset": Reset,
		} {
			if got != "" {
				t.Errorf("%s = %q, want empty when color is disabled regardless of truecolor support", name, got)
			}
		}
	})
}

func TestStripANSIHandlesTrueColorEscapeSequences(t *testing.T) {
	in := "a" + "\033[38;2;226;105;74m" + "red" + "\033[0m" + "b"
	if got := StripANSI(in); got != "aredb" {
		t.Errorf("StripANSI(%q) = %q, want %q", in, got, "aredb")
	}
}

func TestSupportsTrueColorDetection(t *testing.T) {
	cases := []struct {
		name      string
		colorterm string
		want      bool
	}{
		{"COLORTERM=truecolor", "truecolor", true},
		{"COLORTERM=24bit", "24bit", true},
		{"COLORTERM case-insensitive", "TrueColor", true},
		{"COLORTERM padded with whitespace", "  truecolor  ", true},
		{"COLORTERM unset", "", false},
		{"COLORTERM=yes (some terminals set this, but it's not the standard signal)", "yes", false},
		{"COLORTERM=1", "1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.colorterm == "" {
				// t.Setenv can't "unset"; simulate absence by clearing it.
				t.Setenv("COLORTERM", "")
			} else {
				t.Setenv("COLORTERM", c.colorterm)
			}
			if got := supportsTrueColor(); got != c.want {
				t.Errorf("supportsTrueColor() with COLORTERM=%q = %v, want %v", c.colorterm, got, c.want)
			}
		})
	}
}

func TestSupportsTrueColorIgnoresTerm256Color(t *testing.T) {
	// TERM advertising 256-color support (xterm-256color, tmux-256color,
	// screen-256color, ...) must NOT be read as truecolor support — that
	// would risk rendering garbled escape codes on a real 256-color-only
	// terminal. Only an explicit COLORTERM opts in.
	t.Setenv("COLORTERM", "")
	for _, term := range []string{"xterm-256color", "screen-256color", "tmux-256color", "xterm-kitty"} {
		t.Setenv("TERM", term)
		if supportsTrueColor() {
			t.Errorf("supportsTrueColor() with TERM=%q and no COLORTERM = true, want false", term)
		}
	}
}
