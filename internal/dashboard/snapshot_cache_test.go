package dashboard

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vitals/internal/llm"
)

func TestSnapshotCacheRefreshesOnceThenServesFromCacheWithinTTL(t *testing.T) {
	var calls int32
	c := &snapshotCache{
		ttl: time.Hour, // long enough that this test can't flake on timing
		refresh: func() cachedSnapshot {
			atomic.AddInt32(&calls, 1)
			return cachedSnapshot{}
		},
	}

	c.Get()
	c.Get()
	c.Get()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("refresh called %d times, want 1 (cached within TTL)", got)
	}
}

func TestSnapshotCacheRefreshesAgainAfterTTLExpires(t *testing.T) {
	var calls int32
	c := &snapshotCache{
		ttl: 10 * time.Millisecond,
		refresh: func() cachedSnapshot {
			atomic.AddInt32(&calls, 1)
			return cachedSnapshot{}
		},
	}

	c.Get()
	time.Sleep(20 * time.Millisecond)
	c.Get()

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("refresh called %d times, want 2 (once per TTL window)", got)
	}
}

func TestSnapshotCacheSingleFlightsConcurrentRefreshes(t *testing.T) {
	// N concurrent callers hitting an expired/empty cache at once must
	// collapse into exactly one real refresh — this is the fix for the
	// design review's finding that concurrent dashboard requests could
	// each trigger their own Collect()/probeProviders() call.
	var calls int32
	release := make(chan struct{})
	c := &snapshotCache{
		ttl: time.Hour,
		refresh: func() cachedSnapshot {
			atomic.AddInt32(&calls, 1)
			<-release // hold every concurrent caller here until we let go
			return cachedSnapshot{}
		},
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c.Get()
		}()
	}

	time.Sleep(20 * time.Millisecond) // let every goroutine reach the cache
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("refresh called %d times for %d concurrent callers, want 1", got, n)
	}
}

func TestNewSnapshotCacheIsConfiguredButNotYetTriggered(t *testing.T) {
	// Constructing the cache must never itself call the live
	// Collect/ProbeProviders pair — only Get() does, on demand — so this
	// test can assert on the wiring without touching a real machine.
	c := newSnapshotCache("http://localhost:11434")
	if c.ttl != snapshotCacheTTL {
		t.Errorf("ttl = %v, want %v", c.ttl, snapshotCacheTTL)
	}
	if c.refresh == nil {
		t.Error("refresh should be set to the live Collect/ProbeProviders pair")
	}
	if !c.expiry.IsZero() {
		t.Error("a freshly constructed cache should not appear already-warm")
	}
}

func TestSnapshotCacheReturnsTheRefreshedValue(t *testing.T) {
	c := &snapshotCache{
		ttl: time.Hour,
		refresh: func() cachedSnapshot {
			return cachedSnapshot{Providers: []llm.Provider{{Name: "test-provider", Reachable: true}}}
		},
	}
	got := c.Get()
	if len(got.Providers) != 1 || got.Providers[0].Name != "test-provider" {
		t.Errorf("Get() = %+v, want the refreshed value with Providers[0].Name == \"test-provider\"", got)
	}
}
