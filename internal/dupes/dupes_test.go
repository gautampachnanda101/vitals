package dupes

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// captureStdout swaps both os.Stdout and os.Stderr for the duration of f
// and returns everything written to either — render() prints directly
// rather than building a string first, and some of vitals' own print
// helpers (ui.Warnf) write to stderr specifically. Matches the pattern
// used in internal/monitor and internal/memcheck. Drained concurrently,
// not after f returns — a synchronous write bigger than Windows' small
// default pipe buffer deadlocks otherwise.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w

	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()

	f()
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-done
}

func TestRenderNoGroupsIsFriendly(t *testing.T) {
	out := captureStdout(t, func() {
		render(Result{Root: "/x", ScannedFiles: 5, ScannedBytes: 100}, 10)
	})
	if !strings.Contains(out, "no duplicate files found") {
		t.Errorf("render with no groups should say so plainly, got: %s", out)
	}
}

func TestRenderListsGroupsUpToTopAndCountsTheRest(t *testing.T) {
	groups := []Group{
		{SizeBytes: 100, Hash: "a", Paths: []string{"/a1", "/a2"}},
		{SizeBytes: 200, Hash: "b", Paths: []string{"/b1", "/b2"}},
		{SizeBytes: 300, Hash: "c", Paths: []string{"/c1", "/c2"}},
	}
	out := captureStdout(t, func() {
		render(Result{Root: "/x", ScannedFiles: 10, ScannedBytes: 1000, Groups: groups, WastedBytes: 600}, 2)
	})
	if !strings.Contains(out, "/a1") || !strings.Contains(out, "/b1") {
		t.Errorf("render should list the first 2 groups (top=2), got: %s", out)
	}
	if strings.Contains(out, "/c1") {
		t.Errorf("render should not list the 3rd group when top=2, got: %s", out)
	}
	if !strings.Contains(out, "1 more group") {
		t.Errorf("render should report the 1 remaining group, got: %s", out)
	}
	if !strings.Contains(out, "nothing was deleted") {
		t.Errorf("render should always print the never-deletes-automatically reminder, got: %s", out)
	}
}

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

func TestWriteResultToFileErrorsWhenTheParentCannotBeCreated(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// blocker is a file, not a dir — MkdirAll(blocker/sub, ...) must fail.
	if err := writeResultToFile(filepath.Join(blocker, "sub", "result.json"), Result{}); err == nil {
		t.Error("writeResultToFile should fail when its parent directory can't be created")
	}
}

func TestWriteResultToFileErrorsWhenPathIsADirectory(t *testing.T) {
	dir := t.TempDir() // os.Create on an existing directory always fails
	if err := writeResultToFile(dir, Result{}); err == nil {
		t.Error("writeResultToFile should fail when path is itself a directory")
	}
}

func TestHashPrefixAndHashFileErrorOnAMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := hashPrefix(missing, 64<<10); err == nil {
		t.Error("hashPrefix should error when the file can't be opened")
	}
	if _, err := hashFile(missing); err == nil {
		t.Error("hashFile should error when the file can't be opened")
	}
}

func TestGroupWastedBytesKeepsOneCopyFree(t *testing.T) {
	g := Group{SizeBytes: 100, Paths: []string{"a", "b", "c"}}
	if want := int64(200); g.WastedBytes() != want {
		t.Errorf("WastedBytes = %d, want %d (3 copies, 1 kept)", g.WastedBytes(), want)
	}
}

// writeDupFixture creates two byte-identical files (above minSize) under
// dir, returning their paths — the same shape TestScanFindsIdenticalFilesAcrossDirs
// already uses for a real Scan.
func writeDupFixture(t *testing.T, dir string) (a, b string) {
	t.Helper()
	content := strings.Repeat("x", 2<<20) // above the 1 MiB default minSize
	a, b = filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return a, b
}

func TestRunUsesHomeDirWhenRootIsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeDupFixture(t, dir)
	d := deps{homeDir: func() (string, error) { return dir, nil }}

	out := captureStdout(t, func() {
		if err := run(d, Options{}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "2.00 MB") { // one group's wasted bytes, in the printed report
		t.Errorf("run() with empty Root should scan the injected home dir, got:\n%s", out)
	}
}

func TestRunErrorsWhenHomeDirFails(t *testing.T) {
	d := deps{homeDir: func() (string, error) { return "", errors.New("no home") }}
	if err := run(d, Options{}); err == nil || !strings.Contains(err.Error(), "cannot determine home directory") {
		t.Errorf("run() = %v, want a cannot-determine-home-directory error", err)
	}
}

func TestRunAppliesHardlinksWhenYesIsSet(t *testing.T) {
	dir := t.TempDir()
	a, b := writeDupFixture(t, dir)
	d := deps{homeDir: func() (string, error) { return dir, nil }}

	out := captureStdout(t, func() {
		if err := run(d, Options{Root: dir, Hardlink: true, Yes: true}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "hardlinked 1 file(s)") {
		t.Errorf("run() with Hardlink+Yes should report the real link, got:\n%s", out)
	}
	fa, _ := os.Stat(a)
	fb, _ := os.Stat(b)
	if !os.SameFile(fa, fb) {
		t.Error("run() with Hardlink+Yes should have actually hardlinked the duplicate pair")
	}
}

func TestRunHardlinkPromptsAndAbortsOnNo(t *testing.T) {
	dir := t.TempDir()
	a, b := writeDupFixture(t, dir)
	d := deps{homeDir: func() (string, error) { return dir, nil }, confirmReader: strings.NewReader("n\n")}

	out := captureStdout(t, func() {
		if err := run(d, Options{Root: dir, Hardlink: true}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	if !strings.Contains(out, "aborted") {
		t.Errorf("run() with Hardlink, Yes=false, a 'n' answer should report aborted, got:\n%s", out)
	}
	fa, _ := os.Stat(a)
	fb, _ := os.Stat(b)
	if os.SameFile(fa, fb) {
		t.Error("an aborted confirmation must not have linked anything")
	}
}

func TestConfirmHardlinkParsesYesNoAndEOF(t *testing.T) {
	cases := map[string]bool{"y\n": true, "yes\n": true, "Y\n": true, "n\n": false, "\n": false, "": false}
	for in, want := range cases {
		out := captureStdout(t, func() {
			if got := confirmHardlink(strings.NewReader(in)); got != want {
				t.Errorf("confirmHardlink(%q) = %v, want %v", in, got, want)
			}
		})
		if !strings.Contains(out, "Continue?") {
			t.Errorf("confirmHardlink should print the prompt, got:\n%s", out)
		}
	}
}

func TestRunWritesOutputFileAndEmitsJSON(t *testing.T) {
	dir := t.TempDir()
	writeDupFixture(t, dir)
	outFile := filepath.Join(t.TempDir(), "result.json")
	d := deps{homeDir: func() (string, error) { return dir, nil }}

	out := captureStdout(t, func() {
		if err := run(d, Options{Root: dir, JSON: true, Output: outFile}); err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	var r Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("run() --json output is not valid JSON: %v\n%s", err, out)
	}
	if len(r.Groups) != 1 {
		t.Errorf("expected 1 duplicate group in the JSON output, got %+v", r)
	}
	if _, err := os.Stat(outFile); err != nil {
		t.Errorf("run() with Output set should have written the file: %v", err)
	}
}

func TestRunWarnsButDoesNotFailWhenTheOutputFileCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	writeDupFixture(t, dir)
	d := deps{homeDir: func() (string, error) { return dir, nil }}

	out := captureStdout(t, func() {
		// Output is itself an existing directory — os.Create must fail;
		// run() should warn, not abort the whole scan over it.
		if err := run(d, Options{Root: dir, Output: dir}); err != nil {
			t.Fatalf("run() should not fail just because --output couldn't be written: %v", err)
		}
	})
	if !strings.Contains(out, "warning: could not write --output file") {
		t.Errorf("run() should print a warning for the failed --output write, got:\n%s", out)
	}
}

func TestApplyHardlinksWithConfirmationPrintsAFailure(t *testing.T) {
	// A Result built by hand (no real Scan needed) whose kept file doesn't
	// exist — ApplyHardlinks' own failure-reporting is already covered by
	// TestApplyHardlinksReportsAMissingKeptFileAsAFailure; this covers the
	// caller's own job of actually printing that failure via ui.Warnf.
	d := deps{}
	result := Result{Groups: []Group{{SizeBytes: 10, Paths: []string{"/nonexistent-keep", "/nonexistent-dup"}}}}
	out := captureStdout(t, func() {
		if err := applyHardlinksWithConfirmation(d, result, true); err != nil {
			t.Fatalf("applyHardlinksWithConfirmation: %v", err)
		}
	})
	if !strings.Contains(out, "hardlinked 0 file(s)") {
		t.Errorf("expected 0 successful links, got:\n%s", out)
	}
	if !strings.Contains(out, "/nonexistent-dup") {
		t.Errorf("the failure should name the path that couldn't be linked, got:\n%s", out)
	}
}

func TestPublicRunGoesThroughTheRealHomeDir(t *testing.T) {
	// One real end-to-end call through defaultDeps — the same
	// memcheck/monitor-style exercise of the actual wiring, not just the
	// injected-fake paths above. A real (probably empty-of-duplicates)
	// scan of the real home dir; this only proves Run() doesn't error.
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	out := captureStdout(t, func() {
		if err := Run(Options{MinSize: 1 << 30}); err != nil { // absurdly high minSize keeps this fast
			t.Fatalf("Run: %v", err)
		}
	})
	if !strings.Contains(out, "no duplicate files found") {
		t.Errorf("Run() against an empty temp home should report no duplicates, got:\n%s", out)
	}
}
