package dashboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withFakeDiskUsageCache(t *testing.T, fn func() (diskScanResult, error)) {
	t.Helper()
	old := defaultDiskUsageCache
	defaultDiskUsageCache = &diskUsageCache{ttl: time.Hour, scan: fn}
	t.Cleanup(func() { defaultDiskUsageCache = old })
}

// writeFile makes a file of exactly n bytes at root/rel, creating dirs.
func writeSizedFile(t *testing.T, root, rel string, n int) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanBiggestPathsRanksChildrenAndFiles(t *testing.T) {
	root := t.TempDir()
	writeSizedFile(t, root, "big/huge.bin", 5000)
	writeSizedFile(t, root, "big/nested/more.bin", 1000)
	writeSizedFile(t, root, "small/tiny.txt", 10)
	writeSizedFile(t, root, "loose.dat", 3000) // a file directly in root

	res := scanBiggestPaths(root)

	if len(res.Dirs) == 0 || filepath.Base(res.Dirs[0].Path) != "big" {
		t.Fatalf("expected 'big' as the largest child, got %+v", res.Dirs)
	}
	if res.Dirs[0].Size != 6000 {
		t.Errorf("'big' size = %d, want 6000 (recursive)", res.Dirs[0].Size)
	}
	if len(res.Files) == 0 || filepath.Base(res.Files[0].Path) != "huge.bin" || res.Files[0].Size != 5000 {
		t.Errorf("largest file wrong: %+v", res.Files)
	}
	// the loose file in root is counted as its own "child" entry
	var sawLoose bool
	for _, d := range res.Dirs {
		if filepath.Base(d.Path) == "loose.dat" && d.Size == 3000 {
			sawLoose = true
		}
	}
	if !sawLoose {
		t.Errorf("a large file directly in root should appear as its own row, got %+v", res.Dirs)
	}
}

func TestScanBiggestPathsHandlesMissingRoot(t *testing.T) {
	res := scanBiggestPaths(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(res.Dirs) != 0 || len(res.Files) != 0 {
		t.Errorf("missing root should yield an empty result, got %+v", res)
	}
}

func TestBiggestPathsSectionRendersBothTables(t *testing.T) {
	withFakeDiskUsageCache(t, func() (diskScanResult, error) {
		return diskScanResult{
			Root:      "/home/u",
			Dirs:      []pathSize{{"/home/u/Library", 18 << 30}},
			Files:     []pathSize{{"/home/u/Library/big.vmdk", 10 << 30}},
			Entries:   1234567,
			Truncated: true,
		}, nil
	})
	out := biggestPathsSection()
	for _, want := range []string{"Biggest directories", "~/Library", "Biggest single files", "big.vmdk", "GB", "Partial", "1.2M"} {
		if !strings.Contains(out, want) {
			t.Errorf("biggestPathsSection missing %q, got: %s", want, out)
		}
	}
}

func TestBiggestPathsSectionSilentWhenEmptyOrError(t *testing.T) {
	withFakeDiskUsageCache(t, func() (diskScanResult, error) { return diskScanResult{}, nil })
	if out := biggestPathsSection(); out != "" {
		t.Errorf("empty scan should render nothing, got: %s", out)
	}
	withFakeDiskUsageCache(t, func() (diskScanResult, error) { return diskScanResult{}, errors.New("no home") })
	if out := biggestPathsSection(); out != "" {
		t.Errorf("scan error should render nothing, got: %s", out)
	}
}

func TestToPathRowsShortensToTilde(t *testing.T) {
	rows := toPathRows([]pathSize{{"/home/u/x/y", 2048}}, "/home/u")
	if rows[0].Path != "~"+string(os.PathSeparator)+filepath.Join("x", "y") {
		t.Errorf("path not tilde-shortened: %q", rows[0].Path)
	}
	if rows[0].Size != "2.00 KB" {
		t.Errorf("size = %q", rows[0].Size)
	}
}

func TestFormatCount(t *testing.T) {
	for in, want := range map[int]string{5: "5", 12000: "12k", 2_500_000: "2.5M"} {
		if got := formatCount(in); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDiskUsageCacheDefaultScanRunsAgainstTheRealHome(t *testing.T) {
	// Exercise newDiskUsageCache's real wiring once. Bounded by the scan's
	// own wall-clock budget, so this can't hang the suite.
	if _, err := newDiskUsageCache().Get(); err != nil {
		t.Skipf("home dir unavailable: %v", err)
	}
}
