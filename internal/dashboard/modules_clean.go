package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"vitals/internal/clean"
	"vitals/internal/ui"
)

func init() {
	Register(Module{Slug: "clean", NavLabel: "Clean", Group: "Tools", Icon: iconClean, Order: 80, Available: Always, Render: renderCleanPage})
	RegisterWrite(WriteAction{Path: "/clean/preview", Handler: handleCleanPreview})
	RegisterWrite(WriteAction{Path: "/clean/apply", Handler: handleCleanApply})
}

// cleanPageTmpl is the dashboard's first client-side interactivity: a
// button POSTs to /clean/preview and injects the (already
// html/template-escaped, server-rendered) response — no framework, no
// build step, matching the rest of this page's zero-external-asset
// approach. sameOriginOnly (internal/guide/serve.go) is what actually
// protects both POSTs below; neither fetch call needs a token or header,
// consistent with the design's own reasoning that same-origin is the
// real boundary here, not a client-side secret.
//
// The Apply button starts hidden and only appears once a preview
// response has rendered (design.md §7's client-side UX section) — no
// separate state variable, the DOM's own hidden attribute carries it.
// window.confirm() before the actual POST mirrors the CLI's own
// interactive y/N prompt (clean.confirm) in spirit: a deliberate,
// hard-to-mistype last step before a real, irreversible delete, on top
// of (not instead of) the server-side same-origin check.
var cleanPageTmpl = template.Must(template.New("cleanPage").Parse(`<div class="card">
<p class="unavailable"><strong>Cleanup is at your own risk.</strong> Back up important data and close affected applications. Deletion is permanent with no undo; previews do not guarantee files are unused.</p>
<p>Preview selected cleanup actions without deleting anything. Close affected applications first. Apply rescans; a preview is not a frozen file list.</p>
<fieldset id="clean-categories"><legend>Cleanup categories</legend>
<label><input type="checkbox" value="caches" checked> Caches (may need downloading or rebuilding)</label>
<label><input type="checkbox" value="temp" checked> Old temporary files</label>
<label><input type="checkbox" value="logs"> Logs (troubleshooting history is lost)</label>
<label><input type="checkbox" value="trash"> Trash (recovery is lost)</label>
<label><input type="checkbox" value="tools"> Tool commands (may remove unused resources; reclaim unknown)</label>
</fieldset>
<label>Temporary-file minimum age <input id="clean-min-age" value="168h" aria-label="Temporary-file minimum age"></label>
<label>Tool subset (blank selects all installed supported tools) <input id="clean-tools" aria-label="Cleanup tool subset" placeholder="uv,pnpm,conda"></label>
<p>Age is not proof a file is unused. Logical bytes are not guaranteed physical recovery. Empty directories and symlinks are retained.</p>
<button type="button" class="btn" id="clean-preview-btn">Preview</button>
<button type="button" class="btn" id="clean-apply-btn" hidden>Apply</button>
</div>
<div id="clean-preview-result" aria-live="polite"></div>
<div id="clean-apply-result" aria-live="polite"></div>
<script>
(function(){
  var previewBtn = document.getElementById('clean-preview-btn');
  var applyBtn = document.getElementById('clean-apply-btn');
  var out = document.getElementById('clean-preview-result');
  var applyOut = document.getElementById('clean-apply-result');
  var selection = document.getElementById('clean-categories');
  var age = document.getElementById('clean-min-age');
  var tools = document.getElementById('clean-tools');
  function options(){
    return {categories: Array.from(selection.querySelectorAll('input:checked')).map(function(e){return e.value;}).join(','), min_age: age.value, tools: tools.value};
  }
  function invalidate(){ applyBtn.hidden = true; out.innerHTML = ''; }
  selection.addEventListener('change', invalidate);
  age.addEventListener('input', invalidate);
  tools.addEventListener('input', invalidate);
  var previewOptions;
  previewBtn.addEventListener('click', function(){
    previewOptions = options();
    previewBtn.disabled = true;
    applyBtn.hidden = true;
    out.innerHTML = '<p class="unavailable">Measuring…</p>';
    applyOut.innerHTML = '';
    fetch('/clean/preview', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(previewOptions)})
      .then(function(r){ return r.text().then(function(t){ return {ok: r.ok, text: t}; }); })
      .then(function(res){
        if (res.ok) { out.innerHTML = res.text; } else { out.textContent = 'Preview failed: ' + res.text; }
        applyBtn.hidden = !res.ok || !out.querySelector('[data-clean-complete="true"]') || JSON.stringify(previewOptions) !== JSON.stringify(options());
      })
      .catch(function(){
        out.innerHTML = '<p class="unavailable">Preview failed — could not reach the server.</p>';
      })
      .finally(function(){ previewBtn.disabled = false; });
  });
  applyBtn.addEventListener('click', function(){
    if (!window.confirm('Proceed at your own risk? Back up important data first. Apply selected categories and listed tool commands? Files are rescanned; deletion is permanent with no undo.')) {
      return;
    }
    applyBtn.disabled = true;
    applyOut.innerHTML = '<p class="unavailable">Cleaning…</p>';
    fetch('/clean/apply', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(Object.assign({confirm: true}, previewOptions))
    })
      .then(function(r){ return r.text().then(function(t){ return {ok: r.ok, text: t}; }); })
      .then(function(res){
        if (res.ok) { applyOut.innerHTML = res.text; } else { applyOut.textContent = 'Apply failed: ' + res.text; }
        if (res.ok) { applyBtn.hidden = true; }
      })
      .catch(function(){
        applyOut.innerHTML = '<p class="unavailable">Apply failed — could not reach the server.</p>';
      })
      .finally(function(){ applyBtn.disabled = false; });
  });
})();
</script>`))

