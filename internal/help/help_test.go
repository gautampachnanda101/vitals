package help

import (
	"vitals/internal/doctor"
	"vitals/internal/ui"

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
	for _, want := range []string{"doctor", "top", "clean", "memhogs", "memcheck", "llm", "guide", "completion", "licenses"} {
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

func TestRenderCommandFlagColumnFitsTheLongestFlag(t *testing.T) {
	var b strings.Builder
	if err := RenderCommand(&b, "doctor"); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	// The longest doctor flag must still leave a gap before its help text.
	if !strings.Contains(out, "--webhook-allow-insecure  allow plain http") {
		t.Errorf("long flag runs into its description:\n%s", out)
	}
	// Every flag's description starts in the same column.
	col := -1
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  --") {
			continue
		}
		i := strings.Index(line, "  ")
		j := strings.Index(line[i+2:], "  ")
		// position after the padded flag column
		rest := line[i+2+j:]
		pos := len(line) - len(strings.TrimLeft(rest, " "))
		if col == -1 {
			col = pos
		} else if pos != col {
			t.Errorf("flag descriptions are misaligned (col %d vs %d): %q", pos, col, line)
		}
	}
}

func TestRenderCommandSchemaVersionIsCurrent(t *testing.T) {
	var b strings.Builder
	_ = RenderCommand(&b, "doctor")
	if !strings.Contains(b.String(), "schema v"+doctor.SchemaVersion+")") {
		t.Errorf("doctor help should quote the live schema version %s:\n%s", doctor.SchemaVersion, b.String())
	}
}

func TestExamplesGetAPromptAndKeepNotes(t *testing.T) {
	if got := ui.StripANSI(example("vitals doctor")); got != "$ vitals doctor" {
		t.Errorf("example = %q", got)
	}
	if got := ui.StripANSI(example("vitals top  # the default view")); got != "$ vitals top  # the default view" {
		t.Errorf("example with note = %q", got)
	}
	if got := ui.StripANSI(flagHelp("base URL (default http://x)")); got != "base URL (default http://x)" {
		t.Errorf("flagHelp changed the text: %q", got)
	}
	if got := flagHelp("no default here"); got != "no default here" {
		t.Errorf("flagHelp without a default = %q", got)
	}
}
