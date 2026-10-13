package diskusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type generatedDirectory struct {
	directory
	offset int
}

func (d *generatedDirectory) Lstat(name string) (fs.FileInfo, error) {
	if name == "." {
		return d.directory.Lstat(name)
	}
	return &testInfo{name: name, size: 1}, nil
}

func (d *generatedDirectory) Reader() (directoryReader, error) { return d, nil }

func (d *generatedDirectory) ReadDir(n int) ([]fs.DirEntry, error) {
	if n != 256 {
		panic("unbounded enumeration")
	}
	entries := make([]fs.DirEntry, 0, n)
	for range n {
		entries = append(entries, fs.FileInfoToDirEntry(&testInfo{name: fmt.Sprintf("f%07d", d.offset)}))
		d.offset++
	}
	return entries, nil
}

// Reader and directory ownership are separate on real filesystems; this
// generator has no resources to close.
func (*generatedDirectory) Close() error { return nil }

func TestProductionEntryCapWithoutWholeListAllocation(t *testing.T) {
	f := newFS(t)
	d := f.deps()
	d.openRoot = func(context.Context, string) (directory, error) {
		return &generatedDirectory{directory: &testDirectory{f: f, path: "."}}, nil
	}
	opts := DefaultOptions()
	opts.Budget = 5 * time.Minute
	r, err := scanWith(context.Background(), f.root, opts, d)
	if err != nil || r.Complete || r.Entries != 1_000_000 || r.ObservedBytes != 999_999 || !slices.Contains(r.StopReasons, "entry_limit") || len(r.Files) != 20 || cap(r.Files) > 64 {
		t.Fatalf("production entry limit/ranking violated: %+v %v", r, err)
	}
}

func TestJSONContractAndRawPaths(t *testing.T) {
	want := map[reflect.Type][]string{
		reflect.TypeOf(Options{}): {"top", "budget"},
		reflect.TypeOf(Row{}):     {"path", "bytes", "complete"},
		reflect.TypeOf(Issue{}):   {"path", "message"},
		reflect.TypeOf(Result{}): {"root", "options", "directories", "files", "root_files", "observed_bytes",
			"entries", "elapsed_ms", "complete", "stop_reasons", "skipped_symlinks", "skipped_special",
			"skipped_mounts", "boundary_detection_available", "error_count", "errors"},
	}
	for typ, fields := range want {
		if typ.NumField() != len(fields) {
			t.Fatalf("%s field count changed", typ)
		}
		for i, tag := range fields {
			if got := typ.Field(i).Tag.Get("json"); got != tag {
				t.Fatalf("%s field %d has tag %q, want %q", typ, i, got, tag)
			}
		}
	}
	f := newFS(t)
	f.add("raw\u202e-name", 3, 0)
	r := f.scan(t, nil)
	path := filepath.Join(f.root, "raw\u202e-name")
	if r.Files[0].Path != path || DisplayPath(r.Files[0].Path) == path {
		t.Fatal("display escaping corrupted the raw path")
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got Result
	if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(r, got) || strings.Contains(string(data), ":null") {
		t.Fatalf("JSON round trip changed API: %s %v", data, err)
	}
}

type failedReader struct{}

func (failedReader) ReadDir(int) ([]fs.DirEntry, error) { return nil, io.ErrUnexpectedEOF }
func (failedReader) Close() error                       { return nil }

func TestAlreadyStoppedReadAndWalk(t *testing.T) {
	f := newFS(t)
	s := &scanner{ctx: context.Background(), deps: f.deps(), stopped: true,
		reasons: map[string]bool{}, start: f.deps().now(), result: Result{Options: DefaultOptions()}}
	if complete, err := s.read(failedReader{}, f.root, func(fs.DirEntry) bool { panic("visited stopped reader") }); complete || err != nil {
		t.Fatalf("%v %v", complete, err)
	}
	if bytes, complete := s.walk(nil, "", "", nil, 1); bytes != 0 || complete {
		t.Fatalf("%d %v", bytes, complete)
	}
	// Keep a full sample set, then insert an earlier sample: total count grows
	// while retained samples remain bounded and deterministic.
	for i := range 21 {
		s.issue(fmt.Sprintf("z%02d", i), io.ErrUnexpectedEOF)
	}
	s.issue("a", io.ErrUnexpectedEOF)
	if len(s.result.Errors) != 20 || s.result.ErrorCount != 22 || s.result.Errors[0].Path != "a" {
		t.Fatal(s.result.Errors)
	}
}
