# Shared read-only disk scanner

Integration entry points:

```go
diskusage.Scan(ctx, absoluteRoot, opts) (diskusage.Result, error)
diskusage.ScanRoot(ctx, authorizedRoot, displayRoot, opts) (diskusage.Result, error)
diskusage.DefaultOptions() diskusage.Options
diskusage.Validate(opts) error
diskusage.DisplayPath(rawPath) string
```

`Options` contains `Top` (1–100, default 20) and `Budget` (greater than
zero, at most five minutes, default 30 seconds). JSON encodes the Go
`time.Duration` budget as **nanoseconds**, not a duration string.

`Result`, `Row`, and `Issue` retain raw paths. Use `DisplayPath` for visible
terminal/HTML labels, `html/template` for HTML, and `encoding/json` for JSON;
never use display strings for filesystem access, URLs, or shell commands.
`DisplayPath` visibly escapes C0/C1, DEL, Unicode format/bidi controls and
invalid UTF-8 bytes.

## Scope and completion

- All rows use absolute display paths. `Directories` ranks immediate
  children by recursively observed regular-file logical bytes. `Files`
  ranks regular files anywhere in scope; `RootFiles` separately ranks files
  directly under the root. Each list is independently limited to `Top`.
  Larger byte totals rank first, with lexical ascending path ties.
- `ObservedBytes` is the total across all observed eligible files, not the
  sum of truncated rankings. Hardlinks count once **per path**; sparse files
  count their logical size. This is neither allocated space nor guaranteed
  reclaim, and it cannot account for snapshots/clones/compression.
- The root counts as one visited entry. Symlinks, special files, and detected
  nested mounts are excluded and counted, not traversed. Intentional
  exclusions do not by themselves make the eligible scan incomplete.
- `Complete` means all eligible entries were traversed without errors or
  limits; it is not a filesystem snapshot or a claim of complete disk
  accounting. Directory rows retain their own completion: already-finished
  children remain exact even when another child fails or cancellation occurs.
  Unfinished children report observed lower bounds, including zero.
  File rows are complete individual metadata observations even in a partial
  scan; partial rankings are **largest observed**, not proven largest.
- Stop reasons are sorted machine-readable strings: `invalid_options`,
  `root_error`, `access_error`, `canceled`, `deadline_exceeded`,
  `budget_exceeded`, `entry_limit`, `root_state_limit`, `depth_limit`,
  and `byte_overflow`. Errors are counted fully, with at most 20 lexical
  path/message-ordered samples. An overflowing total saturates at
  `math.MaxInt64` and is explicitly incomplete rather than wrapping.
- Invalid options and root open/stat/enumeration failures return a non-nil
  error and an incomplete result. Child errors and cooperative limits return
  an incomplete result with nil error: consumers must check `Complete`.
  Arrays remain non-nil even for empty/error results.

## Containment and mount caveats

`Scan` opens an absolute path component by component from its filesystem
anchor using `os.Root`. It rejects symlink roots and ancestors, compares
each opened directory's identity with its prior metadata, and rechecks the
parent entry after opening to reject replacement symlinks. Descendant walks
use scoped rooted handles and the same identity checks. No file contents
are read; no subprocesses or mutations are performed.

This is intentionally strict: macOS `/var` is commonly an alias for
`/private/var`, so pass the actual real-directory path (or deliberately
canonicalize an explicitly caller-authorized root **before** scanning).
The scanner does not silently canonicalize symlink ancestors.

`ScanRoot` accepts an already-authorized `*os.Root` and never reopens
`displayRoot` or closes the caller's handle. The caller must keep that handle
alive until the call returns. The absolute display label should describe
that handle's original selected location and is used to match known mount
paths. This is the preferred entry point for a dashboard that already
validated a home-confined root. Authorization belongs to the caller:
`ScanRoot` cannot establish how the handle was originally opened.

The live adapter calls the existing gopsutil mount inventory through an
injectable seam; the traversal core itself uses only standard-library
filesystem operations. Known nested mount paths are skipped, including
same-device mounts. Linux/macOS also compare native device identity.
`BoundaryDetectionAvailable` is false when inventory fails, native root
identity is unsupported (including Windows), or encountered eligible
metadata lacks native identity. Known mount paths are still excluded in
that case, but **do not describe these results as filesystem-exclusive**.
Mount inventories are observations, not protection against mounts changing
concurrently. Device identity alone cannot detect same-device bind mounts.

## Bounded work and cancellation

Six fixed workers consume a 64-task channel. Only immediate root directories
are queued; workers traverse children depth-first, retaining at most 128
directory levels. Readers request batches of 256 entries. The scan admits
at most 1,000,000 entries including the root and retains at most 10,000
immediate-child aggregate states; file rankings retain only top-N state.

The budget includes scanner setup/mount discovery, not any preceding doctor
collection. Cancellation/deadlines are cooperative between filesystem
operations, including root opening. They do **not** interrupt blocked
filesystem syscalls or the mount inventory's underlying implementation.
Queued work is drained without executing it after a stop. The function
waits for in-flight work and closes owned handles before returning.
Consumers must retain any global scan admission slot until actual return,
not release it merely because a request or deadline expired.

Tests exercise exact deterministic fixtures, read-only content retention,
root/symlink replacement failures, mount scope, byte overflow, production
entry/state/depth caps, fixed-worker cancellation/shutdown, raw JSON paths,
and borrowed-root rename/ownership. Linux/Windows builds can be checked
with cross-compilation; a build is not live-platform mount validation.
