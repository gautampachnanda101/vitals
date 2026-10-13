package clean

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const defaultMinAge = 7 * 24 * time.Hour
const planFileLimit = 100000
const scanTimeout = 30 * time.Second

// RiskWarning applies equally to file removal and manager-owned cleanup.
const RiskWarning = "Cleanup is at your own risk. Back up important data and close affected applications. Deletion is permanent with no undo; previews and age checks do not guarantee a file is unused."

// Action reports one fixed cleanup target, including commands whose reclaim is unknown.
type Action struct {
	Target    string   `json:"target"`
	Category  string   `json:"category"`
	Kind      string   `json:"kind"`
	Reason    string   `json:"reason"`
	Bytes     int64    `json:"eligible_bytes"`
	Removed   int64    `json:"removed_bytes"`
	Preserved int      `json:"preserved_files"`
	Complete  bool     `json:"complete"`
	Status    string   `json:"status"`
	Issues    []string `json:"issues,omitempty"`
	Protected []string `json:"protected_paths,omitempty"`
	Locations []string `json:"locations,omitempty"`
	Discovery string   `json:"discovery,omitempty"`
}

type target struct {
	path, category, reason, command string
	args                            []string
	blocked                         string
}

type candidate struct {
	path string
	info os.FileInfo
}

type cleanupPlan struct {
	actions  []Action
	targets  []target
	files    [][]candidate
	roots    []os.FileInfo
	complete bool
}

// ParseOptions validates selection; --yes never changes which categories are selected.
func ParseOptions(categories string, minAge time.Duration) (Options, error) {
	if categories == "" {
		categories = "caches,temp"
	}
	for _, c := range strings.Split(categories, ",") {
		switch c {
		case "caches", "temp", "logs", "trash", "tools":
		default:
			return Options{}, fmt.Errorf("unknown cleanup category %q; choose caches,temp,logs,trash,tools (see vitals guide)", c)
		}
	}
	if minAge < 0 {
		return Options{}, fmt.Errorf("min-age must not be negative")
	}
	return Options{Categories: categories, MinAge: minAge, MinAgeSet: true}, nil
}

func normalizedOptions(opts Options) (Options, error) {
	age := opts.MinAge
	if age == 0 && !opts.MinAgeSet {
		age = defaultMinAge
	}
	validated, err := ParseOptions(opts.Categories, age)
	if err != nil {
		return opts, err
	}
	opts.Categories, opts.MinAge = validated.Categories, validated.MinAge
	if opts.Tools != "" {
		if !strings.Contains(","+opts.Categories+",", ",tools,") {
			return opts, fmt.Errorf("--tools requires tools in --categories; it never expands selection")
		}
		for _, name := range strings.Split(opts.Tools, ",") {
			if !strings.Contains(",docker,pip,npm,pnpm,uv,conda,go,dotnet,brew,xcrun,apt-get,dnf,yum,pacman,journalctl,flatpak,dism.exe,", ","+name+",") || name == "" {
				return opts, fmt.Errorf("unsupported cleanup tool %q; see vitals guide", name)
			}
		}
	}
	return opts, nil
}

// ValidateOptions checks category, retention and tool selection without scanning.
func ValidateOptions(opts Options) error {
	_, err := normalizedOptions(opts)
	return err
}

