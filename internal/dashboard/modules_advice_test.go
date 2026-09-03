package dashboard

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepareAdviceCacheRefreshesOnceThenServesFromCacheWithinTTL(t *testing.T) {
	var calls int32
	c := &prepareAdviceCache{
		ttl: time.Hour, // long enough that this test can't flake on timing
		generate: func(*PageContext) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "advice", nil
		},
	}

	ctx := &PageContext{}
	c.Get(ctx)
	c.Get(ctx)
	c.Get(ctx)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("generate called %d times, want 1 (cached within TTL) — this is the bug: every request to /advice used to call the LLM fresh", got)
	}
}

func TestPrepareAdviceCacheRefreshesAgainAfterTTLExpires(t *testing.T) {
	var calls int32
	c := &prepareAdviceCache{
		ttl: 10 * time.Millisecond,
		generate: func(*PageContext) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "advice", nil
		},
	}

	ctx := &PageContext{}
	c.Get(ctx)
	time.Sleep(20 * time.Millisecond)
	c.Get(ctx)

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("generate called %d times, want 2 (once per TTL window)", got)
	}
}

func TestPrepareAdviceCacheSingleFlightsConcurrentRefreshes(t *testing.T) {
	// N concurrent requests to /advice hitting an expired/empty cache at
	// once must collapse into exactly one real LLM call — this is the
	// actual fix for the bug found by review: every request used to
	// trigger its own uncached completion.
	var calls int32
	release := make(chan struct{})
	c := &prepareAdviceCache{
		ttl: time.Hour,
		generate: func(*PageContext) (string, error) {
			atomic.AddInt32(&calls, 1)
			<-release // hold every concurrent caller here until we let go
			return "advice", nil
		},
	}

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c.Get(&PageContext{})
		}()
	}

	time.Sleep(20 * time.Millisecond) // let every goroutine reach the cache
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("generate called %d times for %d concurrent callers, want 1", got, n)
	}
}

func TestPrepareAdviceCacheReturnsTheCachedReplyAndError(t *testing.T) {
	c := &prepareAdviceCache{
		ttl:      time.Hour,
		generate: func(*PageContext) (string, error) { return "cached reply", nil },
	}
	reply, err := c.Get(&PageContext{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if reply != "cached reply" {
		t.Errorf("reply = %q, want %q", reply, "cached reply")
	}
}

func TestNewPrepareAdviceCacheIsConfiguredButNotYetTriggered(t *testing.T) {
	c := newPrepareAdviceCache()
	if c.ttl != prepareAdviceCacheTTL {
		t.Errorf("ttl = %v, want %v", c.ttl, prepareAdviceCacheTTL)
	}
	if c.generate == nil {
		t.Error("generate should be set to the live generateAdvice function")
	}
	if !c.expiry.IsZero() {
		t.Error("a freshly constructed cache should not appear already-warm")
	}
}
