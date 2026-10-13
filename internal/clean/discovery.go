package clean

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type cacheProbe func(context.Context, string, ...string) (string, error)

func cacheQuery(name string) ([]string, string) {
	switch name {
	case "npm":
		return []string{"config", "get", "cache"}, "path"
	case "pnpm":
		return []string{"store", "path"}, "path"
	case "pip", "pip3":
		return []string{"cache", "dir"}, "path"
	case "uv":
		return []string{"cache", "dir"}, "path"
	case "go":
		return []string{"env", "GOCACHE"}, "path"
	case "brew":
		return []string{"--cache"}, "path"
	case "conda":
		return []string{"info", "--json"}, "conda"
	case "dotnet":
		return []string{"nuget", "locals", "http-cache", "--list", "--force-english-output"}, "nuget"
	default:
		return nil, ""
	}
}

func parseCacheLocations(output, format string) ([]string, error) {
	var paths []string
	switch format {
	case "conda":
		var info struct {
			PackageDirs []string `json:"pkgs_dirs"`
		}
		if err := json.Unmarshal([]byte(output), &info); err != nil {
			return nil, err
		}
		paths = info.PackageDirs
	case "nuget":
		for _, line := range strings.Split(output, "\n") {
			if path, found := strings.CutPrefix(strings.TrimSpace(line), "http-cache:"); found {
				paths = append(paths, strings.TrimSpace(path))
			}
		}
	default:
		paths = []string{strings.TrimSpace(output)}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("cache discovery returned no paths")
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "\r\n\x00") {
			return nil, fmt.Errorf("cache discovery returned an invalid or relative path; no guessed fallback will be used")
		}
	}
	return paths, nil
}

// Discovery queries never mutate cache contents; actual cleanup stays manager-owned.
func discoverToolCaches(ctx context.Context, p *cleanupPlan, probe cacheProbe) {
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i, t := range p.targets {
		args, format := cacheQuery(t.command)
		if args == nil || p.actions[i].Status == "skipped" {
			continue
		}
		wg.Add(1)
		go func(i int, t target, args []string, format string) {
			defer wg.Done()
			a := &p.actions[i]
			a.Discovery = t.command + " " + strings.Join(args, " ")
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				a.Status, a.Complete, a.Issues = "skipped", false, []string{ctx.Err().Error()}
				return
			}
			queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			output, err := probe(queryCtx, t.command, args...)
			if err == nil {
				a.Locations, err = parseCacheLocations(output, format)
			}
			if err != nil {
				a.Status, a.Complete = "skipped", false
				a.Issues = []string{fmt.Sprintf("Cache discovery failed: %v. Check the discovery command manually; cleanup will not guess a path.", err)}
			}
		}(i, t, args, format)
	}
	wg.Wait()
	for _, a := range p.actions {
		p.complete = p.complete && a.Complete
	}
}

type discoveryOutput struct{ buffer bytes.Buffer }

func (b *discoveryOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 64*1024 {
		return 0, fmt.Errorf("cache discovery output exceeds 64 KiB")
	}
	return b.buffer.Write(p)
}

func probeToolCache(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	var output discoveryOutput
	cmd.Stdout = &output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s cache query failed: %w", name, err)
	}
	return output.buffer.String(), nil
}
