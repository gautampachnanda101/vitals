package guide

import "testing"

func TestHeadingLevelDetectsEachLevelAndStripsThePrefix(t *testing.T) {
	cases := []struct {
		in        string
		wantLevel int
		wantText  string
	}{
		{"# Title", 1, "Title"},
		{"## Section", 2, "Section"},
		{"### Sub-section", 3, "Sub-section"},
	}
	for _, c := range cases {
		level, text, ok := headingLevel(c.in)
		if !ok || level != c.wantLevel || text != c.wantText {
			t.Errorf("headingLevel(%q) = (%d, %q, %v), want (%d, %q, true)", c.in, level, text, ok, c.wantLevel, c.wantText)
		}
	}
}

func TestHeadingLevelRejectsNonHeadingLines(t *testing.T) {
	for _, in := range []string{"", "plain text", "- a bullet", "#no space", "regular #hashtag mention"} {
		if _, _, ok := headingLevel(in); ok {
			t.Errorf("headingLevel(%q) = ok, want not-a-heading", in)
		}
	}
}

// TestHeadingEmphasisLevel1GetsARuleEveryOtherLevelDoesNot pins the one
// place that decides which levels look "biggest" — both RenderTerminal and
// RenderHTML's pageTemplate CSS are built to match this, so a future level
// added here only needs updating once, not once per renderer.
func TestHeadingEmphasisLevel1GetsARuleEveryOtherLevelDoesNot(t *testing.T) {
	e1 := headingEmphasis(1)
	if !e1.Bold || !e1.Color || !e1.Rule {
		t.Errorf("headingEmphasis(1) = %+v, want Bold+Color+Rule all true", e1)
	}
	for _, level := range []int{2, 3} {
		e := headingEmphasis(level)
		if !e.Bold || !e.Color {
			t.Errorf("headingEmphasis(%d) = %+v, want Bold+Color true", level, e)
		}
		if e.Rule {
			t.Errorf("headingEmphasis(%d) = %+v, want Rule false (only level 1 gets the underline)", level, e)
		}
	}
}
