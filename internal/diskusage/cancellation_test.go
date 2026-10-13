package diskusage

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancellationBeforeIO(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		f := newFS(t)
		ctx, cancel := context.WithCancel(context.Background())
		reason := "canceled"
		if deadline {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			reason = "deadline_exceeded"
		} else {
			cancel()
		}
		defer cancel()
		r, err := scanWith(ctx, f.root, DefaultOptions(), f.deps())
		if err != nil || r.Complete || !slices.Contains(r.StopReasons, reason) || f.opened.Load() != 0 {
			t.Fatalf("cancellation still performed I/O: %+v, %v", r, err)
		}
	}
}

func TestInjectedBudget(t *testing.T) {
	f := newFS(t)
	f.add("file", 99, 0)
	start := time.Now()
	var elapsed atomic.Int64
	f.read = func(string, int) { elapsed.Store(int64(30 * time.Second)) }
	r := f.scan(t, func(d *dependencies) {
		d.now = func() time.Time { return start.Add(time.Duration(elapsed.Load())) }
	})
	if r.Complete || r.ElapsedMS != 30000 || r.ObservedBytes != 0 || r.Entries != 1 || !slices.Contains(r.StopReasons, "budget_exceeded") {
		t.Fatal(r)
	}
}

func TestCancellationBetweenOperations(t *testing.T) {
	for _, stage := range []string{"root-open", "root-stat", "mounts", "inspect", "child-open", "child-recheck", "child-stat"} {
		t.Run(stage, func(t *testing.T) {
			f := newFS(t)
			f.add("d", 0, fs.ModeDir)
			f.add("d/file", 9, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deps := f.deps()
			if stage == "root-open" {
				original := deps.openRoot
				deps.openRoot = func(ctx context.Context, path string) (directory, error) {
					dir, err := original(ctx, path)
					cancel()
					return dir, err
				}
			}
			if stage == "mounts" {
				deps.mounts = func(context.Context) ([]string, error) { cancel(); return nil, nil }
			}
			if stage == "child-open" {
				f.open = func(string) error { cancel(); return nil }
			}
			var rechecks atomic.Int64
			f.stat = func(path, name string) (fs.FileInfo, error) {
				switch stage {
				case "root-stat":
					if path == "." && name == "." {
						cancel()
					}
				case "inspect":
					if name == "d" {
						cancel()
					}
				case "child-recheck":
					if name == "d" && rechecks.Add(1) == 2 {
						cancel()
					}
				case "child-stat":
					if path == "d" && name == "." {
						cancel()
					}
				}
				return nil, nil
			}
			r, err := scanWith(ctx, f.root, DefaultOptions(), deps)
			f.assertClosed(t)
			if err != nil || r.Complete || r.ObservedBytes != 0 || !slices.Contains(r.StopReasons, "canceled") {
				t.Fatalf("cancellation ignored: %+v, %v", r, err)
			}
		})
	}
}

func receive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("worker failed to reach injected operation")
	}
}

func TestFixedWorkersBoundedQueueAndBlockedCancellation(t *testing.T) {
	f := newFS(t)
	for i := range 140 {
		name := string(rune('a' + i))
		f.add(name, 0, fs.ModeDir)
		f.add(filepath.Join(name, "file"), 2, 0)
	}
	started := make(chan struct{}, workerCount+1)
	queueFilled := make(chan struct{})
	release := make(chan struct{})
	var active, maximum, opens atomic.Int64
	var rootStates atomic.Int64
	f.stat = func(path, name string) (fs.FileInfo, error) {
		if path == "." && name != "." && rootStates.Add(1) == workerCount+queueSize+1 {
			close(queueFilled)
		}
		return nil, nil
	}
	f.open = func(string) error {
		opens.Add(1)
		n := active.Add(1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Result, 1)
	go func() {
		r, _ := scanWith(ctx, f.root, DefaultOptions(), f.deps())
		done <- r
	}()
	for range workerCount {
		receive(t, started)
	}
	receive(t, queueFilled)
	if maximum.Load() != 6 || opens.Load() != 6 {
		t.Fatalf("not exactly six workers: max %d opens %d", maximum.Load(), opens.Load())
	}
	// All workers are in an injected filesystem operation; cancellation may
	// stop the producer, but must not release scan admission before they return.
	cancel()
	select {
	case r := <-done:
		t.Fatalf("returned with blocked calls still alive: %+v", r)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case r := <-done:
		if r.Complete || r.ObservedBytes != 0 || r.Entries != workerCount+queueSize+2 || len(r.Directories) > workerCount+queueSize+1 || opens.Load() != 6 {
			t.Fatalf("queued work executed after cancellation: %+v; opens %d", r, opens.Load())
		}
		for _, row := range r.Directories {
			if row.Complete {
				t.Fatal("canceled directory marked complete")
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queue shutdown deadlocked")
	}
	f.assertClosed(t)
}

func TestFixedWorkersCompleteHighFanout(t *testing.T) {
	f := newFS(t)
	for i := range 700 {
		name := string(rune(0x100 + i))
		f.add(name, 0, fs.ModeDir)
		f.add(filepath.Join(name, "deep"), 0, fs.ModeDir)
		f.add(filepath.Join(name, "deep/file"), 1, 0)
	}
	r := f.scan(t, nil)
	if !r.Complete || r.ObservedBytes != 700 || r.Entries != 2101 || len(r.Directories) != 20 {
		t.Fatal(r)
	}
	for _, row := range r.Directories {
		if row.Bytes != 1 || !row.Complete {
			t.Fatal(row)
		}
	}

}

func TestCancellationKeepsCompletedChildExact(t *testing.T) {
	f := newFS(t)
	f.add("good", 0, fs.ModeDir)
	f.add("good/file", 3, 0)
	f.add("blocked", 0, fs.ModeDir)
	f.add("blocked/file", 900, 0)
	goodDone := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.onReaderClose = func(path string) {
		if path == "good" {
			close(goodDone)
		}
	}
	f.read = func(path string, _ int) {
		if path == "blocked" {
			<-goodDone
			cancel()
		}
	}
	r, err := scanWith(ctx, f.root, DefaultOptions(), f.deps())
	f.assertClosed(t)
	if err != nil || r.Complete || len(r.Directories) != 2 || r.ObservedBytes != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Directories[0] != (Row{filepath.Join(f.root, "good"), 3, true}) || r.Directories[1] != (Row{filepath.Join(f.root, "blocked"), 0, false}) {
		t.Fatalf("completed child lost its exact total: %+v", r.Directories)
	}
}
