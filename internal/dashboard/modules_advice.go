package dashboard

import (
	"encoding/json"
	"html/template"
	"sync"
	"time"

	"vitals/internal/advice"
	"vitals/internal/doctor"
	"vitals/internal/guide"
)

func init() {
	// Available: Always — the heuristic half of this page (see renderAdvice)
	// needs no LLM at all, so the page itself shouldn't disappear from the
	// nav just because none is reachable; only the LLM commentary section
	// is conditional on that.
	Register(Module{Slug: "advice", NavLabel: "Advice", Order: 70, Prepare: prepareAdvice, Available: Always, Render: renderAdvice})
}

// prepareAdviceCacheTTL bounds how stale a synthesized advice reply can
// be. Deliberately longer than snapshotCacheTTL: generating a real
// completion is slow and costs real API quota, and the answer doesn't
// meaningfully change every few seconds the way raw metrics do. This
// cache is what actually fixes a real bug found by review after item 002
// shipped: every request to /advice was triggering a fresh, uncached LLM
// call, unlike every other route, which route (dashboard.go) already
// protects via snapshotCache.
const prepareAdviceCacheTTL = 30 * time.Second

// prepareAdviceCache is a single-flight, TTL cache for prepareAdvice's
// one real cost — the LLM call — same shape as snapshotCache
// (snapshot_cache.go). A generic, slug-keyed version of this (so any
// future Prepare hook would get this for free automatically) was
// considered and deliberately not built: advice is still the only module
// using Prepare today, and this codebase's own convention is to extract
// a shared abstraction when a second real case shows up, not
// speculatively for a hypothetical one.
type prepareAdviceCache struct {
	ttl      time.Duration
	generate func(*PageContext) (string, error)

	mu      sync.Mutex
	reply   string
	err     error
	expiry  time.Time
	loading chan struct{}
}

func newPrepareAdviceCache() *prepareAdviceCache {
	return &prepareAdviceCache{ttl: prepareAdviceCacheTTL, generate: generateAdvice}
}

// Get returns the cached advice, generating it first if missing or
// stale. A caller that arrives mid-refresh waits on that same refresh
// rather than starting its own — identical shape to snapshotCache.Get.
func (c *prepareAdviceCache) Get(ctx *PageContext) (string, error) {
	c.mu.Lock()
	if time.Now().Before(c.expiry) {
		reply, err := c.reply, c.err
		c.mu.Unlock()
		return reply, err
	}
	if c.loading != nil {
		ch := c.loading
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		reply, err := c.reply, c.err
		c.mu.Unlock()
		return reply, err
	}
	ch := make(chan struct{})
	c.loading = ch
	c.mu.Unlock()

	reply, err := c.generate(ctx)

	c.mu.Lock()
	c.reply, c.err = reply, err
	c.expiry = time.Now().Add(c.ttl)
	c.loading = nil
	c.mu.Unlock()
	close(ch)

	return reply, err
}

// defaultAdviceCache is the one instance prepareAdvice uses in the real
// binary — a single `vitals dashboard` process serves one machine, so a
// package-level cache is safe there. Tests that need a clean cache use
// withFreshAdviceCache (modules_test.go), the same swap-and-restore
// pattern withRegistry uses for the module registry.
var defaultAdviceCache = newPrepareAdviceCache()

// generateAdvice does the actual LLM call — split out from prepareAdvice
// so prepareAdviceCache.Get has something pure-ish (given a ctx) to
// memoize.
func generateAdvice(ctx *PageContext) (string, error) {
	reportJSON, err := json.Marshal(doctor.JSONReport(ctx.Snapshot, ctx.Report))
	if err != nil {
		return "", err
	}
	return advice.Generate(reportJSON, ctx.LLMOpts)
}

// prepareAdvice is the advice module's Prepare hook: the one piece of
// request-scoped work Render can't do on its own, since it needs an
// actual LLM call rather than just formatting already-collected data.
// Failure is absorbed into ctx.AdviceErr rather than returned — the same
// graceful-degradation shape renderAdvice already expects — so the page
// still renders a friendly message instead of an error page when no
// provider answers.
func prepareAdvice(ctx *PageContext) error {
	reply, err := defaultAdviceCache.Get(ctx)
	if err != nil {
		ctx.AdviceErr = err
		return nil
	}
	ctx.AdviceReply = reply
	return nil
}

// adviceErrorTmpl renders ctx.AdviceErr's message — an error string that
// can embed arbitrary provider/network detail, so it goes through
// html/template's auto-escaping like every other render function in this
// package, not a manual html.EscapeString call. It's shown alongside the
// heuristic findings, not instead of them: the LLM is a complement, so
// its being unreachable is a quiet note here, not a page-level failure.
var adviceErrorTmpl = template.Must(template.New("adviceError").Parse(
	`<div class="card"><p class="unavailable">No LLM reachable for further AI commentary: {{.}}</p></div>`))

// renderAdvice shows this machine's rule-based findings first — the same
// data and layout the overview page uses (reportHeadline/verdictBanner/
// findingsList), needing no LLM at all — then, when Prepare above managed
// to reach one, an LLM commentary section underneath adding whatever a
// per-finding list can't: shared root causes, what matters most when
// there's more than one issue. Render itself stays pure over whatever
// ctx.Report/AdviceReply/AdviceErr it's handed, consistent with every
// other module, and testable the same way.
func renderAdvice(ctx PageContext) string {
	headline := reportHeadline(ctx.Report, "Healthy — nothing needs attention")
	body := verdictBanner(headline, "What vitals doctor's own rule-based checks found", ctx.Report.Worst())
	body += `<div class="card">` + findingsList(ctx.Report.SortedBySeverity()) + `</div>`

	switch {
	case ctx.AdviceReply != "":
		body += `<div class="card"><h3>AI commentary</h3>` + guide.RenderFragment(ctx.AdviceReply) + `</div>`
	case ctx.AdviceErr != nil:
		body += mustExecute(adviceErrorTmpl, ctx.AdviceErr.Error())
	}
	return body
}
