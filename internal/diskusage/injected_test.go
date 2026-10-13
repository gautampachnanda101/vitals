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
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testInfo struct {
	name string
	size int64
	mode fs.FileMode
}

func (i *testInfo) Name() string       { return filepath.Base(i.name) }
func (i *testInfo) Size() int64        { return i.size }
func (i *testInfo) Mode() fs.FileMode  { return i.mode }
func (i *testInfo) ModTime() time.Time { return time.Time{} }
func (i *testInfo) IsDir() bool        { return i.mode.IsDir() }
func (i *testInfo) Sys() any           { return nil }

type testNode struct {
	info    *testInfo
	entries []string
}

type testFS struct {
	root          string
	nodes         map[string]*testNode
	stat          func(string, string) (fs.FileInfo, error)
	open          func(string) error
	reader        func(string) error
	read          func(string, int)
	readErr       map[string]error
	rootErr       error
	opened        atomic.Int64
	closed        atomic.Int64
	readers       atomic.Int64
	readerClosed  atomic.Int64
	onReaderClose func(string)
	reads         atomic.Int64
	badBatch      atomic.Bool
}

func newFS(t *testing.T) *testFS {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	f := &testFS{root: filepath.Join(cwd, "mock-root"), nodes: map[string]*testNode{}, readErr: map[string]error{}}
	f.add(".", 0, fs.ModeDir)
	return f
}

func (f *testFS) add(path string, size int64, mode fs.FileMode) {
	path = filepath.Clean(path)
	f.nodes[path] = &testNode{info: &testInfo{name: path, size: size, mode: mode}}
	if path != "." {
		parent := filepath.Dir(path)
		f.nodes[parent].entries = append(f.nodes[parent].entries, filepath.Base(path))
	}
}

func (f *testFS) deps() dependencies {
	d := liveDependencies()
	d.openRoot = func(context.Context, string) (directory, error) {
		if f.rootErr != nil {
			return nil, f.rootErr
		}
		f.opened.Add(1)
		return &testDirectory{f: f, path: "."}, nil
	}
	d.mounts = func(context.Context) ([]string, error) { return nil, nil }
	d.sameFile = func(a, b fs.FileInfo) bool { return a == b }
	d.volume = func(fs.FileInfo) (uint64, bool) { return 1, true }
	return d
}

func (f *testFS) scan(t *testing.T, modify func(*dependencies)) Result {
	t.Helper()
	deps := f.deps()
	if modify != nil {
		modify(&deps)
	}
	result, err := scanWith(context.Background(), f.root, DefaultOptions(), deps)
	if err != nil {
		t.Fatal(err)
	}
	f.assertClosed(t)
	return result
}

func (f *testFS) assertClosed(t *testing.T) {
	t.Helper()
	if f.opened.Load() != f.closed.Load() || f.readers.Load() != f.readerClosed.Load() || f.badBatch.Load() {
		t.Fatalf("unclosed handles or wrong batch: dirs %d/%d readers %d/%d bad batch %v",
			f.opened.Load(), f.closed.Load(), f.readers.Load(), f.readerClosed.Load(), f.badBatch.Load())
	}
}

type testDirectory struct {
	f      *testFS
	path   string
	closed bool
}

func (d *testDirectory) Lstat(name string) (fs.FileInfo, error) {
	if d.f.stat != nil {
		if info, err := d.f.stat(d.path, name); info != nil || err != nil {
			return info, err
		}
	}
	node := d.f.nodes[filepath.Join(d.path, name)]
	if node == nil {
		return nil, fs.ErrNotExist
	}
	return node.info, nil
}

func (d *testDirectory) OpenRoot(name string) (directory, error) {
	path := filepath.Join(d.path, name)
	if d.f.open != nil {
		if err := d.f.open(path); err != nil {
			return nil, err
		}
	}
	d.f.opened.Add(1)
	return &testDirectory{f: d.f, path: path}, nil
}