func cleanupTargets(goos, home string, opts Options) []target {
	selected := "," + opts.Categories + ","
	home = filepath.Clean(home)
	var targets []target
	add := func(path, category, reason string) {
		if strings.Contains(selected, ","+category+",") && path != "" {
			path = filepath.Clean(path)
			t := target{path: path, category: category, reason: reason}
			rel, relErr := filepath.Rel(path, home)
			containsHome := relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
			if containsHome || filepath.Dir(path) == path {
				t.blocked = "Refusing a cleanup root equal to or above the home directory, or the filesystem root; fix the cache/temp environment setting."
			}
			targets = append(targets, t)
		}
	}
	for _, path := range devCacheDirs(home) {
		add(path, "caches", "Cache files may need downloading or rebuilding; close affected applications first.")
	}
	for _, path := range osCacheDirs(goos, home) {
		category, reason := "caches", "Application caches may need rebuilding; close affected applications first."
		switch {
		case filepath.Base(path) == ".Trash":
			category, reason = "trash", "Permanently removes recoverable Trash files."
		case filepath.Base(path) == "Logs":
			category, reason = "logs", "Removes logs that may be needed for troubleshooting."
		case goos == "linux" || path == os.Getenv("TEMP") || path == os.Getenv("TMP") || filepath.Base(path) == "Temp":
			category, reason = "temp", "Only regular files older than min-age; age does not prove a file is unused."
		}
		add(path, category, reason)
	}
	if goos == "darwin" && runtime.GOOS == "darwin" {
		if path, err := filepath.EvalSymlinks(os.TempDir()); err == nil && strings.HasPrefix(path, "/private/var/folders/") {
			add(path, "temp", "Old files in the macOS per-user temporary directory; age does not prove they are unused.")
		}
	}
	if goos == "windows" {
		if sr := os.Getenv("SystemRoot"); sr != "" {
			add(filepath.Join(sr, "Temp"), "temp", "Old temporary files only; locked files are reported, not forced.")
		}
	}
	command := func(name, reason string, args ...string) {
		if strings.Contains(selected, ",tools,") && (opts.Tools == "" || strings.Contains(","+opts.Tools+",", ","+name+",")) {
			targets = append(targets, target{category: "tools", command: name, args: args, reason: reason + " Reclaim is unknown; command timeout is two minutes."})
		}
	}
	command("docker", "Prunes stopped containers, unused networks, dangling images and build cache; not volumes. Uses the configured Docker context, which may be remote.", "system", "prune", "-f")
	command("pip", "Purges downloaded package cache.", "cache", "purge")
	command("npm", "Purges npm cache; packages must be downloaded again.", "cache", "clean", "--force")
	command("pnpm", "Prunes unreferenced store packages.", "store", "prune")
	command("uv", "Prunes unused cache entries and centralized project environments, which may need recreating.", "cache", "prune")
	command("conda", "Removes cached archives and index metadata; preserves installed environments and extracted package caches.", "clean", "--tarballs", "--index-cache", "--yes")
	command("go", "Clears build and test-result caches; preserves downloaded modules and installed tools.", "clean", "-cache", "-testcache")
	command("dotnet", "Clears NuGet HTTP request cache; preserves global packages.", "nuget", "locals", "http-cache", "--clear")
	switch goos {
	case "darwin":
		command("brew", "Removes old Homebrew downloads and versions.", "cleanup", "-s")
		command("xcrun", "Deletes unavailable simulator devices.", "simctl", "delete", "unavailable")
	case "linux":
		command("apt-get", "Removes downloaded apt packages; may require elevation.", "clean")
		command("dnf", "Removes dnf cache.", "clean", "all")
		command("yum", "Removes yum cache.", "clean", "all")
		command("pacman", "Removes all cached packages, including downgrade copies.", "-Scc", "--noconfirm")
		command("journalctl", "Deletes journal history older than seven days.", "--vacuum-time=7d")
		command("flatpak", "Uninstalls unused runtimes.", "uninstall", "--unused", "-y")
	case "windows":
		command("dism.exe", "Removes superseded Windows components; requires elevation.", "/online", "/cleanup-image", "/startcomponentcleanup")
		if strings.Contains(selected, ",trash,") {
			targets = append(targets, target{
				category: "trash", command: "powershell",
				args:   []string{"-NoProfile", "-NonInteractive", "-Command", "Clear-RecycleBin -Force -ErrorAction Stop"},
				reason: "Permanently empties the Windows Recycle Bin; recovery is lost. Reclaim is unknown.",
			})
		}
	}
	return targets
}

func normalizeTargets(targets []target) []target {
	sort.SliceStable(targets, func(i, j int) bool { return len(targets[i].path) < len(targets[j].path) })
	var out []target
	for _, t := range targets {
		overlap := false
		for i, existing := range out {
			if t.path == "" || existing.path == "" {
				continue
			}
			rel, err := filepath.Rel(existing.path, t.path)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				overlap = true
				if existing.category != t.category {
					out[i].category = "temp"
					out[i].reason = "Overlapping cleanup categories use the conservative min-age policy for the entire target."
				}
				break
			}
		}
		if !overlap {
			out = append(out, t)
		}
	}
	return out
}

