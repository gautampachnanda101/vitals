// Package diskusage measures observed regular-file logical bytes without reading
// contents or modifying the tree. Totals are per path, not reclaimable space.
package diskusage

import (
	"fmt"
	"time"
)

type Options struct {
	Top    int           `json:"top"`
	Budget time.Duration `json:"budget"`
}

type Row struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Complete bool   `json:"complete"`
}

type Issue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Result struct {
	Root                       string   `json:"root"`
	Options                    Options  `json:"options"`
	Directories                []Row    `json:"directories"`
	Files                      []Row    `json:"files"`
	RootFiles                  []Row    `json:"root_files"`
	ObservedBytes              int64    `json:"observed_bytes"`
	Entries                    int      `json:"entries"`
	ElapsedMS                  int64    `json:"elapsed_ms"`
	Complete                   bool     `json:"complete"`
	StopReasons                []string `json:"stop_reasons"`
	SkippedSymlinks            int      `json:"skipped_symlinks"`
	SkippedSpecial             int      `json:"skipped_special"`
	SkippedMounts              int      `json:"skipped_mounts"`
	BoundaryDetectionAvailable bool     `json:"boundary_detection_available"`
	ErrorCount                 int      `json:"error_count"`
	Errors                     []Issue  `json:"errors"`
}

func DefaultOptions() Options { return Options{Top: 20, Budget: 30 * time.Second} }

func Validate(opts Options) error {
	if opts.Top < 1 || opts.Top > 100 {
		return fmt.Errorf("top must be between 1 and 100")
	}
	if opts.Budget <= 0 || opts.Budget > 5*time.Minute {
		return fmt.Errorf("budget must be greater than zero and at most 5m")
	}
	return nil
}
