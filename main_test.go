package main

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"vitals/internal/ui"
)

// captureStdout swaps both os.Stdout and os.Stderr for the duration of f and
// returns everything written to either. The pipe is drained concurrently,
// not after f returns — some of what run() prints (the embedded user guide,
// the JSON Schema) is tens of KB, comfortably larger than the small
// anonymous-pipe buffer Windows allocates by default, so writing it all
// before anyone reads deadlocks there (it didn't surface on Linux/macOS,
// whose pipe buffers are large enough to absorb it unread).
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

func TestRunEmptyArgsPrintsUsageAndExitsTwo(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = run(nil, "1.2.3") })
	if code != 2 {
		t.Errorf("run(nil, ...) = %d, want 2", code)
	}
	if !strings.Contains(out, "USAGE") {
		t.Errorf("expected usage text, got %q", out)
	}
}

func TestRunUnknownCommandExitsTwo(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = run([]string{"bogus"}, "1.2.3") })
	if code != 2 {
		t.Errorf("run([bogus]) = %d, want 2", code)
	}
	if !strings.Contains(out, `unknown command "bogus"`) {
		t.Errorf("got %q", out)
	}
}

func TestRunVersionPrintsVersionAndExitsZero(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = run([]string{"version"}, "9.9.9") })
	if code != 0 {
		t.Errorf("run([version]) = %d, want 0", code)
	}
	if !strings.Contains(out, "9.9.9") {
		t.Errorf("got %q", out)
	}
}

func TestRunHelpVariants(t *testing.T) {
	t.Run("no args lists commands", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"help"}, "1.0") })
		if code != 0 {
			t.Errorf("code = %d, want 0", code)
		}
		if !strings.Contains(out, "COMMANDS") {
			t.Errorf("got %q", out)
		}
	})
	t.Run("known command", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"help", "doctor"}, "1.0") })
		if code != 0 {
			t.Errorf("code = %d, want 0", code)
		}
		if !strings.Contains(out, "vitals doctor") {
			t.Errorf("got %q", out)
		}
	})
	t.Run("unknown command falls back to the list and exits 2", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"help", "nope"}, "1.0") })
		if code != 2 {
			t.Errorf("code = %d, want 2", code)
		}
		if !strings.Contains(out, "COMMANDS") {
			t.Errorf("got %q", out)
		}
	})
}

func TestRunCompletionVariants(t *testing.T) {
	t.Run("missing shell arg", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"completion"}, "1.0") })
		if code != 2 {
			t.Errorf("code = %d, want 2", code)
		}
		if !strings.Contains(out, "usage: vitals completion") {
			t.Errorf("got %q", out)
		}
	})
	t.Run("unknown shell", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"completion", "powershell"}, "1.0") })
		if code != 2 {
			t.Errorf("code = %d, want 2", code)
		}
		if !strings.Contains(out, "error:") {
			t.Errorf("got %q", out)
		}
	})
	t.Run("bash", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"completion", "bash"}, "1.0") })
		if code != 0 {
			t.Errorf("code = %d, want 0", code)
		}
		if !strings.Contains(out, "doctor") {
			t.Errorf("got %q", out)
		}
	})
}

func TestRunDoctorSchemaPrintsSchemaAndExitsZero(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = run([]string{"doctor", "--schema"}, "1.0") })
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out, "properties") {
		t.Errorf("expected JSON Schema output, got %q", out)
	}
}

func TestRunDoctorCompareValidatesArgs(t *testing.T) {
	t.Run("wrong arg count", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"doctor", "--compare", "one.json"}, "1.0") })
		if code != 2 {
			t.Errorf("code = %d, want 2", code)
		}
		if !strings.Contains(out, "usage: vitals doctor --compare") {
			t.Errorf("got %q", out)
		}
	})
	t.Run("unreadable files", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() {
			code = run([]string{"doctor", "--compare", "/nonexistent-a.json", "/nonexistent-b.json"}, "1.0")
		})
		if code != 1 {
			t.Errorf("code = %d, want 1", code)
		}
		if !strings.Contains(out, "error:") {
			t.Errorf("got %q", out)
		}
	})
}

