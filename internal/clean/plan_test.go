package clean

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSelection(t *testing.T) {
	opts, err := ParseOptions("", 7*24*time.Hour)
	if err != nil || opts.Categories != "caches,temp" {
		t.Fatalf("defaults: %+v %v", opts, err)
	}
	for _, categories := range []string{"unknown", "caches,,temp", "all"} {
		if _, err := ParseOptions(categories, time.Hour); err == nil {
			t.Fatalf("accepted %q", categories)
		}
	}
	if _, err := ParseOptions("temp", -time.Hour); err == nil {
		t.Fatal("accepted negative retention")
	}
}

func TestPlanRetentionOverlapAndApply(t *testing.T) {
	dir := planTree(t)
	old := time.Now().Add(-10 * 24 * time.Hour)
	stale := filepath.Join(dir, "a", "f2")
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	targets := []target{{path: dir, category: "temp"}, {path: filepath.Join(dir, "a"), category: "temp"}}
	p := buildPlan(context.Background(), targets, old.Add(24*time.Hour), 100)
	if len(p.actions) != 1 || p.actions[0].Bytes != 200 || p.actions[0].Preserved != 2 {
		t.Fatalf("plan: %+v", p.actions)
	}
	if _, err := os.Stat(stale); err != nil {
		t.Fatal("preview mutated files")
	}
	result := executePlan(context.Background(), p, nil, os.Remove)
	if result.FreedBytes != 200 || result.Actions[0].Status != "completed" {
		t.Fatalf("result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "f1")); err != nil {
		t.Fatal("recent file removed")
	}
}

func TestPlanChangedFileAndRemovalFailure(t *testing.T) {
	dir := planTree(t)
	p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
	if err := os.WriteFile(filepath.Join(dir, "f1"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := executePlan(context.Background(), p, nil, func(string) error { return errors.New("denied") })
	if result.FreedBytes != 0 || result.Actions[0].Status != "failed" || len(result.Actions[0].Issues) != 3 {
		t.Fatalf("result: %+v", result)
	}
}

func TestPlanCancellationAndBudget(t *testing.T) {
	dir := planTree(t)
	targets := []target{{path: dir, category: "caches"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, p := range []cleanupPlan{
		buildPlan(ctx, targets, time.Now(), 100),
		buildPlan(context.Background(), targets, time.Now(), 1),
	} {
		if p.complete || p.actions[0].Complete {
			t.Fatalf("incomplete walk reported complete: %+v", p)
		}

		result := executePlan(context.Background(), p, nil, os.Remove)
		if result.FreedBytes != 0 {
			t.Fatal("applied incomplete plan")
		}
	}
}

func TestToolActionsAreVisibleAndReportFailures(t *testing.T) {
	p := buildPlan(context.Background(), []target{{category: "tools", command: "docker", args: []string{"system", "prune", "-f"}, reason: "Removes unused Docker resources; reclaim unknown."}}, time.Now(), 100)
	if len(p.actions) != 1 || p.actions[0].Target != "docker system prune -f" || p.actions[0].Kind != "command" {
		t.Fatalf("invisible command: %+v", p)
	}

	result := executePlan(context.Background(), p, func(context.Context, string, ...string) error { return errors.New("tool failed") }, os.Remove)
	if result.Actions[0].Status != "failed" || result.FreedBytes != 0 {
		t.Fatalf("result: %+v", result)
	}
}

func TestDefaultTargetsExcludeConsequentialActions(t *testing.T) {
	opts, _ := ParseOptions("", 7*24*time.Hour)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, target := range cleanupTargets(goos, t.TempDir(), opts) {
			if target.category != "caches" && target.category != "temp" {
				t.Fatalf("unsafe default: %+v", target)
			}
		}
	}
}

func planTree(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(mktree(t))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPlanPartialSuccessCreditsOnlyRemovedFiles(t *testing.T) {
	dir := planTree(t)
	p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
	r := executePlan(context.Background(), p, nil, func(path string) error {
		if filepath.Base(path) == "f1" {
			return errors.New("locked")
		}
		return os.Remove(path)
	})
	if r.FreedBytes != 500 || r.Actions[0].Status != "partial" || r.Error == "" || len(r.Records) != 1 || r.Records[0].Entries != 2 {
		t.Fatalf("partial result: %+v", r)
	}
}

func TestPlanUsesRootedRemovalAndKeepsDirectories(t *testing.T) {
	dir := planTree(t)
	p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
	r := executePlan(context.Background(), p, nil, nil)
	if r.FreedBytes != 600 || r.Error != "" {
		t.Fatalf("result: %+v", r)
	}
	if info, err := os.Stat(filepath.Join(dir, "a", "b")); err != nil || !info.IsDir() {
		t.Fatal("directory structure was removed")
	}
}

func TestPlanRefusesSymlinkTargetsAndPreservesLinks(t *testing.T) {
	dir := planTree(t)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "f1"), link); err != nil {
		t.Fatal(err)
	}
	p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
	if p.actions[0].Bytes != 600 || p.actions[0].Preserved != 1 {
		t.Fatalf("symlink counted: %+v", p.actions)
	}
	targetLink := filepath.Join(dir, "target-link")
	if err := os.Symlink(filepath.Join(dir, "a"), targetLink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{targetLink, filepath.Join(targetLink, "b"), filepath.Join(dir, "f1")} {
		p := buildPlan(context.Background(), []target{{path: path, category: "caches"}}, time.Now(), 100)
		if p.complete || len(p.actions[0].Issues) == 0 {
			t.Fatalf("accepted unsafe path %s: %+v", path, p)
		}
	}
}

func TestPlanRevalidatesIdentityEvenWithSameSizeAndTime(t *testing.T) {
	dir := planTree(t)
	p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
	path := filepath.Join(dir, "f1")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(dir, "original")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, info.Size()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	r := executePlan(context.Background(), p, nil, nil)
	if r.FreedBytes != 500 || r.Actions[0].Status != "partial" {
		t.Fatalf("replacement removed: %+v", r)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("replacement disappeared")
	}
}

func TestPlanRejectsChangedRootOrAncestor(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		dir := planTree(t)
		p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
		if err := os.Rename(dir, dir+"-old"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Rename(dir+"-old", dir) })
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		r := executePlan(context.Background(), p, nil, nil)
		if r.Actions[0].Status != "skipped" || r.FreedBytes != 0 {
			t.Fatalf("changed root: %+v", r)
		}
	})
	t.Run("ancestor", func(t *testing.T) {
		dir := planTree(t)
		p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
		if err := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "saved")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "saved"), filepath.Join(dir, "a")); err != nil {
			t.Fatal(err)
		}
		r := executePlan(context.Background(), p, nil, nil)
		if r.FreedBytes != 100 || r.Actions[0].Status != "partial" {
			t.Fatalf("followed replaced ancestor: %+v", r)
		}
		if _, err := os.Stat(filepath.Join(dir, "saved", "f2")); err != nil {
			t.Fatal("symlink target modified")
		}
	})
}

