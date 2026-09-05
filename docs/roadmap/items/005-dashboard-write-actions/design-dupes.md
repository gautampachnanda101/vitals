# Design note — `dupes`/`--hardlink` as a dashboard write action

[docs](../../../index.md) / [Roadmap](../../index.md) / [005 — Dashboard write actions](index.md) / **Design note: dupes hardlink**

**Status: reviewed (2026-09-05), go-with-changes — see §8.** It reuses the
`sameOriginOnly` + `WriteAction` foundation from [`design.md`](design.md)
unchanged; only the route-specific decisions are here.

## 1. Why this is not just "another `/clean/apply`"

`/clean/apply` takes **no path input at all** — `home` comes from
`os.UserHomeDir()` server-side, so the request body carries nothing an
attacker could aim (`design.md` §7, finding 3). `vitals dupes` is
different: `Scan(root string, minSize int64)` is parameterized by a
directory to walk. Exposing it naively would let the request body name
an arbitrary filesystem path — a traversal / information-disclosure
surface the `clean` routes simply don't have. And `Scan` walks an
entire tree hashing file prefixes and full contents: seconds to minutes
on a large root, versus `clean.ReclaimableSummary`'s bounded stat of a
known cache-dir list. So this route has two risks `clean` doesn't:
**client-chosen paths** and **unbounded work**.

Blast radius in the other direction is *lower*: `ApplyHardlinks` /
`linkOver` (`internal/dupes/hardlink.go`) destroy no data even when
wrong — every path keeps working and keeps reading the same bytes, they
just share an inode. A mistaken hardlink is recoverable by copying the
file back to a private copy; a mistaken `clean` delete is not. So the
confirmation bar is about **DoS and path safety**, not data loss.

## 2. Scope selection: a fixed server-side enum, never a raw path

The request never carries a directory string. Instead the dashboard
offers a small fixed set of scopes, and the client picks one by key:

| Scope key | Root walked | minSize |
|---|---|---|
| `home` | `os.UserHomeDir()` | 1 MiB |
| `downloads` | `<home>/Downloads` | 1 MiB |
| `caches` (macOS/Linux) | `<home>/Library/Caches` / `<home>/.cache` | 1 MiB |

Unknown key → `400` before any walk. This mirrors how
`internal/memhogs` ships a fixed embedded family list rather than
accepting arbitrary regexes from outside the process, and how
`/clean/apply` derives its target server-side. Adding a "pick any
folder" capability later is a separate, separately-reviewed change (it
would need a directory picker constrained to the user's own home, plus
a symlink-escape check) — explicitly out of scope for v1.

## 3. The two routes

Same preview→apply shape as `clean`, both registered as `WriteAction`s
in a new `internal/dashboard/modules_dupes.go`, both behind
`sameOriginOnly`:

- **`POST /dupes/preview`** — body `{"scope": "<key>"}`. Runs `Scan` on
  the resolved root, renders the duplicate groups (path count, wasted
  bytes per group, total reclaimable) via `html/template`. No mutation.
  A `<script>`-bearing filename in a scanned path must render escaped —
  same crafted-path regression test as `renderCleanPreview`.
- **`POST /dupes/hardlink`** — body must be exactly
  `{"confirm": true, "scope": "<key>"}`. Anything else (missing
  `confirm`, `false`, unknown scope, malformed JSON, body over
  `maxWriteActionBody`) → `400` before `Scan` runs. On confirm: **re-run
  `Scan` server-side** and call `ApplyHardlinks` on *those* freshly
  computed groups — never on a group/path list sent by the client (that
  would reintroduce the arbitrary-path surface §2 removes). Response
  mirrors `renderCleanApplyResult`: linked count, bytes reclaimed, and
  each entry in `failures` as an escaped row.

"Confirm" here means the same thing it means for `/clean/apply`: a
footgun guard against a client-side bug wiring a button to the wrong
handler, not a security control (`sameOriginOnly` is the boundary). The
`scope` field is echoed in both the preview response and the apply body
so the "Apply" button's request is built from what the user actually
previewed, the same client-side pattern `modules_clean.go` already uses.

## 4. Bounding the work (the new requirement)

`Scan` today has no `context.Context` and no walk cap. Before this route
ships, `Scan` gains:

- a `context.Context` parameter, checked in the `filepath.WalkDir`
  callback, so the handler can cancel it;
- a caller-supplied max-file-count (or max-duration) budget; on hitting
  it, `Scan` stops walking and returns what it has plus a
  `Truncated bool` on `Result`, which the preview/apply response
  surfaces ("scan stopped at 200k files — results are partial").

The handler wraps each call in `context.WithTimeout` (30s for preview,
longer but still bounded for apply) so a pathological tree can't wedge
the request goroutine — and, via the single-flight mutex below, every
future call — indefinitely. This is the same class of concern
`design.md` §7 finding 2 recorded as *accepted* for `clean`'s
untimed `optional()` subprocesses; here it is designed out from the
start because the walk is in-process and cancellable, not a subprocess.

## 5. Single-flight guard

`var dupesApplyMu sync.Mutex` + `TryLock`, identical to
`cleanApplyMu` in `modules_clean.go` — a concurrent second
`/dupes/hardlink` (double-click, two tabs) gets `409 Conflict`.
`/dupes/preview` is **not** guarded (a read-only scan; two concurrent
previews just do redundant work and each returns), matching how
`/clean/preview` is unguarded while `/clean/apply` is not.

