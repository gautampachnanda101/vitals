package doctor

import "vitals/internal/config"

// thresholds holds the currently active alert thresholds. Package-level like
// badMounts and the disk-history cache elsewhere in this package: Analyze
// stays a pure function of Snapshot (no new parameter to thread through every
// call site in main.go, mcp, and metrics), while main sets this once at
// startup from the user's config file, if any.
var thresholds = config.Default()

// SetThresholds overrides the active alert thresholds. Call once at startup,
// before any Collect/Analyze — tests that care about specific threshold
// values should call it too, since they otherwise share this package state.
func SetThresholds(c config.Config) { thresholds = c }