func TestPlanMissingTargetsAndCancelledExecution(t *testing.T) {
	dir := planTree(t)
	missing := filepath.Join(dir, "absent")
	p := buildPlan(context.Background(), []target{{path: missing, category: "caches"}}, time.Now(), 100)
	r := executePlan(context.Background(), p, nil, nil)
	if r.Actions[0].Status != "skipped" || r.Error != "" {
		t.Fatalf("missing target: %+v", r)
	}
	p = buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = executePlan(ctx, p, nil, nil)
	if r.FreedBytes != 0 || r.Error == "" || r.Actions[0].Status != "skipped" {
		t.Fatalf("cancelled: %+v", r)
	}
	if err := os.Remove(filepath.Join(dir, "f1")); err != nil {
		t.Fatal(err)
	}
	r = executePlan(context.Background(), p, nil, nil)
	if r.FreedBytes != 500 || r.Actions[0].Status != "partial" {
		t.Fatalf("missing file: %+v", r)
	}
}

func TestPlanConservativeOverlapAndCommandSuccess(t *testing.T) {
	dir := planTree(t)
	p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}, {path: filepath.Join(dir, "a"), category: "temp"}}, time.Now().Add(-time.Hour), 100)
	if len(p.actions) != 1 || p.actions[0].Bytes != 0 || p.actions[0].Category != "temp" {
		t.Fatalf("overlap bypassed retention: %+v", p)
	}
	p = buildPlan(context.Background(), []target{{command: "tool", category: "tools"}}, time.Now(), 100)
	for _, runner := range []func(context.Context, string, ...string) error{nil, func(context.Context, string, ...string) error { return nil }} {
		r := executePlan(context.Background(), p, runner, nil)
		want := "completed"
		if runner == nil {
			want = "failed"
		}
		if r.Actions[0].Status != want {
			t.Fatalf("command result: %+v", r)
		}
	}
}

