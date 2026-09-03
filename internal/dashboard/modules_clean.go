package dashboard

import (
	"html/template"
	"net/http"
	"time"

	"vitals/internal/clean"
	"vitals/internal/ui"
)

func init() {
	RegisterWrite(WriteAction{Path: "/clean/preview", Handler: handleCleanPreview})
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
