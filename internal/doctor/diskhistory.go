package doctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// diskHistoryEntry is one mount's last-seen free-space reading, persisted
// across separate `vitals` invocations so a single snapshot can still report a
// growth rate and a time-to-full estimate.
type diskHistoryEntry struct {
	FreeBytes uint64 `json:"free_bytes"`
	UnixTime  int64  `json:"unix_time"`
}

// minGrowthInterval is the shortest gap between samples worth trusting for a
// rate: two invocations a second apart would turn normal noise into a wild
// bytes/sec figure.
const minGrowthInterval = 60 * time.Second

func diskHistoryPath() (string, bool) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(dir, "vitals", "disk_history.json"), true
}

func loadDiskHistory() map[string]diskHistoryEntry {
	path, ok := diskHistoryPath()
	if !ok {
		return map[string]diskHistoryEntry{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]diskHistoryEntry{}
	}
	var m map[string]diskHistoryEntry
	if json.Unmarshal(data, &m) != nil || m == nil {
		return map[string]diskHistoryEntry{}
	}
	return m
}

func saveDiskHistory(hist map[string]diskHistoryEntry) {
	path, ok := diskHistoryPath()
	if !ok {
		return
	}
	data, err := json.Marshal(hist)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// diskGrowthRate returns bytes/sec the mount is filling (positive = shrinking
// free space) since the previous invocation's reading for the same mount, and
// records the current reading into hist for next time. It returns 0 when
// there is no usable prior sample, the samples are too close together to be
// stable, or free space actually grew (cleanup happened, not filling).
func diskGrowthRate(hist map[string]diskHistoryEntry, mount string, freeBytes uint64, now time.Time) float64 {
	prev, had := hist[mount]
	hist[mount] = diskHistoryEntry{FreeBytes: freeBytes, UnixTime: now.Unix()}
	if !had {
		return 0
	}
	dt := now.Unix() - prev.UnixTime
	if dt < int64(minGrowthInterval.Seconds()) {
		return 0
	}
	if prev.FreeBytes < freeBytes {
		return 0
	}
	return float64(prev.FreeBytes-freeBytes) / float64(dt)
}
