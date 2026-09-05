package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"vitals/internal/dupes"
	"vitals/internal/ui"
)

func init() {
	Register(Module{Slug: "dupes", NavLabel: "Duplicates", Order: 90, Available: Always, Render: renderDupesPage})
	RegisterWrite(WriteAction{Path: "/dupes/preview", Handler: handleDupesPreview})
	RegisterWrite(WriteAction{Path: "/dupes/hardlink", Handler: handleDupesHardlink})
}

// dupesScope is one entry in the fixed, server-side scope enum
// design-dupes.md §2 requires: the request body never carries a
// directory path, only a key naming one of these — the same reasoning
// internal/memhogs' fixed embedded family list and /clean/apply's
// server-derived home directory both already apply, closing the
// arbitrary-path surface a raw "root" field would open.
const (
	dupesMinSize = 1 << 20 // 1 MiB, matching dupes.Options' own default
)

// resolveScope turns a client-chosen scope key into the root directory
// and minimum file size to scan, or ok=false for an unknown key or one
// this OS doesn't support (goos is injected so this stays pure and
// table-testable on every OS from a single machine, not just the one
// running the test).
func resolveScope(key, home, goos string) (root string, minSize int64, ok bool) {
	switch key {
	case "home":
		return home, dupesMinSize, true
	case "downloads":
		return filepath.Join(home, "Downloads"), dupesMinSize, true
	case "caches":
		switch goos {
		case "darwin":
			return filepath.Join(home, "Library", "Caches"), dupesMinSize, true
		case "linux":
			return filepath.Join(home, ".cache"), dupesMinSize, true
		default:
			return "", 0, false // no equivalent single well-known cache root on Windows
		}
	default:
		return "", 0, false
	}
}

// dupesScopeLabels drives both the page's <select> options and the
// error message for an unresolvable scope — one list, so the client
// never offers a key the server would reject as unknown.
var dupesScopeLabels = []struct{ Key, Label string }{
	{"home", "Home directory"},
	{"downloads", "Downloads"},
	{"caches", "Caches (macOS/Linux only)"},
}

// dupesPageTmpl mirrors modules_clean.go's cleanPageTmpl exactly —
// same preview-then-apply shape, same window.confirm() gate before the
// one destructive call (matching this repo's own standing rule: every
// destructive action needs an explicit confirm step on both the CLI
// and the dashboard, not just one). The one addition is the scope
// <select>, echoed into both requests so an Apply always re-targets
// whatever was actually previewed.
var dupesPageTmpl = template.Must(template.New("dupesPage").Parse(`<div class="card">
<p>Find duplicate files and, if you choose, replace them with hardlinks to reclaim space — this destroys no data even if it's wrong: every path keeps working and keeps reading the same bytes.</p>
<label for="dupes-scope">Scope</label>
<select id="dupes-scope">{{range .Scopes}}<option value="{{.Key}}">{{.Label}}</option>{{end}}</select>
<button type="button" class="btn" id="dupes-preview-btn">Preview</button>
<button type="button" class="btn" id="dupes-hardlink-btn" hidden>Hardlink duplicates</button>
</div>
<div id="dupes-preview-result" aria-live="polite"></div>
<div id="dupes-hardlink-result" aria-live="polite"></div>
<script>
(function(){
  var scope = document.getElementById('dupes-scope');
  var previewBtn = document.getElementById('dupes-preview-btn');
  var hardlinkBtn = document.getElementById('dupes-hardlink-btn');
  var out = document.getElementById('dupes-preview-result');
  var hardlinkOut = document.getElementById('dupes-hardlink-result');
  previewBtn.addEventListener('click', function(){
    previewBtn.disabled = true;
    hardlinkBtn.hidden = true;
    out.innerHTML = '<p class="unavailable">Scanning…</p>';
    hardlinkOut.innerHTML = '';
    fetch('/dupes/preview', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({scope: scope.value})
    })
      .then(function(r){ return r.text().then(function(t){ return {ok: r.ok, text: t}; }); })
      .then(function(res){
        out.innerHTML = res.ok ? res.text : ('<p class="unavailable">Preview failed: ' + res.text + '</p>');
        hardlinkBtn.hidden = !res.ok;
      })
      .catch(function(){
        out.innerHTML = '<p class="unavailable">Preview failed — could not reach the server.</p>';
      })
      .finally(function(){ previewBtn.disabled = false; });
  });
  hardlinkBtn.addEventListener('click', function(){
    if (!window.confirm('This replaces duplicate files with hardlinks (no data is deleted). Continue?')) {
      return;
    }
    hardlinkBtn.disabled = true;
    hardlinkOut.innerHTML = '<p class="unavailable">Hardlinking…</p>';
    fetch('/dupes/hardlink', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({confirm: true, scope: scope.value})
    })
      .then(function(r){ return r.text().then(function(t){ return {ok: r.ok, text: t}; }); })
      .then(function(res){
        hardlinkOut.innerHTML = res.ok ? res.text : ('<p class="unavailable">Hardlink failed: ' + res.text + '</p>');
        if (res.ok) { hardlinkBtn.hidden = true; }
      })
      .catch(function(){
        hardlinkOut.innerHTML = '<p class="unavailable">Hardlink failed — could not reach the server.</p>';
      })
      .finally(function(){ hardlinkBtn.disabled = false; });
  });
})();
</script>`))

