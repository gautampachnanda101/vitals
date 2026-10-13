package clean

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeCacheDiscoveryUsesConfiguredLocations(t *testing.T) {
	path := planTree(t)
	var targets []target
	for _, name := range []string{"npm", "pnpm", "pip", "pip3", "uv", "go", "brew", "conda", "dotnet"} {
		targets = append(targets, target{command: name, category: "tools"})
	}
	p := buildPlan(context.Background(), targets, time.Now(), 100)
	discoverToolCaches(context.Background(), &p, func(_ context.Context, name string, args ...string) (string, error) {
		want, _ := cacheQuery(name)
		if strings.Join(args, " ") != strings.Join(want, " ") {
			t.Errorf("wrong discovery argv for %s: %v", name, args)
		}
		switch name {
		case "conda":
			output, err := json.Marshal(struct {
				Dirs []string `json:"pkgs_dirs"`
			}{[]string{path}})
			return string(output), err
		case "dotnet":
			return "http-cache: " + path + "\r\n", nil
		default:
			return path + "\n", nil
		}
	})
	for _, a := range p.actions {
		if len(a.Locations) != 1 || a.Locations[0] != path || a.Discovery == "" || !a.Complete {
			t.Fatalf("configured path not discovered: %+v", a)
		}
	}
}

func TestDiscoveryFailureNeverFallsBack(t *testing.T) {
	for _, output := range []string{"", "relative/cache", "/cache\n/another", "undefined"} {
		p := buildPlan(context.Background(), []target{{command: "npm", category: "tools"}}, time.Now(), 100)
		discoverToolCaches(context.Background(), &p, func(context.Context, string, ...string) (string, error) { return output, nil })
		calls := 0
		result := executePlan(context.Background(), p, func(context.Context, string, ...string) error { calls++; return nil }, nil)
		if p.complete || result.Error == "" || calls != 0 || len(p.actions[0].Locations) != 0 {
			t.Fatalf("discovery guessed or executed: %+v", result)
		}
	}
	for _, format := range []string{"conda", "nuget"} {
		for _, output := range []string{"{bad", "{}", ""} {
			if _, err := parseCacheLocations(output, format); err == nil {
				t.Fatalf("accepted invalid %s output %q", format, output)
			}
		}
	}
}

func TestDiscoveryCancellationAndUnavailableTools(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := buildPlan(context.Background(), []target{{command: "uv", category: "tools"}, {command: "docker", category: "tools"}}, time.Now(), 100)
	discoverToolCaches(ctx, &p, func(ctx context.Context, _ string, _ ...string) (string, error) { return "", ctx.Err() })
	if p.complete {
		t.Fatal("cancelled discovery reported complete")
	}
	p = buildPlan(context.Background(), []target{{command: "pip", category: "tools"}, {command: "uv", category: "tools"}}, time.Now(), 100)
	markToolAvailability(&p, func(name string) (string, error) {
		if name == "pip3" {
			return "pip3", nil
		}
		return "", errors.New("not installed")
	})
	calls := 0
	discoverToolCaches(context.Background(), &p, func(_ context.Context, name string, _ ...string) (string, error) {
		calls++
		if name != "pip3" {
			t.Errorf("queried unavailable tool %s", name)
		}
		return "", errors.New("cache query failed")
	})
	if calls != 1 || p.targets[0].command != "pip3" {
		t.Fatalf("pip alias/availability not honored: %+v", p)
	}
}

func TestDiscoveryOutputIsBoundedAndCommandsAreCancellable(t *testing.T) {
	var output discoveryOutput
	if _, err := output.Write(make([]byte, 64*1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte("x")); err == nil {
		t.Fatal("unbounded discovery output")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if output, err := probeToolCache(context.Background(), executable, "-test.run=^TestCleanupCommandChild$"); err != nil || output == "" {
		t.Fatalf("read-only probe failed: %q %v", output, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := probeToolCache(ctx, executable, "-test.run=^TestCleanupCommandChild$"); err == nil {
		t.Fatal("cancelled probe succeeded")
	}
}

func TestEnvironmentOverridesAndWindowsBrowserProfiles(t *testing.T) {
	dir := planTree(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("GRADLE_USER_HOME", filepath.Join(dir, "gradle"))
	t.Setenv("CARGO_HOME", filepath.Join(dir, "cargo"))
	got := devCacheDirs(filepath.Join(dir, "home"))
	want := []string{filepath.Join(dir, "xdg"), filepath.Join(dir, "gradle", "caches"), filepath.Join(dir, "cargo", "registry", "cache")}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ignored configured roots: %v", got)
	}
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("TEMP", "")
	t.Setenv("TMP", "")
	profile := filepath.Join(dir, "Google", "Chrome", "User Data", "Profile 3", "Cache")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, target := range osCacheDirs("windows", dir) {
		found = found || target == profile
	}
	if !found {
		t.Fatal("non-default browser profile was not discovered")
	}
}

func TestInvalidEnvironmentCannotSweepHomeOrFilesystemRoot(t *testing.T) {
	home := planTree(t)
	for _, path := range []string{home, filepath.Dir(home), filepath.VolumeName(home) + string(filepath.Separator)} {
		t.Setenv("XDG_CACHE_HOME", path)
		opts, _ := ParseOptions("caches", defaultMinAge)
		p := buildPlan(context.Background(), cleanupTargets("linux", home, opts), time.Now(), 100)
		found := false
		for _, a := range p.actions {
			if a.Target == path {
				found = true
				if a.Complete || a.Status != "skipped" || a.Bytes != 0 || len(a.Issues) == 0 {
					t.Fatalf("dangerous configured root accepted: %+v", a)
				}
			}
		}
		if !found {
			t.Fatalf("invalid root was not explicitly reported: %s", path)
		}
	}
}
