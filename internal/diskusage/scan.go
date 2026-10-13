package diskusage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	workerCount  = 6
	queueSize    = 64
	batchSize    = 256
	errorSamples = 20
)

type child struct {
	row  Row
	name string
	info fs.FileInfo
}

type scanner struct {
	mu        sync.Mutex
	ctx       context.Context
	deps      dependencies
	start     time.Time
	result    Result
	reasons   map[string]bool
	stopped   bool
	mounts    map[string]bool
	device    uint64
	hasDevice bool
	children  []*child
}

// Scan waits for all workers, including any in-flight filesystem calls, before
// returning. The budget and context are checked cooperatively between calls.
func Scan(ctx context.Context, root string, opts Options) (Result, error) {
	return scanWith(ctx, root, opts, liveDependencies())
}

// ScanRoot scans an already-authorized handle without reopening displayRoot.
// It never closes the caller's root; the caller must keep it open until return.
func ScanRoot(ctx context.Context, root *os.Root, displayRoot string, opts Options) (Result, error) {
	deps := liveDependencies()
	deps.openRoot = func(context.Context, string) (directory, error) {
		if root == nil {
			return nil, fmt.Errorf("scan root handle is nil; open a real directory first")
		}
		return borrowedDirectory{osDirectory{root}}, nil
	}
	return scanWith(ctx, displayRoot, opts, deps)
}

type borrowedDirectory struct{ osDirectory }

func (borrowedDirectory) Close() error { return nil }

func scanWith(ctx context.Context, root string, opts Options, deps dependencies) (result Result, scanErr error) {
	start := deps.now()
	// Registered first so elapsed includes the final owned-handle closes.
	defer func() { result.ElapsedMS = max(0, deps.now().Sub(start).Milliseconds()) }()
	s := &scanner{
		ctx: ctx, deps: deps, start: start, reasons: make(map[string]bool),
		mounts: make(map[string]bool),
		result: Result{Root: root, Options: opts, Directories: []Row{}, Files: []Row{},
			RootFiles: []Row{}, StopReasons: []string{}, Errors: []Issue{}},
	}
	if err := Validate(opts); err != nil {
		s.reason("invalid_options", true)
		return s.finish(), err
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		s.issue(root, err)
		s.reason("root_error", true)
		return s.finish(), err
	}
	s.result.Root = absolute
	ctx, cancel := context.WithTimeout(ctx, opts.Budget)
	defer cancel()
	s.ctx = ctx
	if s.stop() {
		return s.finish(), nil
	}
	dir, err := deps.openRoot(ctx, absolute)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			s.stop()
			return s.finish(), nil
		}
		s.issue(absolute, err)
		s.reason("root_error", true)
		return s.finish(), fmt.Errorf("open scan root %s: %w", DisplayPath(absolute), err)
	}
	defer dir.Close()
	if s.stop() {
		return s.finish(), nil
	}
	info, err := dir.Lstat(".")
	if err != nil {
		s.issue(absolute, err)
		s.reason("root_error", true)
		return s.finish(), err
	}
	s.result.Entries = 1
	s.device, s.hasDevice = deps.volume(info)
	s.result.BoundaryDetectionAvailable = s.hasDevice
	if s.stop() {
		return s.finish(), nil
	}
	paths, mountErr := deps.mounts(ctx)
	if mountErr != nil {
		// Failure here does not hide the useful native-device fallback; same-
		// device bind mounts remain unrecognizable without the mount inventory.
		s.result.BoundaryDetectionAvailable = false
	}
	for _, path := range paths {
		path = filepath.Clean(path)
		if rel, err := filepath.Rel(absolute, path); err == nil && rel != "." && filepath.IsLocal(rel) {
			s.mounts[path] = true
		}
	}
	if s.stop() {
		return s.finish(), nil
	}
	reader, err := dir.Reader()
	if err != nil {
		s.issue(absolute, err)
		s.reason("root_error", true)
		return s.finish(), err
	}
	defer reader.Close()
	tasks := make(chan *child, queueSize)
	var workers sync.WaitGroup
	for range workerCount {
		workers.Go(func() {
			for task := range tasks {
				if !s.stop() {
					task.row.Bytes, task.row.Complete = s.walk(dir, task.name, task.row.Path, task.info, 1)
				}
			}
		})
	}
	err = s.enumerateRoot(dir, reader, tasks)
	close(tasks)
	workers.Wait()
	if err != nil {
		s.reason("root_error", true)
	}
	return s.finish(), err
}