func renderCleanPage(PageContext) string {
	return mustExecute(cleanPageTmpl, nil)
}

// cleanPreviewBudget bounds how long measuring can take, so a huge
// cache can't stall the request — the same reasoning `vitals disk`
// already applies to its own use of ReclaimableSummary.
const cleanPreviewBudget = 3 * time.Second

// handleCleanPreview is /clean/preview's WriteAction.Handler: measures
// what a real `vitals clean` would reclaim without deleting anything.
// Reuses ReclaimableSummary directly — no new cleanup logic, per
// docs/roadmap/items/005-dashboard-write-actions/design.md §4.
func handleCleanPreview(_ PageContext, body []byte) (int, string) {
	opts, err := cleanRequestOptions(body)
	if err != nil {
		return http.StatusBadRequest, cleanRequestError(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return http.StatusInternalServerError, `{"error":"cannot determine home directory"}`
	}
	ctx, cancel := context.WithTimeout(context.Background(), cleanPreviewBudget)
	defer cancel()
	return http.StatusOK, renderCleanPlan(clean.Preview(ctx, home, opts))
}

// cleanPreviewRow is one measured directory, pre-formatted for display
// — kept separate from clean.CacheEntry so the template only ever sees
// already-humanized strings, never a raw int64 it would have to format
// itself.
type cleanPreviewRow struct {
	Path  string
	Bytes string
}

// cleanPreviewTmpl renders through html/template, not manual string
// concatenation — Path comes from this machine's real filesystem
// (cache directory names), which on any OS can contain characters a
// hand-written string-concat render would get wrong to escape; see
// TestRenderCleanPreviewEscapesACraftedPath.
var cleanPreviewTmpl = template.Must(template.New("cleanPreview").Parse(
	`<div class="card"><h3>Would reclaim: {{.Total}}</h3>` +
		`{{if not .Complete}}<p class="unavailable">Measurement stopped early (a very large cache) — this list may be incomplete.</p>{{end}}` +
		`{{if .Rows}}{{range .Rows}}<div class="row"><span class="k">{{.Path}}</span><span>{{.Bytes}}</span></div>{{end}}` +
		`{{else}}<p class="unavailable">Nothing measurable found.</p>{{end}}</div>`))

func renderCleanPreview(entries []clean.CacheEntry, total int64, complete bool) string {
	rows := make([]cleanPreviewRow, len(entries))
	for i, e := range entries {
		rows[i] = cleanPreviewRow{Path: e.Path, Bytes: ui.HumanBytes(e.Bytes)}
	}
	return mustExecute(cleanPreviewTmpl, struct {
		Total    string
		Complete bool
		Rows     []cleanPreviewRow
	}{ui.HumanBytes(total), complete, rows})
}

// cleanApplyFn is the live, actually-deleting call handleCleanApply
// makes — a function value, not a direct clean.Apply reference, so a
// test can substitute a fake and exercise handleCleanApply's own logic
// (confirm parsing, the single-flight guard, rendering) without ever
// running a real clean.Apply call. That distinction matters here more
// than for handleCleanPreview: clean.Apply in non-dry-run mode is never
// safe to call from an automated test on any OS — cleanLinux's /var/tmp
// and /tmp purge ignores its home parameter entirely, and cleanDevCaches
// unconditionally shells out to real package managers (docker, npm,
// pnpm, pip) when they're on PATH — see
// internal/clean/clean_test.go's TestApplyDryRunNeverMutatesAndReturnsAStructuredResult
// for exactly this reasoning applied to clean's own test suite.
var cleanApplyFn = clean.Apply

// cleanApplyMu is the single-flight guard against concurrent
// /clean/apply calls (design.md §7). TryLock rejects a second concurrent
// caller outright (409) rather than queuing it — a concurrent apply
// request while a real one is in flight is exactly the failure mode a
// double-click or two open browser tabs would produce, and a second
// apply should still be allowed to run once the first one finishes, just
// never concurrently with it.
var cleanApplyMu sync.Mutex

// cleanApplyRequest is /clean/apply's request body — a fixed struct, not
// a generic map, so an unknown extra field is silently ignored (encoding/
// json's own default) rather than rejected: this endpoint only ever
// needs to check one boolean, and a stricter web-only schema requirement
// would be inventing a bar the CLI's own --yes/-y flag doesn't have
// either. See design.md §7.
type cleanApplyRequest struct {
	Confirm    bool    `json:"confirm"`
	Categories *string `json:"categories"`
	MinAge     string  `json:"min_age"`
	Tools      string  `json:"tools"`
}

func cleanRequestOptions(body []byte) (clean.Options, error) {
	var req cleanApplyRequest
	if len(body) != 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			return clean.Options{}, err
		}
	}
	categories := ""
	if req.Categories != nil {
		categories = *req.Categories
		if strings.TrimSpace(categories) == "" {
			return clean.Options{}, fmt.Errorf("select at least one cleanup category")
		}
	}
	age := 7 * 24 * time.Hour
	if req.MinAge != "" {
		var err error
		age, err = time.ParseDuration(req.MinAge)
		if err != nil {
			return clean.Options{}, fmt.Errorf("min_age must be a duration such as 168h")
		}
	}
	opts, err := clean.ParseOptions(categories, age)
	if err == nil {
		opts.Tools = req.Tools
		err = clean.ValidateOptions(opts)
	}
	opts.MinAgeSet = true
	return opts, err
}