## 6. Testing

- `resolveScope(key, home, goos) (root string, minSize int64, ok bool)`
  — pure, table-driven: every known key on each OS, unknown key returns
  `ok == false`.
- Confirm-body validation: missing/`false`/unknown-scope/malformed →
  `400`, guard never contended (assert `dupesApplyMu` is still
  acquirable afterward), mirroring
  `TestHandleCleanApplyRejectsAConcurrentSecondCall`'s companions.
- `Scan` context cancellation + truncation: a fake tree, a
  pre-cancelled context returns promptly; a 3-file budget returns
  `Truncated` with ≤3 files considered.
- `renderDupesPreview` / `renderDupesHardlinkResult` crafted-path
  escaping tests.
- An httptest end-to-end: same-origin `POST /dupes/preview` on a temp
  tree with two known-identical files reports one group; a cross-origin
  `POST /dupes/hardlink` gets `403`; a same-origin confirmed apply on
  that temp tree links the second file to the first (`os.SameFile`
  true afterward) and reports the right reclaimed byte count.
- `dashboard_smoke_test.go`: `POST /dupes/preview` against the running
  binary on a temp tree, asserting the tree is unmodified afterward.
  The real hardlink apply is **not** smoke-tested against the running
  binary — same reasoning as `clean` apply's exclusion.

## 7. Open questions for the security pass

1. Is the fixed-scope-enum (§2) the right v1, or is even `home` too
   broad a walk to expose from a loopback web button without a
   per-directory opt-in?
2. `Scan` re-run on apply (§3) means preview and apply can see different
   trees. `linkOver` is safe by construction regardless — is "the user
   might hardlink a pair they didn't see in preview" an acceptable
   outcome given no data is lost, or does apply need to carry a digest
   of the previewed groups and refuse if the re-scan diverges?
3. Symlinks inside a scoped root: `filepath.WalkDir` does not follow
   them, but a symlinked *file* could still be reported as a duplicate
   and then `linkOver`'d. Should `Scan` skip symlinks entirely for this
   route?
4. Is a 30s preview timeout a bad user experience (a big `home` might
   legitimately need longer) versus a DoS ceiling — what's the right
   number, and should it be configurable via `config.toml`?

## 8. Security review outcome (2026-09-05)

Security-architect-persona review of §§1–7, per this item's exit
criteria and matching the rigor of `design.md` §7's own review. Verdict:
**go-with-changes** — nothing blocking, all four open questions closed
against the actual `internal/dupes` implementation (read in full before
writing this, not assumed from the design note's own prose).

1. **(Closed) `home` stays in the v1 scope enum.** Blast radius is
   already low by construction (`ApplyHardlinks`/`linkOver`,
   `internal/dupes/hardlink.go`: no data is ever deleted, only two paths
   made to share an inode), and `vitals clean` already reaches
   home-wide via its own fixed cache-dir list — this isn't a new class
   of exposure, just a differently-shaped walk. Contingent on the §4
   budget actually landing as real, tested code before this ships (not
   merely documented) — see the implementation task list below.
2. **(Closed) Preview/apply re-scan divergence is safe as designed, no
   digest needed.** Verified directly in `internal/dupes/dupes.go`:
   `confirmDuplicates` groups by a 64 KiB prefix hash *then* a full
   `sha256` of the entire file (`hashFile`) before two paths are ever
   considered the same group — this is full-content verification, not
   size/prefix-only. A re-scan between preview and apply can only ever
   change *which* byte-identical pairs are found, never cause a
   non-identical pair to be linked. The worst realistic outcome is "the
   apply result differs from what was previewed," already accepted in
   §6's own reasoning, not a safety gap.
3. **(Closed, and already true today — no code change needed) Symlinks
   cannot reach this route at all.** Verified directly: `Scan`'s
   `WalkDir` callback checks `d.Type()&fs.ModeSymlink != 0` and returns
   before any further processing, for *every* entry — files and
   symlinked directories alike (`fs.WalkDir` never dereferences a
   symlink to descend into it; a symlinked directory entry carries
   `ModeSymlink`, not `IsDir()`, so it hits the same skip). Neither a
   symlinked file nor anything reachable only through a symlinked
   directory can ever become a scan candidate, in any scope, today. The
   design note's own uncertainty here (§7 Q3) was written without
   re-checking the current source; correcting the record rather than
   adding a redundant second skip.
4. **(Closed) 30s wall-clock ceiling *and* a file-count budget, whichever
   hits first; not configurable in v1.** A pure wall-clock bound is the
   wrong instrument alone — a slow network-mounted home could hit 30s on
   a modest file count, while a fast local SSD could walk hundreds of
   thousands of files in the same window — so §4's own "max-file-count
   (or max-duration)" becomes both, matching how `internal/doctor`'s
   `diskUsageTimeout` and `internal/llm`'s `completeTimeout` are both
   fixed constants with no config knob, not a special case invented for
   this route. Revisit only if real usage shows 30s/the file-count cap
   genuinely too tight — not a blocking concern for v1.

**Required before shipping** (the actual net-new implementation work,
not yet done as of this review): `Scan` gains a `context.Context`
parameter and a file-count budget per §4, surfaced as `Truncated bool`
on `Result`; the two `WriteAction` routes, `resolveScope`, the
single-flight guard, and the render/escaping tests per §§2–6.
