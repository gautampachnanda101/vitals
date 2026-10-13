package diskusage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestDisplayPath(t *testing.T) {
	for path, want := range map[string]string{
		"hello/世界😀":                      "hello/世界😀",
		"\x00\n\r\t\x1b\x7f\u0080\u009f": `\x00\x0a\x0d\x09\x1b\x7f\x80\x9f`,
		"\u202e\u2066\u200b\ufeff":       `\u202e\u2066\u200b\ufeff`,
		"\U000e0001":                     `\U000e0001`,
		"\xff":                           `\xff`,
		"":                               "",
	} {
		if got := DisplayPath(path); got != want {
			t.Fatalf("DisplayPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestLiveRootFailuresAndSymlinks(t *testing.T) {
	root := fixture(t, map[string]string{"file": "abc", "d/file": "def"})
	for _, path := range []string{filepath.Join(root, "missing"), filepath.Join(root, "file")} {
		r, err := Scan(context.Background(), path, DefaultOptions())
		if err == nil || r.Complete {
			t.Fatalf("accepted unusable root %s: %+v, %v", path, r, err)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires optional Windows privileges")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink("d", link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, filepath.Join(link, "descendant")} {
		r, err := Scan(context.Background(), path, DefaultOptions())
		if err == nil || r.Complete || !strings.Contains(err.Error(), "real directory") {
			t.Fatalf("accepted symlink ancestor: %+v, %v", r, err)
		}
	}
	if err := os.Symlink("../outside", filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}
	r, err := Scan(context.Background(), root, DefaultOptions())
	if err != nil || !r.Complete || r.SkippedSymlinks != 2 || r.ObservedBytes != 6 {
		t.Fatalf("descendant links followed: %+v, %v", r, err)
	}
	dir, err := openOSRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, err := dir.OpenRoot("absent"); err == nil {
		t.Fatal("missing rooted directory opened")
	}
}

func TestBorrowedRootSurvivesRenameAndRemainsOpen(t *testing.T) {
	root := fixture(t, map[string]string{"file": "abc"})
	handle, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	moved := root + "-moved"
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "different"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := ScanRoot(context.Background(), handle, root, DefaultOptions())
	if err != nil || !r.Complete || r.ObservedBytes != 3 || r.Files[0].Path != filepath.Join(root, "file") {
		t.Fatalf("root path was reopened: %+v, %v", r, err)
	}
	if _, err := handle.Lstat("file"); err != nil {
		t.Fatalf("caller root was closed: %v", err)
	}
	r, err = ScanRoot(context.Background(), nil, root, DefaultOptions())
	if err == nil || r.Complete {
		t.Fatalf("nil root accepted: %+v %v", r, err)
	}
}

func TestMountInventorySeam(t *testing.T) {
	wantErr := errors.New("cannot enumerate mounts")
	paths, err := mountPaths(context.Background(), func(context.Context, bool) ([]disk.PartitionStat, error) {
		return nil, wantErr
	})
	if err != wantErr || paths != nil {
		t.Fatalf("%v %v", paths, err)
	}
	paths, err = mountPaths(context.Background(), func(ctx context.Context, all bool) ([]disk.PartitionStat, error) {
		if ctx == nil || !all {
			t.Fatal("wrong live inventory args")
		}
		return []disk.PartitionStat{{Mountpoint: filepath.Join("one", "two", "..")}}, nil
	})
	if err != nil || len(paths) != 1 || paths[0] != "one" {
		t.Fatalf("%v %v", paths, err)
	}
	if _, ok := volumeID(&testInfo{}); ok {
		t.Fatal("fake metadata acquired a device identity")
	}
}

func TestRootOpeningCancellationChecks(t *testing.T) {
	for _, stage := range []string{"before", "anchor", "stat", "open", "recheck", "actual"} {
		t.Run(stage, func(t *testing.T) {
			f := newFS(t)
			f.add("d", 0, fs.ModeDir)
			path := filepath.Join(filepath.VolumeName(f.root)+string(filepath.Separator), "d")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if stage == "before" {
				cancel()
			}
			checks := 0
			f.stat = func(parent, name string) (fs.FileInfo, error) {
				checks++
				if stage == "stat" && checks == 1 || stage == "recheck" && checks == 2 || stage == "actual" && parent == "d" {
					cancel()
				}
				return nil, nil
			}
			f.open = func(string) error {
				if stage == "open" {
					cancel()
				}
				return nil
			}
			dir, err := openFromAnchor(ctx, path, func(string) (directory, error) {
				f.opened.Add(1)
				if stage == "anchor" {
					cancel()
				}
				return &testDirectory{f: f, path: "."}, nil
			}, func(a, b fs.FileInfo) bool { return a == b })
			if err != context.Canceled || dir != nil {
				t.Fatalf("root cancellation was ignored at %s: %v %v", stage, dir, err)
			}
			f.assertClosed(t)
		})
	}
	f := newFS(t)
	ctx, cancel := context.WithCancel(context.Background())
	deps := f.deps()
	deps.openRoot = func(context.Context, string) (directory, error) {
		cancel()
		return nil, context.Canceled
	}
	r, err := scanWith(ctx, f.root, DefaultOptions(), deps)
	if err != nil || r.Complete || r.ErrorCount != 0 {
		t.Fatalf("context cancellation misreported as root access failure: %+v %v", r, err)
	}
}

type replacingDirectory struct {
	directory
	replace func()
}

func (d replacingDirectory) OpenRoot(name string) (directory, error) {
	if name == "d" {
		d.replace()
	}
	return d.directory.OpenRoot(name)
}

func TestLiveSymlinkReplacementToOriginalInode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires optional Windows privileges")
	}
	root := fixture(t, map[string]string{"d/file": "secret"})
	deps := liveDependencies()
	deps.mounts = func(context.Context) ([]string, error) { return nil, nil }
	deps.openRoot = func(ctx context.Context, path string) (directory, error) {
		dir, err := openCheckedRoot(ctx, path)
		if err != nil {
			return nil, err
		}
		return replacingDirectory{dir, func() {
			if err := os.Rename(filepath.Join(root, "d"), filepath.Join(root, "moved")); err != nil {
				t.Error(err)
			}
			if err := os.Symlink("moved", filepath.Join(root, "d")); err != nil {
				t.Error(err)
			}
		}}, nil
	}
	r, err := scanWith(context.Background(), root, DefaultOptions(), deps)
	if err != nil || r.Complete || r.ObservedBytes != 0 || r.ErrorCount != 1 || len(r.Directories) != 1 || r.Directories[0].Complete {
		t.Fatalf("followed replacement symlink to the original inode: %+v %v", r, err)
	}
}

func TestOpenAnchorFailures(t *testing.T) {
	f := newFS(t)
	f.add("d", 0, fs.ModeDir)
	path := filepath.Join(string(filepath.Separator), "d")
	// A Windows anchor needs a drive; synthetic handles don't touch that drive.
	if runtime.GOOS == "windows" {
		path = `C:\d`
	}
	for _, stage := range []string{"anchor", "link", "file", "lstat", "open", "recheck-error", "recheck-link", "actual", "replace", "success"} {
		t.Run(stage, func(t *testing.T) {
			g := newFS(t)
			g.add("d", 0, fs.ModeDir)
			var opened bool
			if stage == "link" {
				g.nodes["d"].info.mode = fs.ModeSymlink
			} else if stage == "file" {
				g.nodes["d"].info.mode = 0
			}
			g.open = func(string) error {
				opened = true
				if stage == "open" {
					return fs.ErrPermission
				}
				return nil
			}
			g.stat = func(parent, name string) (fs.FileInfo, error) {
				if stage == "lstat" || stage == "actual" && parent == "d" || stage == "recheck-error" && opened && name == "d" {
					return nil, fs.ErrPermission
				}
				if stage == "replace" && parent == "d" {
					return &testInfo{mode: fs.ModeDir}, nil
				}
				if stage == "recheck-link" && opened && name == "d" {
					return &testInfo{mode: fs.ModeSymlink}, nil
				}
				return nil, nil
			}
			opener := func(string) (directory, error) {
				if stage == "anchor" {
					return nil, fs.ErrPermission
				}
				g.opened.Add(1)
				return &testDirectory{f: g, path: "."}, nil
			}
			dir, err := openFromAnchor(context.Background(), path, opener, func(a, b fs.FileInfo) bool { return a == b })
			if stage == "success" {
				if err != nil {
					t.Fatal(err)
				}
				_ = dir.Close()
			} else if err == nil {
				_ = dir.Close()
				t.Fatal("root replacement/error accepted")
			}
			g.assertClosed(t)
		})
	}
	g := newFS(t)
	dir, err := openFromAnchor(context.Background(), filepath.VolumeName(path)+string(filepath.Separator), func(string) (directory, error) {
		g.opened.Add(1)
		return &testDirectory{f: g, path: "."}, nil
	}, os.SameFile)
	if err != nil {
		t.Fatal(err)
	}
	_ = dir.Close()
	g.assertClosed(t)
	if _, err := openOSRoot(filepath.Join(f.root, "missing")); err == nil {
		t.Fatal("missing anchor opened")
	}
}
