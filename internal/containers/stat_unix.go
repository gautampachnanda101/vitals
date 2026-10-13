//go:build darwin || linux

package containers

import (
	"io/fs"
	"syscall"
)

func filePhysicalBytes(info fs.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		// stat.Blocks is 512-byte blocks allocated
		if stat.Blocks > 0 {
			return uint64(stat.Blocks) * 512
		}
	}
	return uint64(info.Size())
}
