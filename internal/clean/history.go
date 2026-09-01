package clean

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vitals/internal/ui"
)

// PurgeRecord is one directory's contribution to a clean run.
type PurgeRecord struct {
	Dir     string `json:"dir"`
	Bytes   int64  `json:"bytes"`
	Entries int    `json:"entries"`
}

// RunRecord is one completed (non-dry-run) `vitals clean` run — the audit
// trail for "what did I delete and when": `clean` frees space immediately
// rather than quarantining it (the whole point is to answer "my disk is
// full, fix it now"), so this record is the only thing standing between a
// user and total uncertainty about what happened after the fact.
type RunRecord struct {
	Time       time.Time     `json:"time"`
	TotalBytes int64         `json:"total_bytes"`
	Purges     []PurgeRecord `json:"purges,omitempty"`
}

// cleanHistoryMaxRuns bounds the audit log so it can't grow without limit —
// far more history than anyone needs to answer "what did my last few cleans do".
const cleanHistoryMaxRuns = 200

func cleanHistoryPath() (string, bool) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(dir, "vitals", "clean_history.jsonl"), true
}

// loadCleanHistoryFrom reads every well-formed line, oldest first. A missing
// file or a malformed line is dropped rather than failing the whole load.
func loadCleanHistoryFrom(path string) []RunRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var records []RunRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var r RunRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			records = append(records, r)
		}
	}
	return records
}

// appendCleanHistoryTo adds r to the history file at path, capping it to the
// most recent cleanHistoryMaxRuns entries.
func appendCleanHistoryTo(path string, r RunRecord) error {
	records := append(loadCleanHistoryFrom(path), r)
	if len(records) > cleanHistoryMaxRuns {
		records = records[len(records)-cleanHistoryMaxRuns:]
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

// History returns every recorded clean run, oldest first.
func History() []RunRecord {
	path, ok := cleanHistoryPath()
	if !ok {
		return nil
	}
	return loadCleanHistoryFrom(path)
}

// recordRun appends r to the audit log, best effort — a write failure (e.g.
// a read-only config dir) never fails the clean run itself.
func recordRun(r RunRecord) {
	path, ok := cleanHistoryPath()
	if !ok {
		return
	}
	_ = appendCleanHistoryTo(path, r)
}

// renderCleanHistory formats records (oldest first, as returned by History)
// as a newest-first human-readable list.
func renderCleanHistory(records []RunRecord) string {
	if len(records) == 0 {
		return "no recorded clean runs yet\n"
	}
	var b []byte
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		b = fmt.Appendf(b, "%s  freed %s", r.Time.Format("2006-01-02 15:04:05"), ui.HumanBytes(r.TotalBytes))
		if len(r.Purges) > 0 {
			b = fmt.Appendf(b, "  (%d location%s)", len(r.Purges), plural(len(r.Purges)))
		}
		b = append(b, '\n')
	}
	return string(b)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
