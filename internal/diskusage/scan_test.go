package diskusage

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

type metadata struct {
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

func treeMetadata(t *testing.T, root string) map[string]metadata {
	t.Helper()
	result := make(map[string]metadata)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		modTime := info.ModTime()
		if info.IsDir() && runtime.GOOS == "windows" {
			// On Windows NTFS, directory ModTime can shift slightly as child file handles open and close.
			modTime = time.Time{}
		}
		result[path] = metadata{info.Size(), info.Mode(), modTime}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir, err := os.MkdirTemp(".", ".fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// On Windows, background NTFS directory modification time updates can settle asynchronously
	// right after creating files. Wait briefly so ModTime is stable before recording 'before'.
	time.Sleep(100 * time.Millisecond)
	return root
}

func TestExactFixture(t *testing.T) {
	root := fixture(t, map[string]string{"a/nested/x": "12345", "a/z": "123", "b/y": "12345678", "root": "1234", "zero": ""})
	before := treeMetadata(t, root)
	result, err := Scan(context.Background(), root, Options{Top: 2, Budget: DefaultOptions().Budget})
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservedBytes != 20 || result.Entries != 9 || !result.Complete {
		t.Fatalf("want exact 20 bytes / 9 entries / complete; got %+v", result)
	}
	wantDirs := []Row{{filepath.Join(root, "a"), 8, true}, {filepath.Join(root, "b"), 8, true}}
	wantFiles := []Row{{filepath.Join(root, "b/y"), 8, true}, {filepath.Join(root, "a/nested/x"), 5, true}}
	wantRoot := []Row{{filepath.Join(root, "root"), 4, true}, {filepath.Join(root, "zero"), 0, true}}
	if !reflect.DeepEqual(result.Directories, wantDirs) || !reflect.DeepEqual(result.Files, wantFiles) || !reflect.DeepEqual(result.RootFiles, wantRoot) {
		t.Fatalf("unexpected rankings: %+v", result)
	}
	if after := treeMetadata(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("scan changed tree paths/size/mode/mtime:\nbefore: %+v\nafter: %+v", before, after)
	}
	for path, want := range map[string]string{"a/nested/x": "12345", "a/z": "123", "b/y": "12345678", "root": "1234", "zero": ""} {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(got) != want {
			t.Fatalf("scan mutated %s: %q, %v", path, got, err)
		}
	}
}
