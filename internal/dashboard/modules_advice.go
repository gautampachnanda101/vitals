package dashboard

import (
	"encoding/json"
	"html"

	"vitals/internal/advice"
	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/guide"
)

func init() {
	Register(Module{Slug: "advice", NavLabel: "Advice", Order: 70, Prepare: prepareAdvice, Available: AnyLLMReachable, UnavailableReason: "no local or cloud LLM is reachable", Render: renderAdvice})
}

// prepareAdvice is the advice module's Prepare hook: the one piece of
// request-scoped work Render can't do on its own, since it needs an
// actual LLM call rather than just formatting already-collected data.
// Failure is absorbed into ctx.AdviceErr rather than returned — the same
// graceful-degradation shape renderAdvice already expects — so the page
// still renders a friendly message instead of an error page when no
// provider answers.
func prepareAdvice(ctx *PageContext) error {
	reportJSON, err := json.Marshal(doctor.JSONReport(ctx.Snapshot, ctx.Report))
	if err != nil {
		ctx.AdviceErr = err
		return nil
	}
	reply, err := advice.Generate(reportJSON, ctx.LLMOpts)
	if err != nil {
		ctx.AdviceErr = err
		return nil
	}
	ctx.AdviceReply = reply
	return nil
}

// renderAdvice shows the LLM's synthesis, fetched via Prepare above —
// Render itself stays pure over whatever ctx.AdviceReply/AdviceErr it's
// handed, consistent with every other module, and testable the same way.
func renderAdvice(ctx PageContext) string {
	if ctx.AdviceErr != nil {
		return `<div class="card"><p class="unavailable">Could not reach a model: ` + html.EscapeString(ctx.AdviceErr.Error()) + `</p></div>`
	}
	body := verdictBanner("Synthesized advice", "", diag.OK)
	body += `<div class="card">` + guide.RenderFragment(ctx.AdviceReply) + `</div>`
	return body
}
