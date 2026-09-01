---
description: Show what a disk cleanup would remove (dry run) — never deletes without confirmation.
---

Run `vitals clean --dry-run` and report:

1. The total that would be removed, and the per-directory breakdown.
2. Which package-manager prunes (`brew`, `docker`, `npm`, `pip`) would run.

Then ask me whether to proceed. Only if I explicitly confirm, run
`vitals clean --yes`. Never run it without that confirmation.

$ARGUMENTS
