package dupes

import (
	"fmt"
	"os"
)

// ApplyHardlinks replaces every file after the first in each group with a
// hardlink to the first, reclaiming the duplicate's space. Unlike deletion,
// this destroys no data even if it's wrong: every path keeps working and
// keeps reading the same bytes — the only change is that they now share one
// inode instead of two. Only called when the user explicitly opts in
// (--hardlink); the default `vitals dupes` run never modifies anything.
func ApplyHardlinks(groups []Group) (linked int, bytesReclaimed int64, failures []string) {
	for _, g := range groups {
		if len(g.Paths) < 2 {
			continue
		}
		keep := g.Paths[0]
		for _, p := range g.Paths[1:] {
			if err := linkOver(keep, p); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", p, err))
				continue
			}
			linked++
			bytesReclaimed += g.SizeBytes
		}
	}
	return
}

// linkOver makes p a hardlink to keep, replacing p's current content. os.Link
// itself refuses to overwrite an existing destination, so this links to a
// temp name first and renames it over p — the rename is atomic, so a crash
// mid-way leaves either the original p or a stray temp file, never a
// half-written p.
func linkOver(keep, p string) error {
	if fi, err := os.Stat(keep); err != nil {
		return err
	} else if pi, err := os.Stat(p); err == nil && os.SameFile(fi, pi) {
		return nil // already hardlinked together — nothing to do
	}
	tmp := p + ".vitals-tmp"
	_ = os.Remove(tmp) // clear a leftover from a previous failed attempt
	if err := os.Link(keep, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