func renderDupesPage(PageContext) string {
	return mustExecute(dupesPageTmpl, struct{ Scopes []struct{ Key, Label string } }{dupesScopeLabels})
}

// dupesPreviewBudget/dupesHardlinkBudget bound how long a scan can run —
// design-dupes.md §4's own numbers: 30s for a read-only preview, longer
// but still bounded for the apply path (which re-scans before linking).
// dupesFileBudget caps how many files a single scan considers regardless
// of how long that takes, the second half of §4's dual bound: a
// slow-but-shallow network mount and a fast-but-enormous local tree are
// both real, and only one of the two limits catches each case.
const (
	dupesPreviewBudget  = 30 * time.Second
	dupesHardlinkBudget = 60 * time.Second
	dupesFileBudget     = 200_000
)

// dupesScopeRequest is both write actions' shared request shape for the
// scope field — confirm is dupesHardlinkRequest-only, added there by
// embedding this.
type dupesScopeRequest struct {
	Scope string `json:"scope"`
}

// handleDupesPreview is /dupes/preview's WriteAction.Handler: a
// read-only scan of the resolved scope, never guarded by the
// single-flight mutex (a redundant concurrent preview just repeats
// work, matching /clean/preview's own reasoning).
func handleDupesPreview(_ PageContext, body []byte) (int, string) {
	var req dupesScopeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return http.StatusBadRequest, `{"error":"malformed request body"}`
	}
	root, minSize, ok := resolveScopeOnThisHost(req.Scope)
	if !ok {
		return http.StatusBadRequest, fmt.Sprintf(`{"error":"unknown or unsupported scope %q"}`, req.Scope)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dupesPreviewBudget)
	defer cancel()
	result, err := dupes.ScanContext(ctx, root, minSize, dupesFileBudget)
	if err != nil {
		return http.StatusInternalServerError, `{"error":"scan failed"}`
	}
	return http.StatusOK, renderDupesPreview(result)
}

// resolveScopeOnThisHost wraps resolveScope with the live home
// directory and OS — the one place handleDupesPreview/handleDupesHardlink
// touch real host state, so resolveScope itself stays pure and
// table-testable.
func resolveScopeOnThisHost(key string) (root string, minSize int64, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", 0, false
	}
	return resolveScope(key, home, runtime.GOOS)
}

// dupesPathDisplay splits a path into its directory and filename so the
// template can give the filename — the part someone actually scans for —
// more visual weight than the directory, the same dimmed-path/prominent-
// name convention a file explorer uses, instead of one long monospace
// line where the filename is buried at the end and every entry looks the
// same at a glance.
type dupesPathDisplay struct {
	Dir  string // includes the trailing separator's worth of context, no trailing slash itself
	Name string
}

func newDupesPathDisplay(path string) dupesPathDisplay {
	return dupesPathDisplay{Dir: filepath.Dir(path), Name: filepath.Base(path)}
}

// dupesPreviewRow is one duplicate group, pre-formatted for display —
// same reasoning as cleanPreviewRow: the template only ever sees
// already-humanized strings and already-escaping-safe path lists.
type dupesPreviewRow struct {
	Wasted string
	Count  int
	Size   string
	Paths  []dupesPathDisplay
}

// dupesGroupDisplayLimit caps how many groups a preview response shows,
// matching `vitals dupes`' own --top default (dupes.go's Options.Top) —
// a real home can have far more groups than are useful to render inline.
const dupesGroupDisplayLimit = 20

var dupesPreviewTmpl = template.Must(template.New("dupesPreview").Parse(
	`<div class="card"><h3>{{.Total}} reclaimable</h3>` +
		`<p class="summary">scanned {{.ScannedFiles}} files under {{.Root}}</p>` +
		`{{if .Truncated}}<p class="unavailable">Scan stopped early (a very large tree) — this list may be incomplete.</p>{{end}}` +
		`{{if .Rows}}{{range .Rows}}<div class="row"><span class="k">{{.Wasted}} — {{.Count}} copies of a {{.Size}} file</span></div>` +
		`{{range .Paths}}<div class="path"><div class="dir mono">{{.Dir}}</div><div class="name mono">{{.Name}}</div></div>{{end}}{{end}}` +
		`{{if .More}}<p class="unavailable">...and {{.More}} more group(s) — raise --top or use the CLI's --json for the full list</p>{{end}}` +
		`{{else}}<p class="unavailable">No duplicate files found above the size threshold.</p>{{end}}</div>`))