func TestRunGuidePrintsWithoutServing(t *testing.T) {
	t.Run("raw", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"guide", "--raw"}, "1.0") })
		if code != 0 {
			t.Errorf("code = %d, want 0", code)
		}
		if !strings.Contains(out, "vitals doctor") {
			t.Errorf("got %q", out)
		}
	})
	t.Run("default renders terminal formatting", func(t *testing.T) {
		var code int
		out := captureStdout(t, func() { code = run([]string{"guide"}, "1.0") })
		if code != 0 {
			t.Errorf("code = %d, want 0", code)
		}
		if len(out) == 0 {
			t.Error("expected non-empty rendered guide")
		}
	})
}

func TestMustReturnsExitCode(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = must(nil) })
	if code != 0 {
		t.Errorf("must(nil) = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("must(nil) should print nothing, got %q", out)
	}

	out = captureStdout(t, func() { code = must(errors.New("boom")) })
	if code != 1 {
		t.Errorf("must(err) = %d, want 1", code)
	}
	if !strings.Contains(out, "error: boom") {
		t.Errorf("got %q", out)
	}
}

func TestNewFlagSetUsageDelegatesToHelp(t *testing.T) {
	fs := newFlagSet("doctor")
	if fs.Name() != "doctor" {
		t.Errorf("Name() = %q, want doctor", fs.Name())
	}
	out := captureStdout(t, func() { fs.Usage() })
	if !strings.Contains(out, "vitals doctor") {
		t.Errorf("Usage() = %q, want the doctor help text", out)
	}
}

func TestDefaultOllamaURL(t *testing.T) {
	saved := cfg.OllamaURL
	t.Cleanup(func() { cfg.OllamaURL = saved })

	cfg.OllamaURL = ""
	if got := defaultOllamaURL(); got != "http://localhost:11434" {
		t.Errorf("defaultOllamaURL() with no override = %q", got)
	}
	cfg.OllamaURL = "http://gpu-box:11434"
	if got := defaultOllamaURL(); got != "http://gpu-box:11434" {
		t.Errorf("defaultOllamaURL() with override = %q", got)
	}
}

func TestDefaultLocalRuntimeURLsFallThroughToEmpty(t *testing.T) {
	saved := cfg
	t.Cleanup(func() { cfg = saved })

	cfg.LMStudioURL, cfg.LlamaCppURL, cfg.VLLMURL = "", "", ""
	if got := defaultLMStudioURL(); got != "" {
		t.Errorf("defaultLMStudioURL() with no override = %q, want empty (internal/llm fills in its own default)", got)
	}
	if got := defaultLlamaCppURL(); got != "" {
		t.Errorf("defaultLlamaCppURL() with no override = %q, want empty", got)
	}
	if got := defaultVLLMURL(); got != "" {
		t.Errorf("defaultVLLMURL() with no override = %q, want empty", got)
	}

	cfg.LMStudioURL, cfg.LlamaCppURL, cfg.VLLMURL = "http://a:1", "http://b:2", "http://c:3"
	if got := defaultLMStudioURL(); got != "http://a:1" {
		t.Errorf("defaultLMStudioURL() with override = %q", got)
	}
	if got := defaultLlamaCppURL(); got != "http://b:2" {
		t.Errorf("defaultLlamaCppURL() with override = %q", got)
	}
	if got := defaultVLLMURL(); got != "http://c:3" {
		t.Errorf("defaultVLLMURL() with override = %q", got)
	}
}

func TestUserGuideEmbedded(t *testing.T) {
	if len(userGuide) < 500 {
		t.Fatalf("docs/user-guide.md not embedded (got %d bytes)", len(userGuide))
	}
	for _, want := range []string{"vitals doctor", "vitals llm", "Shell completion"} {
		if !strings.Contains(userGuide, want) {
			t.Errorf("embedded guide missing %q", want)
		}
	}
}
