package dashboard

import (
	"sync"
	"time"

	"vitals/internal/diag"
	"vitals/internal/doctor"
	"vitals/internal/llm"
)

// snapshotCacheTTL bounds how stale a served page's numbers can be. Short
// enough that nobody looking at the dashboard would notice, long enough
// that a burst of nav clicks or concurrent tabs shares one real
// Collect()/ProbeProviders() pass instead of paying for one each — the
// fix for the design review's finding that a single page load could cost
// up to tens of seconds in a realistic worst case (see
// docs/architecture/design.md §6.4).
const snapshotCacheTTL = 3 * time.Second

// cachedSnapshot is everything a request needs to build any page's
// PageContext: the live snapshot, its cross-resource analysis (for the
// overview page; resource pages re-run doctor.AnalyzeResource against the
// same Snapshot), and LLM provider reachability (for the advice module's
// nav-gating check).
type cachedSnapshot struct {
	Snapshot  doctor.Snapshot
	Report    diag.Report
	Providers []llm.Provider
}

// snapshotCache serves the most recent cachedSnapshot, refreshing it at
// most once per ttl and collapsing concurrent refreshes into a single
// underlying call — a hand-rolled single-flight rather than a new
// dependency, since the whole mechanism is ~20 lines. refresh is
// injectable so the TTL/single-flight logic itself (the part with real
// risk of a bug) is unit-tested without touching a live machine; only
// newSnapshotCache's caller wires it to the real, live Collect/
// ProbeProviders pair.
type snapshotCache struct {
	ttl     time.Duration
	refresh func() cachedSnapshot

	mu      sync.Mutex
	value   cachedSnapshot
	expiry  time.Time
	loading chan struct{} // non-nil while a refresh is in flight; closed when it completes
}

// newSnapshotCache builds a cache that refreshes from the real, live
// doctor.Collect + doctor.Analyze + llm.ProbeProviders, using ollamaURL
// for both (matching every other command's --ollama-url wiring).
func newSnapshotCache(ollamaURL string) *snapshotCache {
	return &snapshotCache{
		ttl: snapshotCacheTTL,
		refresh: func() cachedSnapshot {
			snap := doctor.Collect(doctor.Options{OllamaURL: ollamaURL})
			return cachedSnapshot{
				Snapshot:  snap,
				Report:    doctor.Analyze(snap),
				Providers: llm.ProbeProviders(llm.Options{OllamaURL: ollamaURL}),
			}
		},
	}
}

// Get returns the cached value, refreshing it first if it's missing or
// older than ttl. A caller that arrives while a refresh is already in
// flight waits on that same refresh rather than starting its own.
func (c *snapshotCache) Get() cachedSnapshot {
	c.mu.Lock()
	if time.Now().Before(c.expiry) {
		v := c.value
		c.mu.Unlock()
		return v
	}
	if c.loading != nil {
		ch := c.loading
		c.mu.Unlock()
		<-ch
		c.mu.Lock()
		v := c.value
		c.mu.Unlock()
		return v
	}
	ch := make(chan struct{})
	c.loading = ch
	c.mu.Unlock()

	v := c.refresh()

	c.mu.Lock()
	c.value = v
	c.expiry = time.Now().Add(c.ttl)
	c.loading = nil
	c.mu.Unlock()
	close(ch)

	return v
}
