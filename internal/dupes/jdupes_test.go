package dupes

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func fakeJdupes(out string, err error) jdupesRunner {
	return func([]string) ([]byte, error) { return []byte(out), err }
}

func TestScanWithJdupesParsesMatchSets(t *testing.T) {
	root := "/data"
	json := `{"exitStatus":0,"matchSets":[
	  {"fileSize":2048,"fileList":[{"filePath":"/data/a.bin"},{"filePath":"/data/b.bin"},{"filePath":"/data/c.bin"}]},
	  {"fileSize":500,"fileList":[{"filePath":"/data/x"},{"filePath":"/data/y"}]}
	]}`
	res, ok := scanWithJdupes(fakeJdupes(json, nil), root, 0)
	if !ok {
		t.Fatal("expected ok=true for valid JSON")
	}
	if res.Backend != "jdupes" {
		t.Errorf("Backend = %q, want jdupes", res.Backend)
	}
	if res.ScannedFiles != 0 || res.ScannedBytes != 0 {
		t.Errorf("jdupes reports no scan totals; want 0/0, got %d/%d", res.ScannedFiles, res.ScannedBytes)
	}
	if len(res.Groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(res.Groups))
	}
	// sorted most-reclaimable-first: 2048*2 = 4096 wasted beats 500*1
	if res.Groups[0].SizeBytes != 2048 || len(res.Groups[0].Paths) != 3 {
		t.Errorf("group 0 = %+v", res.Groups[0])
	}
	if res.WastedBytes != 2048*2+500*1 {
		t.Errorf("WastedBytes = %d, want %d", res.WastedBytes, 2048*2+500)
	}
	// paths within a group are sorted
	if res.Groups[0].Paths[0] != "/data/a.bin" {
		t.Errorf("group paths not sorted: %v", res.Groups[0].Paths)
	}
}

func TestScanWithJdupesEmptyMatchSets(t *testing.T) {
	res, ok := scanWithJdupes(fakeJdupes(`{"exitStatus":0,"matchSets":[]}`, nil), "/d", 0)
	if !ok {
		t.Fatal("empty result is still a successful run")
	}
	if len(res.Groups) != 0 || res.Backend != "jdupes" {
		t.Errorf("res = %+v", res)
	}
}

func TestScanWithJdupesFallsBackOnError(t *testing.T) {
	// a real jdupes error exits non-zero with no JSON — the runner
	// surfaces that as an error.
	if _, ok := scanWithJdupes(fakeJdupes("could not access '/nope'", errors.New("exit status 1")), "/nope", 0); ok {
		t.Error("a runner error must signal fallback (ok=false)")
	}
}

func TestScanWithJdupesFallsBackOnUnparseableOutput(t *testing.T) {
	if _, ok := scanWithJdupes(fakeJdupes("not json at all", nil), "/d", 0); ok {
		t.Error("unparseable output must signal fallback (ok=false)")
	}
}

func TestScanWithJdupesPassesMinSizeFilter(t *testing.T) {
	var gotArgs []string
	runner := func(args []string) ([]byte, error) {
		gotArgs = args
		return []byte(`{"exitStatus":0,"matchSets":[]}`), nil
	}
	scanWithJdupes(runner, "/d", 1<<20)
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "-X size+=:1048576") {
		t.Errorf("min-size not passed to jdupes: %v", gotArgs)
	}
	if !strings.Contains(joined, "-r") || !strings.Contains(joined, "-j") {
		t.Errorf("expected recursive + json flags: %v", gotArgs)
	}
}

func TestScanWithJdupesReappliesSkipDirNames(t *testing.T) {
	root := "/home/u"
	json := `{"exitStatus":0,"matchSets":[
	  {"fileSize":9,"fileList":[
	    {"filePath":"/home/u/keep/one"},
	    {"filePath":"/home/u/node_modules/pkg/two"},
	    {"filePath":"/home/u/keep/three"}]},
	  {"fileSize":9,"fileList":[
	    {"filePath":"/home/u/.git/a"},
	    {"filePath":"/home/u/.cache/b"}]}
	]}`
	res, ok := scanWithJdupes(fakeJdupes(json, nil), root, 0)
	if !ok {
		t.Fatal("ok")
	}
	// group 1: node_modules path dropped, 2 real paths remain -> kept
	// group 2: both paths under skipped dirs -> group drops below 2 -> gone
	if len(res.Groups) != 1 {
		t.Fatalf("want 1 group after skip-dir filtering, got %d: %+v", len(res.Groups), res.Groups)
	}
	for _, p := range res.Groups[0].Paths {
		if strings.Contains(p, "node_modules") {
			t.Errorf("node_modules path survived filtering: %v", res.Groups[0].Paths)
		}
	}
}

func TestRunUsesJdupesWhenFastAndInstalled(t *testing.T) {
	dir := t.TempDir()
	json := `{"exitStatus":0,"matchSets":[{"fileSize":4096,"fileList":[{"filePath":"` +
		filepath.Join(dir, "a") + `"},{"filePath":"` + filepath.Join(dir, "b") + `"}]}]}`
	d := deps{
		homeDir:     func() (string, error) { return dir, nil },
		jdupesAvail: func() bool { return true },
		jdupesRun:   fakeJdupes(json, nil),
	}
	out := captureStdout(t, func() {
		if err := run(d, Options{Root: dir, Fast: true}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "via jdupes") {
		t.Errorf("expected the jdupes backend note in output, got: %s", out)
	}
	if strings.Contains(out, "scanned 0 files") {
		t.Errorf("must not print a bogus scanned-0 line for the jdupes backend: %s", out)
	}
}

func TestRunFallsBackToBuiltinWhenJdupesAbsent(t *testing.T) {
	dir := t.TempDir()
	d := deps{
		homeDir:     func() (string, error) { return dir, nil },
		jdupesAvail: func() bool { return false }, // --fast asked, but not installed
		jdupesRun:   fakeJdupes("", errors.New("should not be called")),
	}
	out := captureStdout(t, func() {
		if err := run(d, Options{Root: dir, Fast: true}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "via jdupes") {
		t.Errorf("jdupes not installed -> built-in backend, got: %s", out)
	}
	if !strings.Contains(out, "scanned") {
		t.Errorf("built-in backend still prints a scanned-files line: %s", out)
	}
}

func TestJdupesRealWiringDoesNotPanic(t *testing.T) {
	// Exercises the real LookPath / exec wiring once — result depends on
	// whether the CI runner has jdupes, so only assert it's consistent
	// and doesn't blow up.
	avail := jdupesAvailable()
	_, err := defaultJdupesRunner([]string{"--version"})
	if avail && err != nil {
		t.Logf("jdupes reported installed but --version errored: %v", err)
	}
	if !avail && err == nil {
		t.Log("jdupes not on PATH but a bare exec somehow succeeded")
	}
}

func TestPathHasSkippedDir(t *testing.T) {
	root := string(filepath.Separator) + filepath.Join("home", "u")
	cases := map[string]bool{
		filepath.Join(root, "docs", "a.txt"):               false,
		filepath.Join(root, "node_modules", "x", "a.js"):   true,
		filepath.Join(root, ".git", "config"):              true,
		filepath.Join(root, "src", "__pycache__", "m.pyc"): true,
		filepath.Join(root, "srcnode_modules", "a"):        false, // substring, not a component
	}
	for p, want := range cases {
		if got := pathHasSkippedDir(p, root); got != want {
			t.Errorf("pathHasSkippedDir(%q) = %v, want %v", p, got, want)
		}
	}
}
