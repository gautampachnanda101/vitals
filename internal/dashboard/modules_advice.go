package dashboard

import (
	"html"

	"vitals/internal/diag"
	"vitals/internal/guide"
)

func init() {
	Register(Module{Slug: "advice", NavLabel: "Advice", Order: 70, Available: AnyLLMReachable, Render: renderAdvice})
}

// renderAdvice shows the LLM's synthesis, fetched by the HTTP handler
// specifically for this route (see dashboard.go) — Render itself stays
// pure over whatever ctx.AdviceReply/AdviceErr it's handed, consistent
// with every other module, and testable the same way.
func renderAdvice(ctx PageContext) string {
	if ctx.AdviceErr != nil {
		return `<div class="card"><p class="unavailable">Could not reach a model: ` + html.EscapeString(ctx.AdviceErr.Error()) + `</p></div>`
	}
	body := verdictBanner("Synthesized advice", "", diag.OK)
	body += `<div class="card">` + guide.RenderFragment(ctx.AdviceReply) + `</div>`
	return body
}