func (d *testDirectory) Reader() (directoryReader, error) {
	if d.f.reader != nil {
		if err := d.f.reader(d.path); err != nil {
			return nil, err
		}
	}
	d.f.readers.Add(1)
	return &testReader{f: d.f, path: d.path}, nil
}

func (d *testDirectory) Close() error {
	if d.closed {
		panic("double directory close")
	}
	d.closed = true
	d.f.closed.Add(1)
	return nil
}

type testReader struct {
	f      *testFS
	path   string
	offset int
	closed bool
}

func (r *testReader) ReadDir(n int) ([]fs.DirEntry, error) {
	if n != batchSize {
		r.f.badBatch.Store(true)
	}
	r.f.reads.Add(1)
	if r.f.read != nil {
		r.f.read(r.path, r.offset)
	}
	names := r.f.nodes[r.path].entries
	end := min(len(names), r.offset+n)
	entries := make([]fs.DirEntry, 0, end-r.offset)
	for _, name := range names[r.offset:end] {
		entries = append(entries, fs.FileInfoToDirEntry(r.f.nodes[filepath.Join(r.path, name)].info))
	}
	r.offset = end
	if err := r.f.readErr[r.path]; err != nil {
		return entries, err
	}
	if end == len(names) {
		return entries, io.EOF
	}
	return entries, nil
}

func (r *testReader) Close() error {
	if r.closed {
		panic("double reader close")
	}
	r.closed = true
	r.f.readerClosed.Add(1)
	if r.f.onReaderClose != nil {
		r.f.onReaderClose(r.path)
	}
	return nil
}

