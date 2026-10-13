//go:build darwin || linux

package diskusage

import (
	"io/fs"
	"syscall"
)

func volumeID(info fs.FileInfo) (uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Dev), true
}
