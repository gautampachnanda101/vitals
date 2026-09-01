package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"

	"vitals/internal/diag"
)

// maybeWriteOutput writes the JSON envelope to path, unless path is empty —
// the no-op case for a command that didn't pass --output.
func maybeWriteOutput(path string, s Snapshot, r diag.Report) error {
	if path == "" {
		return nil
	}
	return WriteJSONReport(path, s, r)
}

// WriteJSONReport writes the canonical --json envelope for (s, r) to path,
// creating any missing parent directories. Used by --output on doctor and
// the resource deep-dive commands so a report can be saved for later —
// e.g. an enterprise host without SSH access, or a before/after comparison.
func WriteJSONReport(path string, s Snapshot, r diag.Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(JSONReport(s, r))
}
