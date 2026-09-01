package dupes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyHardlinksLinksAllButTheFirstAndPreservesContent(t *testing.T) {
	dir := t.TempDir()
	content := []byte("identical bytes in every copy")
	a := write(t, dir, "a.bin", content)
	b := write(t, dir, "b.bin", content)
	c := write(t, dir, "c.bin", content)

	groups := []Group{{SizeBytes: int64(len(content)), Hash: "x", Paths: []string{a, b, c}}}
	linked, reclaimed, failures := ApplyHardlinks(groups)

	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %v", failures)
	}
	if linked != 2 {
		t.Errorf("linked = %d, want 2 (b and c, a is kept as-is)", linked)
	}
	if want := int64(len(content)) * 2; reclaimed != want {
		t.Errorf("reclaimed = %d, want %d", reclaimed, want)
	}

	infoA, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{b, c} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !os.SameFile(infoA, info) {
			t.Errorf("%s is not hardlinked to %s (different inode)", p, a)
		}
		got, err := os.ReadFile(p)
		if err != nil || string(got) != string(content) {
			t.Errorf("%s content changed: got %q err %v", p, got, err)
		}
	}
}

func TestApplyHardlinksSkipsGroupsOfOne(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "solo.bin", []byte("only copy"))
	groups := []Group{{SizeBytes: 9, Paths: []string{a}}}

	linked, reclaimed, failures := ApplyHardlinks(groups)
	if linked != 0 || reclaimed != 0 || len(failures) != 0 {
		t.Errorf("a lone file should be a no-op, got linked=%d reclaimed=%d failures=%v", linked, reclaimed, failures)
	}
}

func TestApplyHardlinksReportsAMissingKeptFileAsAFailure(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone.bin")
	present := write(t, dir, "present.bin", []byte("x"))

	groups := []Group{{SizeBytes: 1, Paths: []string{missing, present}}}
	linked, _, failures := ApplyHardlinks(groups)

	if linked != 0 {
		t.Errorf("linked = %d, want 0 when the kept file doesn't exist", linked)
	}
	if len(failures) != 1 {
		t.Fatalf("expected exactly one reported failure, got %v", failures)
	}
}

func TestApplyHardlinksDoesNotTouchAlreadyLinkedPairs(t *testing.T) {
	// Applying twice should be safe: the second pass just re-links to the
	// same content, never loses data.
	dir := t.TempDir()
	content := []byte("re-apply me")
	a := write(t, dir, "a.bin", content)
	b := write(t, dir, "b.bin", content)
	groups := []Group{{SizeBytes: int64(len(content)), Paths: []string{a, b}}}

	ApplyHardlinks(groups)
	linked, _, failures := ApplyHardlinks(groups)
	if len(failures) != 0 {
		t.Errorf("re-applying to an already-linked pair should not fail: %v", failures)
	}
	if linked != 1 {
		t.Errorf("re-applying should still report the link, got %d", linked)
	}
}
