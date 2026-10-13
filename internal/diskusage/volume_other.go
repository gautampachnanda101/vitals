//go:build !darwin && !linux

package diskusage

import "io/fs"

// Known mount paths are still excluded, but there is no native device-identity
// guarantee on these platforms.
func volumeID(fs.FileInfo) (uint64, bool) { return 0, false }