func TestPreviewAndApplyIsolatedHome(t *testing.T) {
	isolateConfigDir(t)
	home := planTree(t)
	t.Setenv("LOCALAPPDATA", home)
	t.Setenv("TEMP", "")
	t.Setenv("TMP", "")
	cache := filepath.Join(home, ".cache")
	if err := os.Mkdir(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cache, "marker")
	if err := os.WriteFile(marker, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Categories: "caches"}
	preview := Preview(context.Background(), home, opts)
	if preview.FreedBytes != 5 || !preview.Complete || preview.Error != "" {
		t.Fatalf("preview: %+v", preview)
	}
	r := Apply(home, opts)
	if r.FreedBytes != 5 || r.Error != "" || len(History()) != 1 {
		t.Fatalf("apply: %+v", r)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("selected cache not removed")
	}
	for _, r := range []Result{Apply(home, Options{Categories: "invalid"}), Preview(context.Background(), home, Options{Categories: "invalid"})} {
		if r.Error == "" {
			t.Fatal("invalid options silently accepted")
		}
	}
}

func TestCleanupCommandReportsUnavailableFailureAndCancellation(t *testing.T) {
	if err := runCleanupCommand(context.Background(), "vitals-nonexistent-clean-tool"); err == nil {
		t.Fatal("unavailable command succeeded")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := runCleanupCommand(context.Background(), executable, "-test.run=^TestCleanupCommandChild$", "-vitals-invalid-flag"); err == nil {
		t.Fatal("failed command succeeded")
	}
	if err := runCleanupCommand(context.Background(), executable, "-test.run=^TestCleanupCommandChild$"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runCleanupCommand(ctx, executable, "-test.run=^TestCleanupCommandChild$"); err == nil {
		t.Fatal("cancelled command succeeded")
	}
}

func TestCleanupCommandChild(t *testing.T) {}

func TestProtectedWorkloadCachesSurvive(t *testing.T) {
	dir := planTree(t)
	for _, name := range []string{"uv", "huggingface", "models", "datasets", "Steam", "Adobe", "autosaves"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, "valuable"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "weights.gguf"), []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := buildPlan(context.Background(), []target{{path: dir, category: "caches"}}, time.Now(), 100)
	if p.actions[0].Bytes != 600 || len(p.actions[0].Protected) != 8 {
		t.Fatalf("protected paths not excluded: %+v", p.actions)
	}
	r := executePlan(context.Background(), p, nil, nil)
	if r.FreedBytes != 600 {
		t.Fatalf("incorrect bytes: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "huggingface", "valuable")); err != nil {
		t.Fatal("AI assets removed")
	}
}

func TestToolSelectionAndPlatformMatrix(t *testing.T) {
	for _, opts := range []Options{{Tools: "uv"}, {Categories: "tools", Tools: "unknown"}, {Categories: "tools", Tools: "uv,"}} {
		if err := ValidateOptions(opts); err == nil {
			t.Fatalf("accepted invalid selection: %+v", opts)
		}
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		opts := Options{Categories: "tools", Tools: "uv,pnpm,conda,go,dotnet"}
		if err := ValidateOptions(opts); err != nil {
			t.Fatal(err)
		}
		targets := cleanupTargets(goos, t.TempDir(), opts)
		if len(targets) != 5 {
			t.Fatalf("%s: missing cross-platform tools: %+v", goos, targets)
		}
		for _, target := range targets {
			command := strings.Join(target.args, " ")
			if strings.Contains(command, "force-pkgs-dirs") || strings.Contains(command, "global-packages") || strings.Contains(command, "modcache") {
				t.Fatalf("destructive tool policy: %+v", target)
			}
		}
		p := buildPlan(context.Background(), targets, time.Now(), 100)
		markToolAvailability(&p, func(name string) (string, error) {
			if name == "uv" {
				return "/tools/uv", nil
			}
			return "", errors.New("not installed")
		})
		calls := 0
		r := executePlan(context.Background(), p, func(_ context.Context, name string, _ ...string) error {
			if name != "uv" {
				t.Fatalf("ran missing tool %s", name)
			}
			calls++
			return nil
		}, nil)
		if calls != 1 || r.Error != "" {
			t.Fatalf("tool availability: %+v", r)
		}
	}
	t.Setenv("SystemRoot", t.TempDir())
	t.Setenv("TEMP", t.TempDir())
	t.Setenv("TMP", "")
	t.Setenv("LOCALAPPDATA", "")
	targets := cleanupTargets("windows", t.TempDir(), Options{Categories: "temp"})
	if len(targets) != 2 {
		t.Fatalf("Windows temp coverage: %+v", targets)
	}
}

func TestRunJSONSelectionAndExplicitApply(t *testing.T) {
	isolateConfigDir(t)
	home := planTree(t)
	t.Setenv("LOCALAPPDATA", home)
	d := deps{homeDir: func() (string, error) { return home, nil }, confirmReader: strings.NewReader("yes\n")}
	out := captureStdout(t, func() {
		if err := run(d, Options{DryRun: true, JSON: true, Categories: "caches"}); err != nil {
			t.Fatal(err)
		}
	})
	var result Result
	if err := json.Unmarshal([]byte(out), &result); err != nil || !result.DryRun || !result.Complete || result.Warning != RiskWarning {
		t.Fatalf("not structured preview: %s (%v)", out, err)
	}
	for _, opts := range []Options{{JSON: true}, {Categories: "invalid"}, {MinAge: -time.Hour}} {
		if err := run(d, opts); err == nil {
			t.Fatalf("invalid run accepted: %+v", opts)
		}
	}
	captureStdout(t, func() {
		if err := run(d, Options{Categories: "caches"}); err != nil {
			t.Fatal(err)
		}
	})
	captureStdout(t, func() {
		if err := run(d, Options{Categories: "caches", JSON: true, Assume: true}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRenderActionsIncludesConsequencesAndIssues(t *testing.T) {
	r := Result{Actions: []Action{{Category: "tools", Target: "tool command", Kind: "command", Preserved: 1, Issues: []string{"failed"}, Protected: []string{"models"}}}}
	out := captureStdout(t, func() { renderActions(r) })
	for _, want := range []string{"reclaim unknown", "failed", "Protected", "Preserved", "models"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
}

func TestScanFailureAndDepthAreIncomplete(t *testing.T) {
	dir := planTree(t)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, input := range []struct {
		name  string
		depth int
	}{{".", 65}, {"absent", 0}, {"f1", 0}} {
		var a Action
		var files []candidate
		visited := 0
		if err := scanTarget(context.Background(), root, input.name, input.depth, &visited, 100, time.Now(), "caches", &a, &files); err == nil {
			t.Fatalf("invalid scan succeeded: %+v", input)
		}
	}
}

func TestApplyReportsAuditFailure(t *testing.T) {
	isolateConfigDir(t)
	home := planTree(t)
	t.Setenv("LOCALAPPDATA", home)
	path, ok := cleanHistoryPath()
	if !ok {
		t.Fatal("missing config")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if r := Apply(home, Options{Categories: "caches"}); !strings.Contains(r.Error, "audit history") {
		t.Fatalf("audit failure hidden: %+v", r)
	}
	t.Setenv("HOME", "")
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if runtime.GOOS != "windows" {
		if r := Apply(home, Options{Categories: "caches"}); !strings.Contains(r.Error, "audit history") {
			t.Fatalf("missing config hidden: %+v", r)
		}
	}
}

func TestRunReportsIncompletePlanBeforeConfirmation(t *testing.T) {
	home := planTree(t)
	t.Setenv("LOCALAPPDATA", home)
	if err := os.Symlink(filepath.Join(home, "a"), filepath.Join(home, ".cache")); err != nil {
		t.Fatal(err)
	}
	d := deps{homeDir: func() (string, error) { return home, nil }, confirmReader: strings.NewReader("yes\n")}
	for _, opts := range []Options{{Categories: "caches"}, {Categories: "caches", DryRun: true}, {Categories: "caches", DryRun: true, JSON: true}} {
		captureStdout(t, func() {
			if err := run(d, opts); err == nil || !strings.Contains(err.Error(), "incomplete") {
				t.Fatalf("incomplete run succeeded: %v", err)
			}
		})
	}
}
