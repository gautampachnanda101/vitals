package diskusage

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

type directory interface {
	Lstat(string) (fs.FileInfo, error)
	OpenRoot(string) (directory, error)
	Reader() (directoryReader, error)
	Close() error
}

type directoryReader interface {
	ReadDir(int) ([]fs.DirEntry, error)
	Close() error
}

type osDirectory struct{ root *os.Root }

func (d osDirectory) Lstat(name string) (fs.FileInfo, error) { return d.root.Lstat(name) }
func (d osDirectory) Close() error                           { return d.root.Close() }
func (d osDirectory) Reader() (directoryReader, error)       { return d.root.Open(".") }
func (d osDirectory) OpenRoot(name string) (directory, error) {
	r, err := d.root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return osDirectory{r}, nil
}

type dependencies struct {
	openRoot func(context.Context, string) (directory, error)
	mounts   func(context.Context) ([]string, error)
	now      func() time.Time
	sameFile func(fs.FileInfo, fs.FileInfo) bool
	volume   func(fs.FileInfo) (uint64, bool)
	limits   scanLimits
}

type scanLimits struct{ entries, children, depth int }

func liveDependencies() dependencies {
	return dependencies{
		openRoot: openCheckedRoot, mounts: liveMounts, now: time.Now,
		sameFile: os.SameFile, volume: volumeID,
		limits: scanLimits{entries: 1_000_000, children: 10_000, depth: 128},
	}
}

func liveMounts(ctx context.Context) ([]string, error) {
	return mountPaths(ctx, disk.PartitionsWithContext)
}

func mountPaths(ctx context.Context, partitionsWithContext func(context.Context, bool) ([]disk.PartitionStat, error)) ([]string, error) {
	partitions, err := partitionsWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(partitions))
	for _, partition := range partitions {
		paths = append(paths, filepath.Clean(partition.Mountpoint))
	}
	return paths, nil
}

func openCheckedRoot(ctx context.Context, path string) (directory, error) {
	return openFromAnchor(ctx, path, openOSRoot, os.SameFile)
}

func openOSRoot(path string) (directory, error) {
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return osDirectory{r}, nil
}

// Start at the filesystem anchor, never reopen an absolute descendant path.
// Each next handle must still identify the directory checked on its parent.
func openFromAnchor(ctx context.Context, path string, open func(string) (directory, error), same func(fs.FileInfo, fs.FileInfo) bool) (directory, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	anchor := filepath.VolumeName(path) + string(filepath.Separator)
	current, err := open(anchor)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(anchor, path)
	if err != nil {
		_ = current.Close()
		return nil, err
	}
	if relative == "." {
		return current, nil
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if err := ctx.Err(); err != nil {
			_ = current.Close()
			return nil, err
		}
		info, err := current.Lstat(component)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			err = fmt.Errorf("root and ancestors must be real directories, not symlinks; select the real directory path")
		}
		if err != nil {
			_ = current.Close()
			return nil, err
		}
		next, err := current.OpenRoot(component)
		if err == nil {
			err = ctx.Err()
			if err == nil {
				after, checkErr := current.Lstat(component)
				if checkErr != nil {
					err = checkErr
				} else if after.Mode()&os.ModeSymlink != 0 || !same(info, after) {
					err = fmt.Errorf("root directory replaced during opening; select a stable real directory")
				} else {
					err = ctx.Err()
				}
			}
			if err != nil {
				_ = next.Close()
			}
		}
		_ = current.Close()
		if err != nil {
			return nil, err
		}
		actual, err := next.Lstat(".")
		if err == nil {
			err = ctx.Err()
		}
		if err == nil && !same(info, actual) {
			err = fmt.Errorf("root directory changed during opening; retry using a stable real directory")
		}
		if err != nil {
			_ = next.Close()
			return nil, err
		}
		current = next
	}
	return current, nil
}
