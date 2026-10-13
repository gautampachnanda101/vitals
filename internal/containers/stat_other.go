//go:build !darwin && !linux

package containers

import "io/fs"

func filePhysicalBytes(info fs.FileInfo) uint64 {
	return uint64(info.Size())
}