func cleanRequestError(err error) string {
	body, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{err.Error()})
	return string(body)
}

// handleCleanApply is /clean/apply's WriteAction.Handler: the real,
// deleting counterpart to handleCleanPreview above. A missing, false, or
// malformed confirm is rejected with 400 before cleanApplyFn is ever
// called — this mirrors the CLI's own --yes/-y bar (a deliberate,
// hard-to-mistype confirmation) rather than a stricter web-only
// requirement, since the same-origin check (guide.ServeLocal) is what
// actually defends against an unintended trigger; confirm defends
// against a client-side bug (a button wired to the wrong handler) more
// than an attacker. See design.md §7, including its security review
// outcome, for the full reasoning.
func handleCleanApply(_ PageContext, body []byte) (int, string) {
	var req cleanApplyRequest
	if err := json.Unmarshal(body, &req); err != nil || !req.Confirm {
		return http.StatusBadRequest, `{"error":"confirm must be true"}`
	}
	opts, err := cleanRequestOptions(body)
	if err != nil {
		return http.StatusBadRequest, cleanRequestError(err)
	}
	if !cleanApplyMu.TryLock() {
		return http.StatusConflict, `{"error":"a clean is already running"}`
	}
	defer cleanApplyMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return http.StatusInternalServerError, `{"error":"cannot determine home directory"}`
	}
	opts.Assume = true
	result := cleanApplyFn(home, opts)
	return http.StatusOK, renderCleanApplyResult(result)
}

