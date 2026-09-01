package dupes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestScanFindsIdenticalFilesAcrossDirs(t *testing.T) {
	root := t.TempDir()
	content := []byte("the quick brown fox jumps over the lazy dog, twice over")
	a := write(t, root, "a/one.txt", content)
	b := write(t, root, "b/two.txt", content)
	write(t, root, "c/unique.txt", []byte("nothing else looks like this at all, ever"))

	r, err := Scan(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 {
		t.Fatalf("groups = %d, want 1: %+v", len(r.Groups), r.Groups)
	}
	g := r.Groups[0]
	if g.SizeBytes != int64(len(content)) {
		t.Errorf("group size = %d, want %d", g.SizeBytes, len(content))
	}
	if len(g.Paths) != 2 {
		t.Fatalf("group paths = %v, want exactly [a, b]", g.Paths)
	}
	got := map[string]bool{g.Paths[0]: true, g.Paths[1]: true}
	if !got[a] || !got[b] {
		t.Errorf("group paths = %v, want %v and %v", g.Paths, a, b)
	}
	if want := int64(len(content)); g.WastedBytes() != want {
		t.Errorf("WastedBytes = %d, want %d (one extra copy)", g.WastedBytes(), want)
	}
}

func TestScanIgnoresFilesBelowMinSize(t *testing.T) {
	root := t.TempDir()
	content := []byte("short")
	write(t, root, "a.txt", content)
	write(t, root, "b.txt", content)

	r, err := Scan(root, int64(len(content))+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 0 {
		t.Errorf("expected no groups below the size floor, got %+v", r.Groups)
	}
	if r.ScannedFiles != 0 {
		t.Errorf("expected 0 scanned files below the size floor, got %d", r.ScannedFiles)
	}
}

func TestScanSameSizeDifferentContentIsNotADuplicate(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.bin", []byte("AAAAAAAAAA"))
	write(t, root, "b.bin", []byte("BBBBBBBBBB"))

	r, err := Scan(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 0 {
		t.Errorf("same-size, different-content files should not group as duplicates: %+v", r.Groups)
	}
	if r.ScannedFiles != 2 {
		t.Errorf("both files should still count as scanned, got %d", r.ScannedFiles)
	}
}

func TestScanSkipsJunkDirectoriesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	content := []byte("duplicate-me-please-thanks")
	real := write(t, root, "keep/one.txt", content)
	write(t, root, "node_modules/two.txt", content) // must not be reported

	if runtime.GOOS != "windows" { // symlink creation needs elevation on Windows
		link := filepath.Join(root, "link.txt")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
	}

	r, err := Scan(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 0 {
		t.Errorf("the only real duplicate lives under node_modules and should be skipped: %+v", r.Groups)
	}
}

func TestWriteResultToFileWritesValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dupes.json")
	r := Result{Root: "/x", ScannedFiles: 3, Groups: []Group{{SizeBytes: 10, Hash: "abc", Paths: []string{"a", "b"}}}, WastedBytes: 10}

	if err := writeResultToFile(path, r); err != nil {
		t.Fatalf("writeResultToFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the file (including its nested dir) to exist: %v", err)
	}
	var got Result
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}
	if got.ScannedFiles != 3 || got.WastedBytes != 10 || len(got.Groups) != 1 {
		t.Errorf("round-tripped result = %+v, want a match for %+v", got, r)
	}
}

func TestGroupWastedBytesKeepsOneCopyFree(t *testing.T) {
	g := Group{SizeBytes: 100, Paths: []string{"a", "b", "c"}}
	if want := int64(200); g.WastedBytes() != want {
		t.Errorf("WastedBytes = %d, want %d (3 copies, 1 kept)", g.WastedBytes(), want)
	}
}