func renderDupesPreview(r dupes.Result) string {
	n := len(r.Groups)
	if n > dupesGroupDisplayLimit {
		n = dupesGroupDisplayLimit
	}
	rows := make([]dupesPreviewRow, n)
	for i := 0; i < n; i++ {
		g := r.Groups[i]
		paths := make([]dupesPathDisplay, len(g.Paths))
		for j, p := range g.Paths {
			paths[j] = newDupesPathDisplay(p)
		}
		rows[i] = dupesPreviewRow{
			Wasted: ui.HumanBytes(g.WastedBytes()),
			Count:  len(g.Paths),
			Size:   ui.HumanBytes(g.SizeBytes),
			Paths:  paths,
		}
	}
	return mustExecute(dupesPreviewTmpl, struct {
		Total        string
		ScannedFiles int64
		Root         string
		Truncated    bool
		Rows         []dupesPreviewRow
		More         int
	}{ui.HumanBytes(r.WastedBytes), r.ScannedFiles, r.Root, r.Truncated, rows, len(r.Groups) - n})
}

// dupesApplyFn is the live, actually-linking call handleDupesHardlink
// makes — a function value, not a direct dupes.ApplyHardlinks
// reference, matching cleanApplyFn's own reasoning: a test substitutes
// a fake to exercise handleDupesHardlink's own logic (confirm/scope
// parsing, the single-flight guard, rendering) without depending on
// real filesystem mutation for every branch.
var dupesApplyFn = dupes.ApplyHardlinks

// dupesApplyMu is the single-flight guard against concurrent
// /dupes/hardlink calls — identical shape to cleanApplyMu.
// /dupes/preview is deliberately unguarded, matching /clean/preview.
var dupesApplyMu sync.Mutex

type dupesHardlinkRequest struct {
	Confirm bool   `json:"confirm"`
	Scope   string `json:"scope"`
}

// handleDupesHardlink is /dupes/hardlink's WriteAction.Handler. Confirm
// must be exactly true and scope must resolve, both checked before any
// scan runs; on success it re-runs Scan server-side and links only the
// groups that scan just found — never a group/path list the client
// sent, which would reintroduce the arbitrary-path surface the fixed
// scope enum exists to remove (design-dupes.md §3). This mirrors the
// CLI's own --yes/-y bar and the dashboard's own standing rule that
// every destructive action needs an explicit confirm on both surfaces
// (the client's window.confirm() above is the human-facing half of
// that; this check is the server-side half).
func handleDupesHardlink(_ PageContext, body []byte) (int, string) {
	var req dupesHardlinkRequest
	if err := json.Unmarshal(body, &req); err != nil || !req.Confirm {
		return http.StatusBadRequest, `{"error":"confirm must be true"}`
	}
	root, minSize, ok := resolveScopeOnThisHost(req.Scope)
	if !ok {
		return http.StatusBadRequest, fmt.Sprintf(`{"error":"unknown or unsupported scope %q"}`, req.Scope)
	}
	if !dupesApplyMu.TryLock() {
		return http.StatusConflict, `{"error":"a hardlink apply is already running"}`
	}
	defer dupesApplyMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), dupesHardlinkBudget)
	defer cancel()
	result, err := dupes.ScanContext(ctx, root, minSize, dupesFileBudget)
	if err != nil {
		return http.StatusInternalServerError, `{"error":"scan failed"}`
	}
	linked, reclaimed, failures := dupesApplyFn(result.Groups)
	return http.StatusOK, renderDupesHardlinkResult(linked, reclaimed, failures)
}

var dupesHardlinkResultTmpl = template.Must(template.New("dupesHardlinkResult").Parse(
	`<div class="card"><h3>Hardlinked {{.Linked}} file(s), reclaiming {{.Reclaimed}}</h3>` +
		`{{range .Failures}}<div class="row"><span class="k mono">{{.}}</span></div>{{end}}</div>`))

func renderDupesHardlinkResult(linked int, reclaimed int64, failures []string) string {
	return mustExecute(dupesHardlinkResultTmpl, struct {
		Linked    int
		Reclaimed string
		Failures  []string
	}{linked, ui.HumanBytes(reclaimed), failures})
}
