package clean

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// isUnder reports whether path is logically under root, comparing with
// forward slashes so the check doesn't depend on which OS actually ran
// filepath.Join to build path — on Windows, filepath.Join("/home/x",
// ".cache") normalizes to `\home\x\.cache`, which a literal
// strings.HasPrefix(d, "/home/x") would miss even though it's still
// correctly under home.
func isUnder(path, root string) bool {
	return strings.HasPrefix(filepath.ToSlash(path), filepath.ToSlash(root))
}

func TestDevCacheDirsAreUnderHome(t *testing.T) {
	dirs := devCacheDirs("/home/x")
	if len(dirs) == 0 {
		t.Fatal("devCacheDirs returned nothing")
	}
	for _, d := range dirs {
		if !isUnder(d, "/home/x") {
			t.Errorf("devCacheDirs entry %q is not under the given home", d)
		}
	}
}

func TestOSCacheDirsPerPlatform(t *testing.T) {
	if got := osCacheDirs("linux", "/home/x"); len(got) == 0 {
		t.Error("osCacheDirs(linux, ...) returned nothing")
	}
	darwin := osCacheDirs("darwin", "/Users/x")
	if len(darwin) == 0 {
		t.Fatal("osCacheDirs(darwin, ...) returned nothing")
	}
	for _, d := range darwin {
		if !isUnder(d, "/Users/x") {
			t.Errorf("osCacheDirs(darwin) entry %q is not under home", d)
		}
	}
	if got := osCacheDirs("plan9", "/home/x"); got != nil {
		t.Errorf("osCacheDirs(unknown goos) = %v, want nil", got)
	}
}

func TestOSCacheDirsWindowsUsesEnvVars(t *testing.T) {
	t.Setenv("TEMP", `C:\Users\x\Temp`)
	t.Setenv("TMP", "")
	t.Setenv("LOCALAPPDATA", "")
	got := osCacheDirs("windows", `C:\Users\x`)
	if len(got) != 1 || got[0] != `C:\Users\x\Temp` {
		t.Errorf("osCacheDirs(windows, ...) = %v, want just $TEMP with LOCALAPPDATA unset", got)
	}
}

func TestOSCacheDirsWindowsIncludesAppCachesWhenLocalAppDataIsSet(t *testing.T) {
	t.Setenv("TEMP", "")
	t.Setenv("TMP", "")
	t.Setenv("LOCALAPPDATA", `C:\Users\x\AppData\Local`)
	got := osCacheDirs("windows", `C:\Users\x`)
	if len(got) < 5 {
		t.Fatalf("osCacheDirs(windows, LOCALAPPDATA set) = %v, want the Chrome/Edge/INetCache/Explorer/Temp entries", got)
	}
	joined := strings.Join(got, "|")
	for _, want := range []string{"Chrome", "Edge", "INetCache", "Explorer"} {
		if !strings.Contains(joined, want) {
			t.Errorf("osCacheDirs(windows, LOCALAPPDATA set) missing %q entry, got: %v", want, got)
		}
	}
}

func TestOSCacheDirsWindowsWithNoEnvVarsSetIsEmpty(t *testing.T) {
	t.Setenv("TEMP", "")
	t.Setenv("TMP", "")
	t.Setenv("LOCALAPPDATA", "")
	if got := osCacheDirs("windows", `C:\Users\x`); got != nil {
		t.Errorf("osCacheDirs(windows, no env vars) = %v, want nil", got)
	}
}

func TestFreeSpaceRootIsSlashOnUnix(t *testing.T) {
	if got := freeSpaceRoot("linux", ""); got != "/" {
		t.Errorf("freeSpaceRoot(linux) = %q, want /", got)
	}
	if got := freeSpaceRoot("darwin", ""); got != "/" {
		t.Errorf("freeSpaceRoot(darwin) = %q, want /", got)
	}
}

// There is deliberately no test asserting freeSpaceRoot("windows",
// `D:\Windows`) == `D:\` — filepath.VolumeName is itself platform-aware
// in the Go standard library (it only parses a drive letter when actually
// running on Windows; elsewhere it always returns ""), so that specific
// branch can only be verified running on a real Windows host, not on the
// Linux CI job the coverage gate runs on. The two branches below don't
// depend on that OS-specific stdlib behavior and are fully portable.

func TestFreeSpaceRootOnWindowsFallsBackToCWhenSystemRootUnset(t *testing.T) {
	if got := freeSpaceRoot("windows", ""); got != `C:\` {
		t.Errorf("freeSpaceRoot(windows, \"\") = %q, want C:\\ as a fallback", got)
	}
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

func TestPurgeContentsRefusesASymlinkedDir(t *testing.T) {
	// If one of the fixed cache paths (~/.cache, say) has been replaced with
	// a symlink to somewhere else — by malware, or a careless script that
	// already has write access to $HOME — purgeContents must not walk into
	// the link target and delete it. Only a real directory at `dir` is
	// eligible for purging.
	real := mktree(t)
	link := filepath.Join(t.TempDir(), "cache-symlink")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	r := &runner{opts: Options{}}
	r.purgeContents(link)

	if files, bytes := treeStat(real); files != 3 || bytes != 600 {
		t.Errorf("purgeContents followed the symlink and modified its target: now (%d files, %d bytes)", files, bytes)
	}
	if r.freedByRM.Load() != 0 {
		t.Errorf("accounted %d bytes for a symlinked dir, want 0 — it should have been refused, not walked", r.freedByRM.Load())
	}
}

func TestPurgeContentsRefusesAFile(t *testing.T) {
	// dir is always expected to be a directory (one of the fixed cache
	// paths); if it's ever a plain file instead, ReadDir would already fail
	// gracefully, but this pins that a non-directory is never treated as
	// purgeable.
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &runner{opts: Options{}}
	r.purgeContents(f)
	if r.freedByRM.Load() != 0 {
		t.Errorf("accounted %d bytes for a plain file, want 0", r.freedByRM.Load())
	}
	if _, err := os.Stat(f); err != nil {
		t.Errorf("the file itself should have been left alone: %v", err)
	}
}

func TestPurgeContentsDoesNotCreditBytesWhenRemovalActuallyFails(t *testing.T) {
	// Raised by review while designing roadmap item 005 (dashboard write
	// actions): removeTree accumulates a file's size into its return value
	// before attempting os.Remove, and discards that call's error — read in
	// isolation, that looks like it could inflate the reported freed-bytes
	// count on a permission-denied file. It doesn't, in the one path that
	// actually calls it (purgeContents): removeTree deletes deepest-first,
	// so a file os.Remove can't delete leaves its parent directory
	// non-empty, which makes the directory's own os.Remove fail too — all
	// the way up to the top-level entry purgeContents just tried to purge.
	// purgeContents re-checks that top-level entry with its own os.Lstat
	// afterward and only credits freedByRM if it's actually gone. This
	// pins that real behavior with a genuine permission failure, not just
	// reasoning about the code.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply the same way on Windows")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked-file")
	if err := os.WriteFile(locked, make([]byte, 100), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Removing a file needs write+exec on its *parent* directory, not the
	// file's own mode — lock the parent to force the real os.Remove call
	// inside removeTree to fail with a permission error.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	r := &runner{opts: Options{}}
	r.purgeContents(dir)

	if got := r.freedByRM.Load(); got != 0 {
		t.Errorf("freedByRM = %d, want 0 — removal genuinely failed and must not be credited", got)
	}
	if _, err := os.Stat(locked); err != nil {
		t.Errorf("the locked file should still exist (removal should have failed), but os.Stat: %v", err)
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
