package dupes

import (
	"context"
	"testing"
	"time"
)

func TestScanContextPreCancelledReturnsPromptlyAndTruncated(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", []byte("some content here"))
	write(t, root, "b.txt", []byte("some content here"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done before Scan even starts walking

	start := time.Now()
	r, err := ScanContext(ctx, root, 1, 0)
	if err != nil {
		t.Fatalf("ScanContext: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("a pre-cancelled context should return promptly, took %v", elapsed)
	}
	if !r.Truncated {
		t.Error("Result.Truncated should be true when the context was already cancelled")
	}
}

func TestScanContextFileBudgetTruncatesAtTheLimit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 6; i++ {
		write(t, root, string(rune('a'+i))+".txt", []byte("distinct content "+string(rune('a'+i))))
	}

	r, err := ScanContext(context.Background(), root, 1, 3)
	if err != nil {
		t.Fatalf("ScanContext: %v", err)
	}
	if !r.Truncated {
		t.Error("Result.Truncated should be true when the file budget is hit")
	}
	if r.ScannedFiles > 3 {
		t.Errorf("ScannedFiles = %d, want <= 3 (the budget)", r.ScannedFiles)
	}
	if r.ScannedFiles == 0 {
		t.Error("expected some files to be scanned before the budget stopped the walk")
	}
}

func TestScanContextZeroBudgetIsUnlimited(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		write(t, root, string(rune('a'+i))+".txt", []byte("distinct content "+string(rune('a'+i))))
	}

	r, err := ScanContext(context.Background(), root, 1, 0)
	if err != nil {
		t.Fatalf("ScanContext: %v", err)
	}
	if r.Truncated {
		t.Error("a zero budget should never truncate")
	}
	if r.ScannedFiles != 5 {
		t.Errorf("ScannedFiles = %d, want 5 (all of them)", r.ScannedFiles)
	}
}

func TestScanContextUnderBudgetIsNotTruncated(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", []byte("just one file"))

	r, err := ScanContext(context.Background(), root, 1, 10)
	if err != nil {
		t.Fatalf("ScanContext: %v", err)
	}
	if r.Truncated {
		t.Error("a scan that finishes well under its budget should not be marked Truncated")
	}
}

func TestScanFallsThroughToScanContextUnbounded(t *testing.T) {
	// Scan itself (the public, pre-existing entry point) must still behave
	// exactly as before: unbounded, never truncated, real-time context.
	root := t.TempDir()
	write(t, root, "a.txt", []byte("hello"))

	r, err := Scan(root, 1)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if r.Truncated {
		t.Error("Scan (unbounded) should never report Truncated")
	}
}