// checkDirectory rejects symlinks in the target and its ancestor chain.
func checkDirectory(path string) (os.FileInfo, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	for current := path; ; current = filepath.Dir(current) {
		fi, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing non-directory or symlink ancestor: %s", current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return info, nil
}

func buildPlan(ctx context.Context, targets []target, cutoff time.Time, limit int) cleanupPlan {
	p := cleanupPlan{targets: normalizeTargets(targets), complete: true}
	visited := 0
	for _, t := range p.targets {
		a := Action{Target: t.path, Category: t.category, Reason: t.reason, Kind: "files", Complete: true, Status: "planned"}
		var files []candidate
		var rootInfo os.FileInfo
		if t.blocked != "" {
			a.Status, a.Complete, a.Issues = "skipped", false, []string{t.blocked}
		} else if t.command != "" {
			a.Target, a.Kind = t.command+" "+strings.Join(t.args, " "), "command"
		} else {
			var err error
			rootInfo, err = checkDirectory(t.path)
			if os.IsNotExist(err) {
				a.Status, a.Reason = "skipped", "Target does not exist; no cleanup needed."
			} else if err != nil {
				a.Status, a.Complete, a.Issues = "skipped", false, []string{err.Error()}
			} else {
				root, openErr := os.OpenRoot(t.path)
				if openErr != nil {
					a.Complete, a.Issues = false, []string{openErr.Error()}
				} else {
					openedInfo, statErr := root.Lstat(".")
					if statErr != nil {
						err = statErr
					} else if !os.SameFile(rootInfo, openedInfo) {
						err = fmt.Errorf("target changed while opening; preview again")
					} else {
						err = scanTarget(ctx, root, ".", 0, &visited, limit, cutoff, t.category, &a, &files)
					}
					root.Close()
					if err != nil {
						a.Complete = false
						a.Issues = append(a.Issues, err.Error())
					}
				}
			}
		}
		p.complete = p.complete && a.Complete
		p.actions, p.files, p.roots = append(p.actions, a), append(p.files, files), append(p.roots, rootInfo)
	}
	return p
}

func scanTarget(ctx context.Context, root *os.Root, name string, depth int, visited *int, limit int, cutoff time.Time, category string, a *Action, files *[]candidate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 64 {
		return fmt.Errorf("directory nesting exceeds scan limit")
	}
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, readErr := dir.ReadDir(128)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			*visited++
			if *visited > limit {
				return fmt.Errorf("scan limit of %d entries reached; narrow categories and retry", limit)
			}
			path := filepath.Join(name, entry.Name())
			if category == "caches" && protectedCacheEntry(entry.Name()) {
				if len(a.Protected) < 20 {
					a.Protected = append(a.Protected, path)
				}
				continue
			}
			info, err := root.Lstat(path)
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := scanTarget(ctx, root, path, depth+1, visited, limit, cutoff, category, a, files); err != nil {
					return err
				}
			} else if info.Mode().IsRegular() && (category != "temp" || info.ModTime().Before(cutoff)) {
				*files = append(*files, candidate{path: path, info: info})
				a.Bytes += info.Size()
			} else {
				a.Preserved++
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// Preview is read-only and shares target discovery and eligibility with Apply.
func Preview(ctx context.Context, home string, opts Options) Result {
	opts, err := normalizedOptions(opts)
	if err != nil {
		return Result{DryRun: true, Error: err.Error(), Warning: RiskWarning}
	}
	p := buildPlan(ctx, cleanupTargets(runtime.GOOS, home, opts), time.Now().Add(-opts.MinAge), planFileLimit)
	markToolAvailability(&p, exec.LookPath)
	discoverToolCaches(ctx, &p, probeToolCache)
	return previewResult(p)
}

func previewResult(p cleanupPlan) Result {
	r := Result{DryRun: true, Actions: p.actions, Complete: p.complete, Warning: RiskWarning}
	for _, a := range p.actions {
		r.FreedBytes += a.Bytes
	}
	if !p.complete {
		r.Error = "Preview is incomplete; inspect action issues, narrow categories and retry. Incomplete targets will not be cleaned."
	}
	return r
}

func executePlan(ctx context.Context, p cleanupPlan, command func(context.Context, string, ...string) error, remove func(string) error) Result {
	r := Result{Actions: append([]Action(nil), p.actions...), Complete: p.complete, Warning: RiskWarning}
	for i := range r.Actions {
		r.Actions[i].Issues = append([]string(nil), r.Actions[i].Issues...)
	}
	for i := range r.Actions {
		a, t := &r.Actions[i], p.targets[i]
		if a.Status == "skipped" {
			continue
		}
		if !a.Complete {
			a.Status = "skipped"
			a.Issues = append(a.Issues, "Incomplete target was not cleaned; narrow categories and preview again.")
			continue
		}
		if err := ctx.Err(); err != nil {
			a.Status, a.Issues = "skipped", append(a.Issues, err.Error())
			continue
		}
		if t.command != "" {
			if command == nil {
				a.Status, a.Issues = "failed", []string{"command runner unavailable"}
			} else if err := command(ctx, t.command, t.args...); err != nil {
				a.Status, a.Issues = "failed", []string{err.Error()}
			} else {
				a.Status = "completed"
			}
			continue
		}
		info, err := checkDirectory(t.path)
		if err == nil && !os.SameFile(info, p.roots[i]) {
			err = fmt.Errorf("target changed since scan; preview again")
		}
		if err != nil {
			a.Status, a.Issues = "skipped", append(a.Issues, err.Error())
			continue
		}
		root, err := os.OpenRoot(t.path)
		if err != nil {
			a.Status, a.Issues = "failed", []string{err.Error()}
			continue
		}
		openedInfo, err := root.Lstat(".")
		if err != nil || !os.SameFile(openedInfo, p.roots[i]) {
			root.Close()
			a.Status, a.Issues = "skipped", []string{"target changed while opening; preview again"}
			continue
		}
		removed := 0
		for _, f := range p.files[i] {
			err := ctx.Err()
			if err == nil {
				err = revalidate(root, f)
			}
			if err == nil {
				if remove == nil {
					err = root.Remove(f.path)
				} else {
					err = remove(filepath.Join(t.path, f.path))
				}
			}
			if err != nil {
				if len(a.Issues) < 20 {
					a.Issues = append(a.Issues, fmt.Sprintf("%s: %v", f.path, err))
				}
				a.Status = "failed"
			} else {
				a.Removed += f.info.Size()
				removed++
			}
		}
		root.Close()
		if a.Status == "failed" && removed > 0 {
			a.Status = "partial"
		} else if a.Status != "failed" {
			a.Status = "completed"
		}
		r.FreedBytes += a.Removed
		if removed > 0 {
			r.Records = append(r.Records, PurgeRecord{Dir: t.path, Bytes: a.Removed, Entries: removed})
		}
	}
	for _, a := range r.Actions {
		if len(a.Issues) != 0 {
			r.Error = "Some cleanup actions failed or were skipped; inspect action issues and preview again."
			break
		}
	}
	return r
}

func revalidate(root *os.Root, f candidate) error {
	for parent := filepath.Dir(f.path); parent != "."; parent = filepath.Dir(parent) {
		info, err := root.Lstat(parent)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("parent became a symlink or non-directory")
		}
	}
	info, err := root.Lstat(f.path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, f.info) || info.Size() != f.info.Size() || !info.ModTime().Equal(f.info.ModTime()) {
		return fmt.Errorf("file changed since scan; preserved, preview again")
	}
	return nil
}

func runCleanupCommand(ctx context.Context, name string, args ...string) error {
	path, err := exec.LookPath(name)
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s is not installed; install it or omit tools from --categories", name)
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w (check permissions or run the previewed command manually)", name, err)
	}
	return nil
}

func protectedCacheEntry(name string) bool {
	switch strings.ToLower(name) {
	case "uv", "pnpm", "pip", "pip3", "npm", "yarn", "go-build", "conda", "mamba",
		"huggingface", "torch", "whisper", "ollama", "lm-studio",
		"models", "datasets", "checkpoints", "autosave", "autosaves", "backups",
		"steam", "epic", "epicgameslauncher", "battle.net", "adobe", "blender":
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".gguf", ".safetensors", ".onnx", ".ckpt", ".pth", ".pt":
		return true
	}
	return false
}

func markToolAvailability(p *cleanupPlan, lookup func(string) (string, error)) {
	for i, t := range p.targets {
		if t.command == "" {
			continue
		}
		_, err := lookup(t.command)
		if err != nil && t.command == "pip" {
			if _, aliasErr := lookup("pip3"); aliasErr == nil {
				p.targets[i].command = "pip3"
				p.actions[i].Target = "pip3 " + strings.Join(t.args, " ")
				continue
			}
		}
		if err != nil {
			a := &p.actions[i]
			a.Status = "skipped"
			a.Reason += " Tool unavailable: " + err.Error() + "; install it or omit it from --tools."
		}
	}
}
