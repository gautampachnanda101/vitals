package dashboard

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"sync"
	"time"

	"vitals/internal/advice"
	"vitals/internal/diskusage"
	"vitals/internal/doctor"
	"vitals/internal/guide"
	"vitals/internal/llm"
)

func init() {
	// Available: Always — the heuristic half of this page (see renderAdvice)
	// needs no LLM at all, so the page itself shouldn't disappear from the
	// nav just because none is reachable; only the LLM commentary section
	// is conditional on that.
	Register(Module{Slug: "advice", NavLabel: "Advice", Group: "Intelligence", Icon: iconAdvice, Order: 70, Available: Always, Render: renderAdvice})
	// The LLM call is registered as a separate AsyncFragment, not part of
	// this page's own render — see AsyncFragment's own doc comment for why
	// (it replaces an earlier Module.Prepare hook that blocked the whole
	// page, heuristic half included, on the LLM call).
	RegisterAsync(AsyncFragment{Path: "/advice/commentary", Handler: renderAdviceCommentary})
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

// Peek returns the cached advice reply and error if present and not expired,
// without waiting or triggering generation. Used by renderAdvice to render
// cached commentary immediately on revisit or page reload.
func (c *prepareAdviceCache) Peek() (string, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expiry) && (c.reply != "" || c.err != nil) {
		return c.reply, c.err, true
	}
	return "", nil, false
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

// defaultAdviceCache is the one instance renderAdviceCommentary uses in
// the real binary — a single `vitals dashboard` process serves one
// machine, so a package-level cache is safe there. Tests that need a
// clean cache use withFreshAdviceCache (modules_test.go), the same
// swap-and-restore pattern withRegistry uses for the module registry.
var defaultAdviceCache = newPrepareAdviceCache()

type adviceDeps struct {
	scan     func(context.Context, string, diskusage.Options) (diskusage.Result, error)
	home     func() (string, error)
	generate func([]byte, llm.CompleteOptions) (string, error)
}

var defaultAdviceDeps = adviceDeps{
	scan:     diskusage.Scan,
	home:     os.UserHomeDir,
	generate: advice.Generate,
}

// generateAdvice does the actual LLM call — split out from
// renderAdviceCommentary so prepareAdviceCache.Get has something
// pure-ish (given a ctx) to memoize.
func generateAdvice(ctx *PageContext) (string, error) {
	envelope := doctor.JSONReport(ctx.Snapshot, ctx.Report)
	latest := defaultDiskUsageCache.Latest()
	if latest == nil {
		if home, err := defaultAdviceDeps.home(); err == nil && home != "" {
			scanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			opts := diskusage.Options{Budget: 2 * time.Second, Top: 10}
			res, err := defaultAdviceDeps.scan(scanCtx, home, opts)
			cancel()
			if err == nil || res.Entries > 0 {
				defaultDiskUsageCache.Put(home, opts, res)
				latest = &res
			}
		}
	}
	if latest != nil {
		envelope.DiskUsage = latest
	}
	reportJSON, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return defaultAdviceDeps.generate(reportJSON, ctx.LLMOpts)
}

// adviceErrorTmpl renders an LLM error's message — a string that can
// embed arbitrary provider/network detail, so it goes through
// html/template's auto-escaping like every other render function in this
// package, not a manual html.EscapeString call. It's shown alongside the
// heuristic findings, not instead of them: the LLM is a complement, so
// its being unreachable is a quiet note here, not a page-level failure.
var adviceErrorTmpl = template.Must(template.New("adviceError").Parse(
	`<div id="ai-commentary"><p class="unavailable">No LLM reachable for further AI commentary: {{.}}</p></div>`))

// adviceCommentaryScript fetches the AsyncFragment above and swaps it in
// once it resolves, after the page itself (which needs none of this) has
// already rendered — vanilla JS, no framework, the same "no client-side
// dependency" convention modules_clean.go's Preview/Apply buttons use.
// A fetch-level failure (the request never even reaching the server) gets
// its own message distinct from adviceErrorTmpl's "no LLM reachable" —
// that one means the server answered and found no provider; this one
// means the browser couldn't even ask.
const adviceCommentaryScript = `<script>
(function(){
  var ctrl = typeof AbortController !== 'undefined' ? new AbortController() : null;
  var timer = ctrl ? setTimeout(function(){ ctrl.abort(); }, 65000) : null;
  fetch('/advice/commentary', { signal: ctrl ? ctrl.signal : undefined }).then(function(r){
    if (timer) clearTimeout(timer);
    if (!r.ok) throw new Error('HTTP ' + r.status);
    return r.text();
  }).then(function(html){
    var target = document.getElementById('ai-commentary');
    if (target) target.outerHTML = html;
  }).catch(function(err){
    if (timer) clearTimeout(timer);
    var target = document.getElementById('ai-commentary');
    if (target) {
      var msg = (err && err.name === 'AbortError')
        ? 'AI commentary request timed out waiting for LLM response.'
        : 'Could not reach the dashboard for AI commentary.';
      target.innerHTML = '<p class="unavailable">' + msg + '</p>';
    }
  });
})();
</script>`

// renderAdvice shows this machine's rule-based findings — the same data
// and layout the overview page uses (reportHeadline/verdictBanner/
// findingsList), needing no LLM at all — immediately, unconditionally.
// The LLM commentary is a placeholder card filled in later by
// adviceCommentaryScript's fetch to the /advice/commentary AsyncFragment,
// so a slow or unreachable LLM (up to completeTimeout, 60s) never delays
// this page's own render — see AsyncFragment's doc comment for the bug
// this replaced (the whole page, heuristic half included, used to block
// on the LLM call).
func renderAdvice(ctx PageContext) string {
	headline := reportHeadline(ctx.Report, "Healthy — nothing needs attention")
	body := verdictBanner(headline, "What vitals doctor's own rule-based checks found", ctx.Report.Worst())
	body += findingsCard(ctx.Report.SortedBySeverity())
	if reply, err, ok := defaultAdviceCache.Peek(); ok {
		switch {
		case reply != "":
			body += `<div id="ai-commentary" class="card"><h3>AI commentary</h3>` + guide.RenderFragment(reply) + `</div>`
		case err != nil:
			body += mustExecute(adviceErrorTmpl, err.Error())
		default:
			body += `<div id="ai-commentary"></div>`
		}
		return body
	}
	body += `<div id="ai-commentary" class="card"><p class="unavailable">Asking the LLM for further commentary&hellip;</p></div>`
	body += adviceCommentaryScript
	return body
}

// renderAdviceCommentary is the /advice/commentary AsyncFragment's
// Handler: the actual LLM call (via the cache above), rendering just the
// replacement fragment for renderAdvice's placeholder div — not a full
// page.Layout, since the client-side fetch() only wants the fragment.
// Always 200: an unreachable LLM is a graceful-degradation message here
// (adviceErrorTmpl), not an HTTP error, the same posture prepareAdvice
// used to take by absorbing the error into ctx.AdviceErr rather than
// returning it.
func renderAdviceCommentary(ctx PageContext) (int, string) {
	reply, err := defaultAdviceCache.Get(&ctx)
	switch {
	case reply != "":
		return http.StatusOK, `<div id="ai-commentary" class="card"><h3>AI commentary</h3>` + guide.RenderFragment(reply) + `</div>`
	case err != nil:
		return http.StatusOK, mustExecute(adviceErrorTmpl, err.Error())
	default:
		return http.StatusOK, `<div id="ai-commentary"></div>`
	}
}
