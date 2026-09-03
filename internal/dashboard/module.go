// Package dashboard serves vitals as a local web app: the same binary, no
// new dependency, reachable from a browser instead of a terminal. It is
// built as a plugin registry on purpose — every page (overview, one
// resource, advice) is a self-contained Module that declares whether this
// machine can actually offer it (a battery page on a desktop, an advice
// page with no LLM reachable, a GPU page with no GPU) and how to render it
// when it can. Adding a page means adding a file that calls Register in its
// own init(); nothing else in the package has to change.
package dashboard

import (
	"fmt"
	"sort"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/llm"
)

// PageContext is everything a Module's Available/Render functions can see.
// It's built fresh per request from a live Collect(), so every page
// reflects the current machine rather than a cached snapshot. AdviceReply/
// AdviceErr are the one exception: they're only populated by the HTTP
// handler when the advice module itself is being rendered, since asking an
// LLM is too slow to do on every request just in case a page needs it.
type PageContext struct {
	Snapshot    doctor.Snapshot
	Report      diag.Report
	Providers   []llm.Provider
	LLMOpts     llm.CompleteOptions
	AdviceReply string
	AdviceErr   error
	// Version is main.version, threaded through so the footer can show it
	// without this package importing package main.
	Version string
}

// Module is one self-contained dashboard page.
type Module struct {
	Slug     string // URL path segment; "" is the root/overview page
	NavLabel string
	Order    int // nav position, lowest first; ties keep registration order
	// UnavailableReason is shown (via unavailablePage) when Available
	// returns false — a short, specific reason ("no GPU detected"), not a
	// restatement of "isn't available" itself, which the router already
	// says. Empty falls back to a generic reason.
	UnavailableReason string
	// Prepare does whatever request-scoped, module-specific work Render
	// needs but PageContext doesn't carry by default (the advice module
	// uses this to call the LLM only when its own route is hit, not on
	// every request). Called once, uniformly, by the router for whichever
	// module matched — nil means there's nothing to do, not an error.
	// This exists specifically so a module needing extra setup never
	// requires the router to special-case its slug: see
	// docs/architecture/design.md §6.2.
	Prepare   func(*PageContext) error
	Available func(PageContext) bool
	Render    func(PageContext) string
}

var registry []Module

// Register adds a module to the dashboard. Call it from the registering
// module's own init() — see modules_overview.go for the pattern. Panics
// on a duplicate Slug, matching http.ServeMux.Handle's own behavior for a
// duplicate pattern: a silently-shadowed second module would be
// permanently unreachable dead code with no error anywhere else to catch
// it.
func Register(m Module) {
	for _, existing := range registry {
		if existing.Slug == m.Slug {
			panic(fmt.Sprintf("dashboard: duplicate module slug %q", m.Slug))
		}
	}
	registry = append(registry, m)
}

// sortedModules returns every registered module ordered for display.
// sort.SliceStable so two modules registered with the same Order keep
// whatever order they were registered in, rather than a coin flip.
func sortedModules() []Module {
	out := append([]Module(nil), registry...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out
}

// availableModules filters sortedModules to what this PageContext can
// actually offer — the plugin system's whole point: the nav (and routing)
// only ever shows what this machine can do.
func availableModules(ctx PageContext) []Module {
	var out []Module
	for _, m := range sortedModules() {
		if m.Available(ctx) {
			out = append(out, m)
		}
	}
	return out
}

// findModule returns the registered module for slug and whether it's
// available on ctx — a module that exists but isn't available on this
// machine is reported separately so the handler can explain why, instead
// of a bare 404.
func findModule(slug string, ctx PageContext) (Module, bool, bool) {
	for _, m := range registry {
		if m.Slug == slug {
			return m, true, m.Available(ctx)
		}
	}
	return Module{}, false, false
}

// Always is the Available check for a module every machine can offer.
func Always(PageContext) bool { return true }

// HasGPU is the Available check for GPU-backed modules.
func HasGPU(ctx PageContext) bool { return len(ctx.Snapshot.GPUs) > 0 }

// HasBattery is the Available check for the power module — a desktop or a
// cloud VM has nothing meaningful to show here.
func HasBattery(ctx PageContext) bool {
	return ctx.Snapshot.Power.Percent > 0 || ctx.Snapshot.Power.OnBattery
}

// WriteAction is one POST-only, mutating dashboard endpoint (roadmap item
// 005) — a deliberately different shape from Module, not an optional
// field on it: a write action's Handler produces a status+body for a
// client-side script to consume, not a full navigable page wrapped in
// layout(), so folding it into Module would give every purely-read-only
// page (still all of them but this one) a field it never uses. See
// docs/roadmap/items/005-dashboard-write-actions/design.md. Same-origin
// protection is not this type's concern at all — guide.ServeLocal already
// rejects any non-GET/HEAD request that fails the Origin/Sec-Fetch-Site
// check before it reaches here, for every route, uniformly.
type WriteAction struct {
	Path string // URL path, always POST-only, e.g. "/clean/preview"
	// Available mirrors Module.Available. nil means always available,
	// matching every write action shipping today. Present from day one,
	// per review, even though nothing needs it yet — so a future write
	// action gated by machine capability doesn't require reworking this
	// registry after real callers already depend on its absence.
	Available func(PageContext) bool
	// Handler receives the request body as raw, already-read bytes —
	// never a live *http.Request — so it's constructed and called
	// directly in tests the same way Module.Render is, no
	// httptest.NewRequest boilerplate needed to exercise it.
	Handler func(PageContext, []byte) (status int, body string)
}

var writeActions []WriteAction

// RegisterWrite adds a write action to the dashboard, mirroring Register
// — same duplicate-path panic, for the same reason (a silently-shadowed
// second registration would be permanently unreachable dead code).
func RegisterWrite(a WriteAction) {
	for _, existing := range writeActions {
		if existing.Path == a.Path {
			panic(fmt.Sprintf("dashboard: duplicate write action path %q", a.Path))
		}
	}
	writeActions = append(writeActions, a)
}

// findWriteAction returns the registered write action for path and
// whether it's available on ctx, mirroring findModule.
func findWriteAction(path string, ctx PageContext) (action WriteAction, exists, available bool) {
	for _, a := range writeActions {
		if a.Path == path {
			ok := true
			if a.Available != nil {
				ok = a.Available(ctx)
			}
			return a, true, ok
		}
	}
	return WriteAction{}, false, false
}
