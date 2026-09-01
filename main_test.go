package main

import (
	"slices"
	"strings"
	"testing"

	"vitals/internal/ui"
)

func TestApplyGlobalFlags(t *testing.T) {
	t.Run("passes normal args through unchanged", func(t *testing.T) {
		in := []string{"top", "--sort", "mem", "--watch"}
		got := applyGlobalFlags(in)
		if !slices.Equal(got, in) {
			t.Errorf("got %v, want %v", got, in)
		}
	})

	t.Run("consumes --no-color and disables styling", func(t *testing.T) {
		savedRed, savedEnabled := ui.Red, ui.ColorEnabled()
		t.Cleanup(func() { ui.Red = savedRed; _ = savedEnabled })

		got := applyGlobalFlags([]string{"--no-color", "memcheck"})
		if !slices.Equal(got, []string{"memcheck"}) {
			t.Errorf("got %v, want [memcheck]", got)
		}
		if ui.Red != "" {
			t.Errorf("styling not disabled: ui.Red = %q", ui.Red)
		}
	})

	t.Run("empty input yields empty output", func(t *testing.T) {
		if got := applyGlobalFlags(nil); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
}

func TestUserGuideEmbedded(t *testing.T) {
	if len(userGuide) < 500 {
		t.Fatalf("USERGUIDE.md not embedded (got %d bytes)", len(userGuide))
	}
	for _, want := range []string{"vitals doctor", "vitals llm", "Shell completion"} {
		if !strings.Contains(userGuide, want) {
			t.Errorf("embedded guide missing %q", want)
		}
	}
}
