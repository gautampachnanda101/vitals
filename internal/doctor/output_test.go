package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteJSONReportWritesAValidEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "report.json")
	snap := Snapshot{CPU: CPU{Cores: 4, UsedPct: 55}}
	report := Analyze(snap)

	if err := WriteJSONReport(path, snap, report); err != nil {
		t.Fatalf("WriteJSONReport: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the file (including its nested dir) to be created: %v", err)
	}
	// diag.Severity only marshals to JSON (no UnmarshalJSON), so — consistent
	// with this package's other JSON-shape tests (schema_test.go) — verify
	// via a generic map plus a content check rather than a full struct decode.
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
	if !strings.Contains(string(data), `"schema_version": "`+SchemaVersion+`"`) {
		t.Errorf("written file missing schema_version %q:\n%s", SchemaVersion, data)
	}
	snapshot, _ := generic["snapshot"].(map[string]any)
	cpu, _ := snapshot["cpu"].(map[string]any)
	if cpu["used_percent"] != 55.0 {
		t.Errorf("round-tripped snapshot cpu.used_percent = %v, want 55", cpu["used_percent"])
	}
}

func TestMaybeWriteOutputSkipsAnEmptyPath(t *testing.T) {
	if err := maybeWriteOutput("", Snapshot{}, Analyze(Snapshot{})); err != nil {
		t.Errorf("an empty --output path should be a no-op, got %v", err)
	}
}

func TestMaybeWriteOutputWritesWhenPathIsSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := maybeWriteOutput(path, Snapshot{}, Analyze(Snapshot{})); err != nil {
		t.Fatalf("maybeWriteOutput: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist after maybeWriteOutput: %v", path, err)
	}
}

func TestWriteJSONReportRejectsAnUnwritablePath(t *testing.T) {
	// A file where a directory component is expected can never be created.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteJSONReport(filepath.Join(blocker, "report.json"), Snapshot{}, Analyze(Snapshot{}))
	if err == nil {
		t.Error("expected an error writing under a path component that is a plain file")
	}
}
