package doctor

import (
	"sync"
	"testing"
	"time"
)

func TestWithDiskHistoryPersistsAcrossCalls(t *testing.T) {
	isolateConfigDir(t)

	withDiskHistory(func(hist map[string]diskHistoryEntry) {
		hist["/"] = diskHistoryEntry{FreeBytes: 1000, UnixTime: time.Now().Unix()}
	})

	var got diskHistoryEntry
	withDiskHistory(func(hist map[string]diskHistoryEntry) {
		got = hist["/"]
	})
	if got.FreeBytes != 1000 {
		t.Errorf("FreeBytes = %d, want 1000 (should have persisted from the first call)", got.FreeBytes)
	}
}

func TestWithDiskHistorySerializesConcurrentReadModifyWrite(t *testing.T) {
	// Two Collect() calls in the same process (e.g. the dashboard's cache
	// refresh racing a concurrent CLI invocation) must not interleave a
	// load-mutate-save cycle: without a lock, both can read the same
	// starting map, and whichever saves last silently drops the other's
	// update. This runs N concurrent increments and checks none were lost
	// — a real correctness assertion, not just an absence of a -race
	// report (which this also gets, run with `go test -race`).
	isolateConfigDir(t)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			withDiskHistory(func(hist map[string]diskHistoryEntry) {
				e := hist["/"]
				e.FreeBytes++ // read-modify-write: exactly the pattern a lost update would break
				hist["/"] = e
			})
		}(i)
	}
	wg.Wait()

	var got diskHistoryEntry
	withDiskHistory(func(hist map[string]diskHistoryEntry) {
		got = hist["/"]
	})
	if got.FreeBytes != n {
		t.Errorf("FreeBytes = %d after %d concurrent increments, want %d (a lost update means the lock isn't holding)", got.FreeBytes, n, n)
	}
}
