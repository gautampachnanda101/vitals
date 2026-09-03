package help

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestRenderListIncludesVersionUsageAndEveryCommand(t *testing.T) {
	var buf bytes.Buffer
	RenderList(&buf, "1.2.3")
	out := buf.String()
	for _, want := range []string{"1.2.3", "USAGE", "COMMANDS", "vitals help <command>"} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderList missing %q, got:\n%s", want, out)
		}
	}
	for _, name := range Names() {
		if !strings.Contains(out, name) {
			t.Errorf("RenderList missing command %q from the list, got:\n%s", name, out)
		}
	}
}

func TestNamesAreSortedAndComplete(t *testing.T) {
	names := Names()
	if !slices.IsSorted(names) {
		t.Errorf("Names() not sorted: %v", names)
	}
	for _, want := range []string{"doctor", "top", "clean", "memhogs", "memcheck", "llm", "guide", "completion"} {
		if !slices.Contains(names, want) {
			t.Errorf("command %q missing from Names()", want)
		}
	}
}

func TestLookup(t *testing.T) {
	c, ok := Lookup("doctor")
	if !ok {
		t.Fatal("doctor not found")
	}
	if c.Synopsis == "" || c.Long == "" || len(c.Examples) == 0 {
		t.Errorf("doctor help is incomplete: %+v", c)
	}
	if _, ok := Lookup("nonesuch"); ok {
		t.Error("Lookup returned ok for an unknown command")
	}
}

func TestRenderCommand(t *testing.T) {
	var b strings.Builder
	if err := RenderCommand(&b, "llm"); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"vitals llm", "USAGE", "EXAMPLES", "--json"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered llm help missing %q\n%s", want, out)
		}
	}
	if err := RenderCommand(&b, "nope"); err == nil {
		t.Error("RenderCommand should error on an unknown command")
	}
}

func TestCompletionScript(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		s, err := CompletionScript(shell)
		if err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		for _, name := range []string{"doctor", "memcheck", "completion"} {
			if !strings.Contains(s, name) {
				t.Errorf("%s completion missing command %q", shell, name)
			}
		}
	}
	if _, err := CompletionScript("powershell"); err == nil {
		t.Error("unknown shell should error")
	}
}