func (s *scanner) enumerateRoot(dir directory, reader directoryReader, tasks chan<- *child) error {
	_, err := s.read(reader, s.result.Root, func(entry fs.DirEntry) bool {
		path := filepath.Join(s.result.Root, entry.Name())
		info, eligible := s.inspect(dir, entry.Name(), path)
		if !eligible {
			return !s.stop()
		}
		if info.Mode().IsRegular() {
			s.file(path, info.Size(), true)
			return true
		}
		if len(s.children) >= s.deps.limits.children {
			s.reason("root_state_limit", true)
			return false
		}
		task := &child{row: Row{Path: path}, name: entry.Name(), info: info}
		s.children = append(s.children, task)
		select {
		case tasks <- task:
			return true
		case <-s.ctx.Done():
			s.stop()
			return false
		}
	})
	return err
}

func (s *scanner) walk(parent directory, name, path string, expected fs.FileInfo, depth int) (int64, bool) {
	if s.stop() {
		return 0, false
	}
	if depth > s.deps.limits.depth {
		s.reason("depth_limit", false)
		return 0, false
	}
	dir, err := parent.OpenRoot(name)
	if err != nil {
		s.issue(path, err)
		return 0, false
	}
	defer dir.Close()
	if s.stop() {
		return 0, false
	}
	after, err := parent.Lstat(name)
	if err != nil {
		s.issue(path, err)
		return 0, false
	}
	if after.Mode()&os.ModeSymlink != 0 || !s.deps.sameFile(expected, after) {
		s.issue(path, fmt.Errorf("directory replaced during scan; skipped, retry a stable directory"))
		return 0, false
	}
	if s.stop() {
		return 0, false
	}
	actual, err := dir.Lstat(".")
	if err != nil {
		s.issue(path, err)
		return 0, false
	}
	if !s.deps.sameFile(expected, actual) {
		s.issue(path, fmt.Errorf("directory replaced during scan; skipped, retry a stable directory"))
		return 0, false
	}
	if s.stop() {
		return 0, false
	}
	reader, err := dir.Reader()
	if err != nil {
		s.issue(path, err)
		return 0, false
	}
	defer reader.Close()
	var bytes int64
	complete := true
	readComplete, _ := s.read(reader, path, func(entry fs.DirEntry) bool {
		nestedPath := filepath.Join(path, entry.Name())
		info, eligible := s.inspect(dir, entry.Name(), nestedPath)
		if !eligible {
			// Intentional exclusions are complete within the eligible scope.
			if info == nil {
				complete = false
			}
			return !s.stop()
		}
		if info.Mode().IsRegular() {
			s.file(nestedPath, info.Size(), false)
			var exact bool
			bytes, exact = s.addBytes(bytes, info.Size())
			complete = complete && exact
		} else {
			size, done := s.walk(dir, entry.Name(), nestedPath, info, depth+1)
			var exact bool
			bytes, exact = s.addBytes(bytes, size)
			complete = complete && done && exact
		}
		return true
	})
	return bytes, complete && readComplete
}

func (s *scanner) read(reader directoryReader, path string, visit func(fs.DirEntry) bool) (bool, error) {
	for !s.stop() {
		entries, err := reader.ReadDir(batchSize)
		for _, entry := range entries {
			if s.stop() || !s.admit() || !visit(entry) {
				return false, nil
			}
		}
		if s.stop() {
			return false, nil
		}
		if err == io.EOF {
			return true, nil
		}
		if err != nil {
			s.issue(path, err)
			return false, err
		}
	}
	return false, nil
}