// cleanApplyRow is one purge record, pre-formatted for display — mirrors
// cleanPreviewRow's reasoning: the template only ever sees an
// already-humanized string, never a raw int64.
type cleanApplyRow struct {
	Dir   string
	Bytes string
}

// cleanApplyTmpl renders through html/template for the same reason
// cleanPreviewTmpl does: PurgeRecord.Dir is real filesystem data (cache
// directory names) this handler doesn't control. See
// TestRenderCleanApplyResultEscapesACraftedPath.
var cleanApplyTmpl = template.Must(template.New("cleanApply").Parse(
	`<div class="card"><h3>Freed: {{.Total}}</h3>` +
		`{{if .Rows}}{{range .Rows}}<div class="row"><span class="k">{{.Dir}}</span><span>{{.Bytes}}</span></div>{{end}}` +
		`{{else}}<p class="unavailable">Nothing was removed.</p>{{end}}</div>`))

// renderCleanApplyResult mirrors renderCleanPreview exactly: total
// first, then one row per non-empty purge location.
func renderCleanApplyResult(result clean.Result) string {
	if len(result.Actions) > 0 || result.Error != "" {
		return renderCleanPlan(result)
	}
	rows := make([]cleanApplyRow, len(result.Records))
	for i, r := range result.Records {
		rows[i] = cleanApplyRow{Dir: r.Dir, Bytes: ui.HumanBytes(r.Bytes)}
	}

	return mustExecute(cleanApplyTmpl, struct {
		Total string
		Rows  []cleanApplyRow
	}{ui.HumanBytes(result.FreedBytes), rows})
}

var cleanPlanTmpl = template.Must(template.New("cleanPlan").Parse(`<div class="card" data-clean-complete="{{.Complete}}">
<h3>{{if .DryRun}}Would reclaim{{else}}Removed{{end}} (logical file bytes): {{.Total}}</h3>
<p class="unavailable">{{.Warning}}</p>
{{if .Error}}<p class="unavailable">{{.Error}}</p>{{end}}
{{range .Rows}}<div class="card"><strong>{{.Category}} — {{.Status}}</strong>
<div>{{.Target}}</div><p>{{.Reason}}</p><p>{{.Size}}; preserved files: {{.Preserved}}</p>
{{if .Discovery}}<p>Discovery: {{.Discovery}}</p>{{end}}
{{range .Locations}}<p>Discovered cache: {{.}}</p>{{end}}
{{range .Issues}}<p class="unavailable">{{.}}</p>{{end}}
{{range .Protected}}<p>Protected: {{.}} — models/data or manager-owned cache; review with the owning tool or <code>vitals explore</code>.</p>{{end}}
</div>{{else}}<p>Nothing measurable found. No files need cleaning in these categories; see vitals guide.</p>{{end}}</div>`))

func renderCleanPlan(result clean.Result) string {
	type row struct {
		clean.Action
		Size string
	}
	rows := make([]row, len(result.Actions))
	for i, a := range result.Actions {
		bytes := a.Removed
		if result.DryRun {
			bytes = a.Bytes
		}
		size := ui.HumanBytes(bytes)
		if a.Kind == "command" {
			size = "Reclaim unknown"
		}
		rows[i] = row{a, size}
	}
	return mustExecute(cleanPlanTmpl, struct {
		DryRun, Complete bool
		Total, Error     string
		Warning          string
		Rows             []row
	}{result.DryRun, result.Complete, ui.HumanBytes(result.FreedBytes), result.Error, clean.RiskWarning, rows})
}