func TestOptions(t *testing.T) {
	if DefaultOptions() != (Options{Top: 20, Budget: 30 * time.Second}) {
		t.Fatal("defaults changed")
	}
	for _, top := range []int{1, 100} {
		for _, budget := range []time.Duration{time.Nanosecond, 5 * time.Minute} {
			if err := Validate(Options{top, budget}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, opts := range []Options{{0, time.Second}, {101, time.Second}, {1, 0}, {1, -1}, {1, 5*time.Minute + 1}} {
		if Validate(opts) == nil {
			t.Fatalf("accepted %+v", opts)
		}
		f := newFS(t)
		r, err := scanWith(context.Background(), f.root, opts, f.deps())
		if err == nil || r.Complete || f.opened.Load() != 0 || !slices.Contains(r.StopReasons, "invalid_options") {
			t.Fatalf("validation performed I/O: %+v, %v", r, err)
		}
	}
}

func TestDeterministicBoundedRankings(t *testing.T) {
	f := newFS(t)
	for i := 599; i >= 0; i-- {
		f.add(fmt.Sprintf("f%03d", i), 7, 0)
	}
	r := f.scan(t, nil)
	if !r.Complete || r.Entries != 601 || r.ObservedBytes != 4200 || len(r.Files) != 20 || len(r.RootFiles) != 20 || f.reads.Load() != 3 {
		t.Fatalf("wrong bounded batch totals: %+v", r)
	}
	for i, row := range r.Files {
		if row.Path != filepath.Join(f.root, fmt.Sprintf("f%03d", i)) {
			t.Fatalf("tie was not lexical: %+v", r.Files)
		}
	}
	if cap(r.Files) > 64 {
		t.Fatalf("unbounded ranking capacity: %d", cap(r.Files))
	}
	ranking := rank([]Row{{"z", 5, true}}, Row{"a", 5, true}, 1)
	if !reflect.DeepEqual(ranking, []Row{{"a", 5, true}}) {
		t.Fatal(ranking)
	}
	for _, top := range []int{1, 100} {
		g := newFS(t)
		for i := range 110 {
			g.add(fmt.Sprintf("f%03d", i), int64(i), 0)
		}
		opts := DefaultOptions()
		opts.Top = top
		r, err := scanWith(context.Background(), g.root, opts, g.deps())
		g.assertClosed(t)
		if err != nil || !r.Complete || len(r.Files) != top || len(r.RootFiles) != top || r.Files[0].Bytes != 109 {
			t.Fatalf("top %d: %+v %v", top, r, err)
		}
	}
}

func TestExclusionsAndMounts(t *testing.T) {
	f := newFS(t)
	f.add("link", 400, os.ModeSymlink)
	f.add("pipe", 400, os.ModeNamedPipe)
	f.add("mounted", 0, os.ModeDir)
	f.add("mounted/file", 400, 0)
	f.add("other", 0, os.ModeDir)
	f.add("other/file", 400, 0)
	f.add("good", 3, 0)
	r := f.scan(t, func(d *dependencies) {
		d.mounts = func(context.Context) ([]string, error) {
			return []string{f.root, filepath.Dir(f.root), filepath.Join(f.root, "mounted")}, nil
		}
		d.volume = func(info fs.FileInfo) (uint64, bool) {
			if info.Name() == "other" {
				return 2, true
			}
			return 1, true
		}
	})
	if !r.Complete || !r.BoundaryDetectionAvailable || r.SkippedSymlinks != 1 || r.SkippedSpecial != 1 || r.SkippedMounts != 2 || r.ObservedBytes != 3 || r.Entries != 6 {
		t.Fatalf("wrong exclusions: %+v", r)
	}
}

func TestBoundaryUnavailable(t *testing.T) {
	for _, which := range []string{"root", "child", "inventory"} {
		t.Run(which, func(t *testing.T) {
			f := newFS(t)
			f.add("file", 3, 0)
			r := f.scan(t, func(d *dependencies) {
				d.volume = func(info fs.FileInfo) (uint64, bool) {
					return 1, !(which == "root" && info.Name() == "." || which == "child" && info.Name() == "file")
				}
				if which == "inventory" {
					d.mounts = func(context.Context) ([]string, error) { return nil, errors.New("inventory unavailable") }
				}
			})
			if r.BoundaryDetectionAvailable || !r.Complete || r.ObservedBytes != 3 {
				t.Fatal(r)
			}
		})
	}
}

func TestChildFailuresAndBoundedSamples(t *testing.T) {
	f := newFS(t)
	f.add("good", 0, fs.ModeDir)
	f.add("good/file", 3, 0)
	f.add("partial", 0, fs.ModeDir)
	f.add("partial/file", 5, 0)
	f.add("partial/link", 0, fs.ModeSymlink)
	f.readErr["partial"] = errors.New("read failed")
	for i := 24; i >= 0; i-- {
		f.add(fmt.Sprintf("bad%02d", i), 1, 0)
	}
	f.stat = func(path, name string) (fs.FileInfo, error) {
		if strings.HasPrefix(name, "bad") {
			return nil, fs.ErrPermission
		}
		return nil, nil
	}
	r := f.scan(t, nil)
	if r.Complete || r.ErrorCount != 26 || len(r.Errors) != 20 || r.ObservedBytes != 8 || r.SkippedSymlinks != 1 {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(r.Directories, []Row{{filepath.Join(f.root, "partial"), 5, false}, {filepath.Join(f.root, "good"), 3, true}}) {
		t.Fatalf("per-child lower bounds incorrect: %+v", r.Directories)
	}
	for i, issue := range r.Errors {
		if issue.Path != filepath.Join(f.root, fmt.Sprintf("bad%02d", i)) || issue.Message != fs.ErrPermission.Error() {
			t.Fatal(r.Errors)
		}
	}
}

func TestDirectoryFailureStages(t *testing.T) {
	for _, stage := range []string{"open", "stat", "replace", "reader", "recheck-error", "recheck-link"} {
		t.Run(stage, func(t *testing.T) {
			f := newFS(t)
			f.add("d", 0, fs.ModeDir)
			f.add("d/file", 900, 0)
			switch stage {
			case "open":
				f.open = func(string) error { return fs.ErrPermission }
			case "stat":
				f.stat = func(path, name string) (fs.FileInfo, error) {
					if path == "d" && name == "." {
						return nil, fs.ErrNotExist
					}
					return nil, nil
				}
			case "replace":
				f.stat = func(path, name string) (fs.FileInfo, error) {
					if path == "d" && name == "." {
						return &testInfo{name: "replacement", mode: fs.ModeDir}, nil
					}
					return nil, nil
				}
			case "reader":
				f.reader = func(path string) error {
					if path == "d" {
						return fs.ErrPermission
					}
					return nil
				}
			case "recheck-error", "recheck-link":
				var checks atomic.Int64
				f.stat = func(path, name string) (fs.FileInfo, error) {
					if path == "." && name == "d" && checks.Add(1) == 2 {
						if stage == "recheck-error" {
							return nil, fs.ErrNotExist
						}
						return &testInfo{mode: fs.ModeSymlink}, nil
					}
					return nil, nil
				}
			}
			r := f.scan(t, nil)
			if r.Complete || r.ErrorCount != 1 || r.ObservedBytes != 0 || len(r.Directories) != 1 || r.Directories[0].Complete {
				t.Fatal(r)
			}
		})
	}
}

func TestByteOverflowIsAnExplicitLowerBound(t *testing.T) {
	for _, nested := range []bool{false, true} {
		f := newFS(t)
		if nested {
			f.add("d", 0, fs.ModeDir)
			f.add("d/nested", 0, fs.ModeDir)
			f.add("d/a", math.MaxInt64, 0)
			f.add("d/nested/b", 1, 0)
		} else {
			f.add("a", math.MaxInt64, 0)
			f.add("b", 1, 0)
		}
		r := f.scan(t, nil)
		if r.Complete || r.ObservedBytes != math.MaxInt64 || !slices.Contains(r.StopReasons, "byte_overflow") {
			t.Fatal(r)
		}
		if nested && (r.Directories[0].Complete || r.Directories[0].Bytes != math.MaxInt64) {
			t.Fatal(r.Directories)
		}
	}
	f := newFS(t)
	f.add("d", 0, fs.ModeDir)
	f.add("d/a", math.MaxInt64, 0)
	r := f.scan(t, nil)
	if !r.Complete || !r.Directories[0].Complete || r.ObservedBytes != math.MaxInt64 {
		t.Fatal("exact representable maximum should be complete", r)
	}
}

func TestRootFailures(t *testing.T) {
	for _, stage := range []string{"open", "stat", "reader", "read"} {
		t.Run(stage, func(t *testing.T) {
			f := newFS(t)
			switch stage {
			case "open":
				f.rootErr = fs.ErrNotExist
			case "stat":
				f.stat = func(string, string) (fs.FileInfo, error) { return nil, fs.ErrPermission }
			case "reader":
				f.reader = func(string) error { return fs.ErrPermission }
			case "read":
				f.readErr["."] = fs.ErrPermission
			}
			r, err := scanWith(context.Background(), f.root, DefaultOptions(), f.deps())
			f.assertClosed(t)
			if err == nil || r.Complete || r.ErrorCount != 1 || !slices.Contains(r.StopReasons, "root_error") {
				t.Fatalf("silent root failure %+v, %v", r, err)
			}
		})
	}
}

func TestLimits(t *testing.T) {
	if liveDependencies().limits != (scanLimits{1_000_000, 10_000, 128}) || workerCount != 6 || queueSize != 64 || batchSize != 256 {
		t.Fatal("production bounds changed")
	}
	t.Run("entries", func(t *testing.T) {
		f := newFS(t)
		for i := range 6 {
			f.add(fmt.Sprintf("f%d", i), 1, 0)
		}
		r := f.scan(t, func(d *dependencies) { d.limits.entries = 4 })
		if r.Entries != 4 || r.ObservedBytes != 3 || r.Complete || !slices.Contains(r.StopReasons, "entry_limit") {
			t.Fatal(r)
		}
	})
	t.Run("exact entry boundary", func(t *testing.T) {
		f := newFS(t)
		f.add("f", 1, 0)
		r := f.scan(t, func(d *dependencies) { d.limits.entries = 2 })
		if !r.Complete || r.Entries != 2 {
			t.Fatal(r)
		}
	})
	t.Run("states", func(t *testing.T) {
		f := newFS(t)
		for i := range 12 {
			f.add(fmt.Sprintf("d%d", i), 0, fs.ModeDir)
		}
		r := f.scan(t, func(d *dependencies) { d.limits.children = 3 })
		if r.Complete || len(r.Directories) != 3 || !slices.Contains(r.StopReasons, "root_state_limit") {
			t.Fatal(r)
		}
	})
	t.Run("depth", func(t *testing.T) {
		f := newFS(t)
		f.add("d", 0, fs.ModeDir)
		f.add("d/file", 3, 0)
		f.add("d/deep", 0, fs.ModeDir)
		f.add("d/deep/file", 400, 0)
		r := f.scan(t, func(d *dependencies) { d.limits.depth = 1 })
		if r.Complete || r.ObservedBytes != 3 || r.Directories[0].Bytes != 3 || r.Directories[0].Complete || !slices.Contains(r.StopReasons, "depth_limit") {
			t.Fatal(r)
		}
	})
	t.Run("128 allowed", func(t *testing.T) {
		f := newFS(t)
		path := "."
		for range 128 {
			path = filepath.Join(path, "d")
			f.add(path, 0, fs.ModeDir)
		}
		f.add(filepath.Join(path, "file"), 5, 0)
		r := f.scan(t, nil)
		if !r.Complete || r.ObservedBytes != 5 {
			t.Fatal(r)
		}
		if f.opened.Load() != 129 {
			t.Fatal("incorrect depth-first opens")
		}
	})
	t.Run("129 rejected", func(t *testing.T) {
		f := newFS(t)
		path := "."
		for range 129 {
			path = filepath.Join(path, "d")
			f.add(path, 0, fs.ModeDir)
		}
		f.add(filepath.Join(path, "file"), 5, 0)
		r := f.scan(t, nil)
		if r.Complete || r.ObservedBytes != 0 || r.Directories[0].Complete || r.Entries != 130 || !slices.Contains(r.StopReasons, "depth_limit") {
			t.Fatal(r)
		}
		if f.opened.Load() != 129 {
			t.Fatal("directory beyond production depth opened")
		}
	})
	t.Run("production state boundary", func(t *testing.T) {
		f := newFS(t)
		for i := range 10_001 {
			f.add(fmt.Sprintf("d%05d", i), 0, fs.ModeDir)
		}
		r := f.scan(t, nil)
		if r.Complete || !slices.Contains(r.StopReasons, "root_state_limit") || r.Entries != 10_002 {
			t.Fatal(r)
		}
	})
}

func TestEmptyAndHardlinks(t *testing.T) {
	f := newFS(t)
	r := f.scan(t, nil)
	if !r.Complete || r.Entries != 1 || r.ObservedBytes != 0 || r.Files == nil || r.Errors == nil || r.StopReasons == nil {
		t.Fatal(r)
	}
	root := fixture(t, map[string]string{"first": "abc"})
	if err := os.Link(filepath.Join(root, "first"), filepath.Join(root, "second")); err != nil {
		t.Fatal(err)
	}
	live, err := Scan(context.Background(), root, DefaultOptions())
	if err != nil || !live.Complete || live.ObservedBytes != 6 || len(live.Files) != 2 {
		t.Fatalf("must count logical bytes per hardlinked path: %+v, %v", live, err)
	}
}
