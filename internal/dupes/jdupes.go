package dupes

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// jdupes is an optional, much faster duplicate-finder backend
// (roadmap item 010). It's opt-in per run (`vitals dupes --fast`): the
// built-in Scan stays the default because the two aren't perfectly
// equivalent — jdupes' JSON reports no total-scanned count, so the
// "scanned N files" line is replaced by a backend note, and this file
// re-applies skipDirNames itself since jdupes has no matching filter.

// jdupesRunner runs jdupes and returns its raw stdout — injectable so the
// parse/filter/convert logic is tested without the binary installed.
type jdupesRunner func(args []string) ([]byte, error)

var defaultJdupesRunner jdupesRunner = func(args []string) ([]byte, error) {
	return exec.Command("jdupes", args...).Output()
}

// jdupesAvailable reports whether the jdupes binary is on PATH.
func jdupesAvailable() bool {
	_, err := exec.LookPath("jdupes")
	return err == nil
}

// jdupesJSON is the shape of `jdupes -j` output — only the fields vitals
// uses. Validated against real jdupes output (see item 010's plan):
// matchSets is [] (never null) when nothing is found.
type jdupesJSON struct {
	ExitStatus int `json:"exitStatus"`
	MatchSets  []struct {
		FileSize int64 `json:"fileSize"`
		FileList []struct {
			FilePath string `json:"filePath"`
		} `json:"fileList"`
	} `json:"matchSets"`
}

// scanWithJdupes runs jdupes over root and returns a Result with
// Backend set to "jdupes". ok is false when jdupes couldn't be used
// (not installed, non-zero exit, or output that doesn't parse as the
// expected JSON) — the caller then falls back to the built-in Scan
// rather than surfacing a partial or wrong result. A real jdupes error
// (e.g. a bad path) exits non-zero with no JSON at all, which this
// treats the same as unparseable: fall back.
func scanWithJdupes(run jdupesRunner, root string, minSize int64) (Result, bool) {
	args := []string{"-r", "-j"}
	if minSize > 0 {
		// jdupes' own min-size filter — confirmed exactly equivalent to
		// Scan's `>= minSize` semantics.
		args = append(args, "-X", "size+=:"+strconv.FormatInt(minSize, 10))
	}
	args = append(args, root)

	out, err := run(args)
	if err != nil {
		return Result{}, false
	}
	var parsed jdupesJSON
	if json.Unmarshal(out, &parsed) != nil {
		return Result{}, false
	}

	groups := make([]Group, 0, len(parsed.MatchSets))
	var wasted int64
	for _, ms := range parsed.MatchSets {
		paths := make([]string, 0, len(ms.FileList))
		for _, f := range ms.FileList {
			if pathHasSkippedDir(f.FilePath, root) {
				continue // re-apply skipDirNames — jdupes has no equivalent
			}
			paths = append(paths, f.FilePath)
		}
		if len(paths) < 2 {
			continue // filtering dropped it below a real duplicate group
		}
		sort.Strings(paths)
		g := Group{SizeBytes: ms.FileSize, Paths: paths}
		groups = append(groups, g)
		wasted += g.WastedBytes()
	}
	// same ordering the built-in Scan uses: most reclaimable first.
	sort.Slice(groups, func(i, j int) bool { return groups[i].WastedBytes() > groups[j].WastedBytes() })

	return Result{
		Root:        root,
		Groups:      groups,
		WastedBytes: wasted,
		Backend:     "jdupes",
		// ScannedFiles/ScannedBytes deliberately left 0: jdupes reports
		// no total-scanned count. render/renderDupesPreview show the
		// backend note instead of a "scanned N files" line it can't
		// honestly fill.
	}, true
}

// pathHasSkippedDir reports whether any path component between root and
// the file is in skipDirNames — the Go-side re-application of the
// built-in Scan's own directory pruning, kept as the single source of
// truth rather than a parallel set of jdupes -X flags to keep in sync.
func pathHasSkippedDir(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	// jdupes prints whatever separator the OS uses; split on either so
	// the check is the same on every platform.
	for _, part := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
		if skipDirNames[part] {
			return true
		}
	}
	return false
}
