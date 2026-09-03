package dashboard

import (
	"html/template"
	"net/http"
	"time"

	"vitals/internal/clean"
	"vitals/internal/ui"
)

func init() {
	Register(Module{Slug: "clean", NavLabel: "Clean", Order: 80, Available: Always, Render: renderCleanPage})
	RegisterWrite(WriteAction{Path: "/clean/preview", Handler: handleCleanPreview})
}

// cleanPageTmpl is the dashboard's first client-side interactivity: a
// button POSTs to /clean/preview and injects the (already
// html/template-escaped, server-rendered) response — no framework, no
// build step, matching the rest of this page's zero-external-asset
// approach. sameOriginOnly (internal/guide/serve.go) is what actually
// protects this POST; the fetch call itself needs no token or header,
// consistent with the design's own reasoning that same-origin is the
// real boundary here, not a client-side secret.
var cleanPageTmpl = template.Must(template.New("cleanPage").Parse(`<div class="card">
<p>Measure what a real <code>vitals clean</code> run would reclaim — cache, log and temp files — without deleting anything.</p>
<button type="button" class="btn" id="clean-preview-btn">Preview</button>
</div>
<div id="clean-preview-result" aria-live="polite"></div>
<script>
(function(){
  var btn = document.getElementById('clean-preview-btn');
  var out = document.getElementById('clean-preview-result');
  btn.addEventListener('click', function(){
    btn.disabled = true;
    out.innerHTML = '<p class="unavailable">Measuring…</p>';
    fetch('/clean/preview', {method: 'POST'})
      .then(function(r){ return r.text().then(function(t){ return {ok: r.ok, text: t}; }); })
      .then(function(res){
        out.innerHTML = res.ok ? res.text : '<p class="unavailable">Preview failed.</p>';
      })
      .catch(function(){
        out.innerHTML = '<p class="unavailable">Preview failed — could not reach the server.</p>';
      })
      .finally(function(){ btn.disabled = false; });
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
func handleCleanPreview(_ PageContext, _ []byte) (int, string) {
	entries, complete := clean.ReclaimableSummary(cleanPreviewBudget)
	var total int64
	for _, e := range entries {
		total += e.Bytes
	}
	return http.StatusOK, renderCleanPreview(entries, total, complete)
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
