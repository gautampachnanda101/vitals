package doctor

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// HistoryPoint is one recorded `vitals doctor` sample — just enough to answer
// "has this been getting worse" without storing a full Snapshot per point.
type HistoryPoint struct {
	Time        time.Time `json:"time"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemPercent  float64   `json:"mem_percent"`
	DiskPercent float64   `json:"disk_percent"` // fullest real mount, 0 if none measured
	TopMemPID   int32     `json:"top_mem_pid"`
	TopMemName  string    `json:"top_mem_name"`
	TopMemRSS   uint64    `json:"top_mem_rss_bytes"`
}

const (
	historyMaxAge    = 24 * time.Hour
	historyMaxPoints = 2000
)

// pointFromSnapshot extracts the fields worth trending from a Snapshot. Pure,
// so trend logic built on it stays testable from fixtures.
func pointFromSnapshot(s Snapshot, now time.Time) HistoryPoint {
	p := HistoryPoint{
		Time:       now,
		CPUPercent: s.CPU.UsedPct,
		MemPercent: s.Memory.UsedPct,
		TopMemPID:  s.Memory.TopProc.PID,
		TopMemName: s.Memory.TopProc.Name,
		TopMemRSS:  s.Memory.TopProc.RSSBytes,
	}
	if d, ok := fullestDisk(s.Disks); ok {
		p.DiskPercent = d.UsedPct
	}
	return p
}

// pruneHistory drops points older than maxAge, then caps the remainder to the
// most recent maxPoints — both bounds matter: age keeps the window
// meaningful ("last 24h"), the count cap keeps a busy history file (e.g. from
// `vitals doctor` run in a tight loop) from growing without limit. Assumes
// points is not necessarily sorted and returns oldest-first.
func pruneHistory(points []HistoryPoint, now time.Time, maxAge time.Duration, maxPoints int) []HistoryPoint {
	kept := make([]HistoryPoint, 0, len(points))
	for _, p := range points {
		if now.Sub(p.Time) <= maxAge {
			kept = append(kept, p)
		}
	}
	if len(kept) > maxPoints {
		kept = kept[len(kept)-maxPoints:]
	}
	return kept
}

func historyPath() (string, bool) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(dir, "vitals", "history.jsonl"), true
}

// loadHistoryFrom reads every well-formed line of a JSONL history file,
// oldest first. A missing file or a malformed line is not fatal — it's
// dropped rather than failing the whole load, since a corrupted line
// shouldn't cost every other recorded point.
func loadHistoryFrom(path string) []HistoryPoint {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var points []HistoryPoint
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var p HistoryPoint
		if json.Unmarshal(sc.Bytes(), &p) == nil {
			points = append(points, p)
		}
	}
	return points
}

// appendHistory adds p to the history file at path, pruning on every write
// so the file never needs a separate maintenance pass.
func appendHistory(path string, p HistoryPoint) error {
	points := append(loadHistoryFrom(path), p)
	points = pruneHistory(points, time.Now(), historyMaxAge, historyMaxPoints)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, pt := range points {
		if err := enc.Encode(pt); err != nil {
			return err
		}
	}
	return nil
}

// LoadHistory returns the last 24h of recorded `vitals doctor` samples,
// oldest first, or nil if none have been recorded yet (e.g. history has
// never been written, or os.UserConfigDir() is unavailable).
func LoadHistory() []HistoryPoint {
	path, ok := historyPath()
	if !ok {
		return nil
	}
	return loadHistoryFrom(path)
}

// recordHistory appends the current snapshot to the history file, best
// effort — a write failure (e.g. a read-only config dir) never fails the
// caller.
func recordHistory(s Snapshot) {
	path, ok := historyPath()
	if !ok {
		return
	}
	_ = appendHistory(path, pointFromSnapshot(s, time.Now()))
}
