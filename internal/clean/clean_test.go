package clean

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mktree builds a fixed 3-file / 600-byte directory tree and returns its root.
func mktree(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(p string, n int) {
		if err := os.WriteFile(filepath.Join(d, p), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("f1", 100)
	write(filepath.Join("a", "f2"), 200)
	write(filepath.Join("a", "b", "f3"), 300)
	return d
}

func TestTreeStat(t *testing.T) {
	files, bytes := treeStat(mktree(t))
	if files != 3 || bytes != 600 {
		t.Errorf("treeStat = (%d files, %d bytes), want (3, 600)", files, bytes)
	}
}

func TestRemoveTreeDryRunMeasuresButKeeps(t *testing.T) {
	d := mktree(t)
	sub := filepath.Join(d, "a")
	got, err := removeTree(sub, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Errorf("size = %d, want 500", got)
	}
	if _, err := os.Stat(filepath.Join(sub, "b", "f3")); err != nil {
		t.Errorf("dry-run deleted a file: %v", err)
	}
}

func TestRemoveTreeReal(t *testing.T) {
	d := mktree(t)
	sub := filepath.Join(d, "a")
	got, err := removeTree(sub, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Errorf("size = %d, want 500", got)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Errorf("real run left the tree: %v", err)
	}
}

func TestPurgeContentsRemovesEntriesKeepsDir(t *testing.T) {
	d := mktree(t)
	r := &runner{opts: Options{}}
	r.purgeContents(d)

	entries, err := os.ReadDir(d)
	if err != nil {
		t.Fatalf("dir itself was removed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entries left, want 0", len(entries))
	}
	if r.freedByRM.Load() != 600 {
		t.Errorf("accounted %d bytes, want 600", r.freedByRM.Load())
	}
}

func TestPurgeContentsDryRunTouchesNothing(t *testing.T) {
	d := mktree(t)
	r := &runner{opts: Options{DryRun: true}}
	r.purgeContents(d)

	if files, bytes := treeStat(d); files != 3 || bytes != 600 {
		t.Errorf("dry-run modified the tree: now (%d files, %d bytes)", files, bytes)
	}
	if r.freedByRM.Load() != 600 {
		t.Errorf("dry-run should still measure 600 bytes, got %d", r.freedByRM.Load())
	}
}

func TestPurgeContentsMissingDirIsNoop(t *testing.T) {
	r := &runner{opts: Options{}}
	r.purgeContents(filepath.Join(t.TempDir(), "does-not-exist"))
	r.purgeContents("")
	if r.freedByRM.Load() != 0 {
		t.Errorf("accounted %d bytes for nonexistent dirs", r.freedByRM.Load())
	}
}

func TestMeasureDirsRanksLargestFirstAndSkipsMissing(t *testing.T) {
	big := mktree(t)                           // 600 bytes
	small := t.TempDir()                       // exists, empty -> 0 bytes, excluded
	missing := filepath.Join(t.TempDir(), "x") // never created

	entries, complete := measureDirs([]string{small, missing, big}, time.Second)
	if !complete {
		t.Fatalf("expected a generous budget to finish the scan")
	}
	if len(entries) != 1 || entries[0].Path != big || entries[0].Bytes != 600 {
		t.Fatalf("entries = %+v, want exactly {%s, 600}", entries, big)
	}
}

func TestMeasureDirsRespectsBudget(t *testing.T) {
	dirs := make([]string, 5)
	for i := range dirs {
		dirs[i] = mktree(t)
	}
	_, complete := measureDirs(dirs, 0)
	if complete {
		t.Errorf("a zero budget should not be able to reach every directory")
	}
}