func (s *scanner) admit() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result.Entries >= s.deps.limits.entries {
		s.reasons["entry_limit"] = true
		s.stopped = true
		return false
	}
	s.result.Entries++
	return true
}

func (s *scanner) inspect(dir directory, name, path string) (fs.FileInfo, bool) {
	info, err := dir.Lstat(name)
	if err != nil {
		s.issue(path, err)
		return nil, false
	}
	if s.stop() {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		s.result.SkippedSymlinks++
	case !info.IsDir() && !info.Mode().IsRegular():
		s.result.SkippedSpecial++
	case s.mounts[path]:
		s.result.SkippedMounts++
	case s.hasDevice:
		device, known := s.deps.volume(info)
		if !known {
			s.result.BoundaryDetectionAvailable = false
			return info, true
		}
		if device == s.device {
			return info, true
		}
		s.result.SkippedMounts++
	default:
		return info, true
	}
	return info, false
}

func (s *scanner) file(path string, size int64, root bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := Row{Path: path, Bytes: size, Complete: true}
	if size > math.MaxInt64-s.result.ObservedBytes {
		s.result.ObservedBytes = math.MaxInt64
		s.reasons["byte_overflow"] = true
	} else {
		s.result.ObservedBytes += size
	}
	s.result.Files = rank(s.result.Files, row, s.result.Options.Top)
	if root {
		s.result.RootFiles = rank(s.result.RootFiles, row, s.result.Options.Top)
	}
}

func (s *scanner) addBytes(total, size int64) (int64, bool) {
	if size > math.MaxInt64-total {
		s.reason("byte_overflow", false)
		return math.MaxInt64, false
	}
	return total + size, true
}

func rank(rows []Row, row Row, top int) []Row {
	i := sort.Search(len(rows), func(i int) bool {
		return row.Bytes > rows[i].Bytes || row.Bytes == rows[i].Bytes && row.Path < rows[i].Path
	})
	if i >= top {
		return rows
	}
	rows = append(rows, Row{})
	copy(rows[i+1:], rows[i:])
	rows[i] = row
	if len(rows) > top {
		rows = rows[:top]
	}
	return rows
}

func (s *scanner) issue(path string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result.ErrorCount++
	s.reasons["access_error"] = true
	issue := Issue{Path: path, Message: err.Error()}
	i := sort.Search(len(s.result.Errors), func(i int) bool {
		existing := s.result.Errors[i]
		return issue.Path < existing.Path || issue.Path == existing.Path && issue.Message < existing.Message
	})
	if i >= errorSamples {
		return
	}
	s.result.Errors = append(s.result.Errors, Issue{})
	copy(s.result.Errors[i+1:], s.result.Errors[i:])
	s.result.Errors[i] = issue
	if len(s.result.Errors) > errorSamples {
		s.result.Errors = s.result.Errors[:errorSamples]
	}
}

func (s *scanner) reason(reason string, stop bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reasons[reason] = true
	s.stopped = s.stopped || stop
}

func (s *scanner) stop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		reason := "canceled"
		if err == context.DeadlineExceeded {
			reason = "deadline_exceeded"
		}
		s.reasons[reason] = true
		s.stopped = true
	}
	if s.deps.now().Sub(s.start) >= s.result.Options.Budget {
		s.reasons["budget_exceeded"] = true
		s.stopped = true
	}
	return s.stopped
}

func (s *scanner) finish() Result {
	for _, child := range s.children {
		s.result.Directories = rank(s.result.Directories, child.row, s.result.Options.Top)
	}
	for reason := range s.reasons {
		s.result.StopReasons = append(s.result.StopReasons, reason)
	}
	sort.Strings(s.result.StopReasons)
	s.result.Complete = len(s.reasons) == 0
	return s.result
}
