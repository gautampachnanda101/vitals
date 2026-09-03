# Design — 005 Dashboard write actions

[docs](../../../index.md) / [Roadmap](../../index.md) / [005 — Dashboard write actions](index.md) / **Design**

**Status: reviewed (seven-persona panel, all seven go-with-changes) and
implemented, with deviations from the draft below — see "As built"
at the end of this document.** This was the design
`docs/architecture/design.md` §6.5 requires before any write/mutating
dashboard endpoint ships: "no write/mutating endpoint ships without a
same-origin check (Origin/Sec-Fetch-Site header validation) in addition
to the Host-header fix. This is now a named gate on that future phase,
not an open question." This document is that gate being closed —
reviewed on its own, not improvised alongside the first button, per
item 004's own "Why" section. The sections below are kept as originally
drafted (the pre-review proposal), not edited in place, so the record of
what the panel actually reviewed stays intact; "As built" is where this
document says what's different in `main` today.

## 1. Threat model (inherited, not re-litigated)

Same as every other security discussion in this repo: vitals runs at the
invoking user's own privilege level, not a network-facing daemon. The
dashboard already binds loopback-only and validates the `Host` header
(`internal/guide/serve.go`'s `allowedHostsOnly`, closing the DNS-rebinding
hole found in §6.5). The risk this document specifically closes: **any
other browser tab or extension already open on the machine** can
blind-POST to a loopback server whose origin it can reach, even without
DNS rebinding — for a read-only GET this is a confidentiality risk
already mitigated by cross-origin `fetch()` response-reading restrictions
(CORS), but a state-changing POST doesn't need to *read* the response to
cause damage, only to be *sent*. That's the CSRF-shaped gap this design
closes.

Explicitly out of scope (same as always): another process on the same
machine, running as the same user, deliberately calling these endpoints
(e.g. via `curl`) — that process already has the same privilege the user
themselves has, calling `vitals clean` directly. This design defends
against a **remote web page** turning the user's own browser into an
attacker, not against the user's own machine.

## 2. The same-origin check

A new middleware, `sameOriginOnly`, alongside `allowedHostsOnly` in
`internal/guide/serve.go` (shared plumbing, same as the Host check —
`vitals dashboard` is the only caller today, but this belongs where
`allowedHostsOnly` already lives, not duplicated):

```go
// sameOriginOnly rejects any request that looks like it did NOT
// originate from the page this server itself served — before it reaches
// a handler that mutates anything. Two independent, standard signals:
//
//   - Origin header: if present, must exactly match this server's own
//     origin (http://127.0.0.1:<port>). A cross-origin fetch() always
//     sets this; same-origin requests may omit it (e.g. simple form
//     posts from very old browsers), which is why its absence alone
//     isn't rejected — Sec-Fetch-Site is the fallback signal for that.
//   - Sec-Fetch-Site header: if present (virtually all current browsers
//     send it), must be "same-origin" or "none". This is the modern,
//     spec'd replacement for CSRF tokens for exactly this threat model
//     (see the Fetch Metadata Request Headers spec) — no server-side
//     session/token state needed, which matters here since the
//     dashboard has no auth/session/cookie mechanism at all today.
//
// A request with NEITHER header (a non-browser same-machine caller,
// e.g. curl) is allowed through — that caller is already the same
// privilege tier as the user running `vitals clean` directly; this
// middleware defends against a remote page weaponizing the user's own
// browser, not against the user's own machine.
func sameOriginOnly(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && o != origin {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" && s != "none" {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

Applied only to mutating routes (anything registered as a `WriteAction`,
§3) — GET routes stay as they are today, already covered by
`allowedHostsOnly` alone. `origin` is computed once in `dashboard.Serve`
from the same bound address `ServeLocal` already resolves
(`"http://" + ln.Addr().String()`), threaded down the same way
`guide.ServeLocal` already receives the listener's own address.

## 3. Extending the Module architecture: `WriteAction`

Read pages are `Module{Render func(PageContext) string, ...}`. Write
actions are a parallel, explicit concept — not overloading `Module`
itself, so a page that has no mutating action (every page today) doesn't
gain an unused field, and so routing can apply `sameOriginOnly` to
exactly the write-action routes without guessing from a Module's shape:

```go
// WriteAction is one POST-only, mutating dashboard endpoint. Registered
// separately from the read-only Module registry (module.go) — a
// deliberately different shape, not an optional field on Module, so
// routing knows statically which paths need sameOriginOnly without
// inspecting a Module's fields to guess.
type WriteAction struct {
	Path    string // URL path, e.g. "/clean/preview" — always POST-only
	Handler func(PageContext, *http.Request) (status int, body string)
}

var writeActions []WriteAction

func RegisterWriteAction(a WriteAction) {
	for _, existing := range writeActions {
		if existing.Path == a.Path {
			panic(fmt.Sprintf("dashboard: duplicate write action path %q", a.Path))
		}
	}
	writeActions = append(writeActions, a)
}
```

`route` (`dashboard.go`) gains a parallel `routeWrite(path string, r
*http.Request, ctx PageContext) (int, string)` — same pure, testable
shape as `route` itself, checked against `writeActions` the same way
`route` checks `findModule` against the read registry. `Serve`'s
`http.Handler` dispatches POST requests through `sameOriginOnly` wrapping
`routeWrite`, GET requests through the existing unwrapped `route`.

## 4. First concrete write actions: `clean` preview + apply

Matches item 005's own scope ("starting with a clean `--dry-run`
preview, later an actual apply/confirm flow") and reuses
`internal/clean`'s existing `Options{DryRun, Assume}` — no new cleanup
logic, only a new caller of what already exists and is already tested.

- **`POST /clean/preview`**: calls `clean.Run(clean.Options{DryRun:
  true})`(-equivalent — `clean.Run` prints today; this needs a
  variant/refactor returning a value instead, likely extracting the same
  way `doctor.Assess`/`Analyze` already separate live collection from
  pure reporting). Never mutates the filesystem. Returns what *would* be
  freed. Routed through `sameOriginOnly` uniformly with `/clean/apply`
  even though it doesn't itself mutate — one rule for every write-action
  route is a simpler, safer default than special-casing "this POST
  happens not to mutate today."
- **`POST /clean/apply`**: calls the real `clean.Run(clean.Options{Assume:
  true})` — actually deletes. Requires a JSON body `{"confirm": true}`;
  any other body (missing, `false`, malformed) is rejected with 400
  before `clean.Run` is ever called. This matches the bar the CLI itself
  already sets (`--yes`/`-y` is required for the same reason) rather than
  inventing a stricter web-only requirement — the same-origin check
  above is what actually defends against an unintended trigger; the
  `confirm` field defends against a *client-side bug* (a button wired to
  the wrong handler) more than an attacker, same as the CLI flag does.

Client-side UX: the dashboard's clean page renders a "Preview" button
first; the "Apply" button only appears once a preview response has
rendered, and its own POST body is built from that response — not
enforced server-side beyond the `confirm` field above, since the
same-origin check is the actual security boundary here, not a
multi-step handshake.

## 5. Testing

- `sameOriginOnly`: table-driven tests mirroring `allowedHostsOnly`'s
  existing ones in `internal/guide/serve_test.go` (matching, mismatched,
  and absent `Origin`; matching, mismatched, and absent `Sec-Fetch-Site`;
  the "both absent" allow-through case named explicitly, with the
  same-machine-caller rationale in the test's own comment, not just the
  code's).
- `routeWrite`: same pure-function testing shape as `route` — no server
  needed, per-branch coverage (unknown path, malformed confirm body,
  successful preview, successful apply) via a fake `WriteAction`, the
  same `withRegistry`-style isolation `dashboard_test.go` already uses
  for the read registry.
- An httptest-based end-to-end test proving a same-origin POST succeeds
  and a cross-origin one (`Origin: https://evil.example`) gets a 403 —
  the actual regression this whole design exists to prevent, not just
  each unit in isolation.
- `dashboard_smoke_test.go` gains a real-binary case: POST `/clean/
  preview` against the running dashboard and assert it never touches the
  filesystem (point it at a directory with a known-fingerprint temp file
  and diff before/after) — apply is deliberately **not** smoke-tested
  against real files for the same reason `cli_smoke_test.go` excludes
  `clean` without `--dry-run`.

## 6. Open questions for the review panel

1. Is `Sec-Fetch-Site: none` correctly included as allowed? (It covers a
   user typing the dashboard URL directly / a bookmark / a browser
   extension's own direct navigation — legitimate same-user actions that
   aren't strictly "same-origin" by the header's own definition.)
2. Is refactoring `clean.Run` to separate a pure `Preview`/`Plan` step
   from the live delete (mirroring `doctor.Collect`/`Analyze`) the right
   shape, or should the dashboard call something narrower?
3. Does `WriteAction` as a wholly separate registry from `Module` hold up,
   or should it be a field on `Module` after all once there's a second
   real write action to generalize from?

## As built

The panel converged on "go-with-changes" from all seven reviewers, with
three findings that changed the shape of what actually shipped
(commits `170e08e`, `9da3dd5`) from §2–§3 above:

- **§2's `sameOriginOnly` signature was unimplementable as drafted.**
  `func sameOriginOnly(next http.Handler, origin string) http.Handler`
  assumed `dashboard.Serve` could compute the real bound address and
  pass it in — but the actual bound address (needed for the ephemeral
  `--addr 127.0.0.1:0` case) is only known inside `guide.ServeLocal`,
  after `net.Listen` runs, never surfaced back out to `dashboard.Serve`.
  Fixed by moving `sameOriginOnly` into `internal/guide/serve.go`
  itself, applied generically inside `ServeLocal` right after the
  existing `allowedHostsOnly` call, reusing the same `addr`/`port`
  variables `ServeLocal` already computes. This one relocation also
  closed two more findings at once: it accepts **both**
  `127.0.0.1:<port>` and `localhost:<port>` as valid origins (the draft
  only checked one — a real regression the review caught, since
  `allowedHostsOnly` already accepts both), and it now protects *any*
  current or future `ServeLocal`-backed handler uniformly, not just the
  dashboard's write actions specifically.
- **§3's `Handler func(PageContext, *http.Request)` became
  `Handler func(PageContext, []byte)`.** A raw, already-read body
  instead of a live request — so a `WriteAction`'s handler is
  constructed and called directly in tests the same way `Module.Render`
  already is, with no `httptest.NewRequest` boilerplate needed. `Path`
  and `Available func(PageContext) bool` (mirroring `Module.Available`,
  present from day one per review even though nothing needs it yet)
  round out the actual `WriteAction` struct — see
  `internal/dashboard/module.go`.
- `RegisterWriteAction` shipped as `RegisterWrite`, matching `Register`'s
  own naming instead of a longer, inconsistent name.

Findings from the panel not yet acted on — these are the actual
remaining scope of this item, tracked in
[`implementation-plan.md`](implementation-plan.md):
rewriting `clean.go` to reuse `ReclaimableSummary(budget)` for
`/clean/preview` and a proper `Apply`-style function returning
structured data instead of only `error`; mandating `html/template` for
the new write-action render functions with a crafted-filename
regression test; a single-flight guard against concurrent apply calls;
`/clean/apply`'s non-blocking/progress-feedback story; the actual UX
spec (what preview shows, per-category exclusion, partial-failure
display); a preview→apply single-use token as defense-in-depth
(recommended by the security reviewer, not required — the same-origin
check is the real boundary here, per §4 above).

By 2026-09-03, `/clean/preview` (backend + button, commits `7265006`,
`c2dd226`) is the only one of these actually shipped — see
`implementation-plan.md` for exact status. `/clean/apply` itself is
still pending the security-focused review §4 originally scoped for it
("A security-focused review... signs off on the CSRF/auth model
specifically, before any write route ships" — the exit criteria was met
for the CSRF/`WriteAction` foundation itself, but a real *mutating*
route is new risk surface the panel never evaluated). §7 below is that
design, submitted for exactly that review before implementation.

## 7. `/clean/apply` design (pending its own security review)

Extends §4 with the concrete decisions needed to actually implement it.

**Endpoint**: `POST /clean/apply`, registered as a second `WriteAction`
alongside `/clean/preview` in `internal/dashboard/modules_clean.go`.
Routed through the same `sameOriginOnly` protection every write action
gets — no new middleware.

**Request**: body must be exactly `{"confirm": true}` (parsed with
`encoding/json`, a fixed struct, not a generic map — an unknown extra
field is ignored, not rejected, matching `encoding/json`'s default and
avoiding a brittle strict-schema requirement for a single boolean).
Anything else — missing body, `{"confirm": false}`, malformed JSON, a
body over `maxWriteActionBody` — is rejected with `400` *before*
`clean.Apply` is ever called. This mirrors the CLI's own `--yes`/`-y`
bar (a deliberate, hard-to-mistype confirmation) rather than inventing
a stricter web-only requirement: the same-origin check is what actually
defends against an unintended trigger from a hostile page; `confirm`
defends against a client-side bug (e.g. a button wired to the wrong
handler) more than an attacker, exactly as reasoned in §4.

**Single-flight guard**: a package-level guard in
`internal/dashboard/modules_clean.go`, shaped like
`prepareAdviceCache`'s (`internal/dashboard/modules_advice.go`) but
simpler — no TTL/caching semantics are wanted here (a second apply
*should* eventually be allowed to run, just never concurrently with a
first one still in flight):

```go
var cleanApplyMu sync.Mutex

func handleCleanApply(_ PageContext, body []byte) (int, string) {
	var req struct{ Confirm bool `json:"confirm"` }
	if json.Unmarshal(body, &req) != nil || !req.Confirm {
		return http.StatusBadRequest, `{"error":"confirm must be true"}`
	}
	if !cleanApplyMu.TryLock() {
		return http.StatusConflict, `{"error":"a clean is already running"}`
	}
	defer cleanApplyMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return http.StatusInternalServerError, `{"error":"cannot determine home directory"}`
	}
	result := clean.Apply(home, clean.Options{Assume: true})
	return http.StatusOK, renderCleanApplyResult(result)
}
```

`sync.Mutex.TryLock` (stdlib since Go 1.18, already vitals' Go version
floor) is enough — no channel/semaphore machinery needed for "reject a
second concurrent caller outright" rather than "make it wait." A
concurrent request during a real apply is exactly the failure mode a
double-click or two open browser tabs would produce; `409 Conflict` is
the correct HTTP semantics for "the request conflicts with the current
state of the resource" (RFC 9110 §15.5.10) and needs no new status-code
convention in this codebase.

**Response**: `renderCleanApplyResult(result clean.Result) string`
mirrors `renderCleanPreview` exactly — `html/template`, a `.card`
showing `result.FreedBytes` (humanized) and each `result.Records` entry
as a `row()`-shaped line, plus a `partial`-style note if a record's
`Bytes` came back lower than what preview measured for the same path
(informational only; `clean.Apply`'s existing accounting, verified
earlier this session not to over-count on a real partial failure,
already handles the underlying correctness — this is purely a
proof: the person clicking Apply sees *what actually happened*, not
just a bare "done").

**Client-side UX** (extends `modules_clean.go`'s existing script): an
"Apply" button appears only after a preview response has rendered
(`out.innerHTML` being non-empty is enough of a signal — no separate
state variable needed since the DOM already carries it), and is
disabled again for the duration of its own in-flight request the same
way "Preview" already is. Not enforced server-side beyond `confirm`
(per §4) — this is UX sequencing, not a security control.

**Preview→apply single-use token**: still not implemented, per the
original open question — `sameOriginOnly` is the real boundary, and a
token would add server-side state (today there is none at all) for a
threat this codebase's own model (§1) already excludes: a legitimate
same-machine, same-origin caller replaying a request is the user's own
action, not an attacker's. Flagged again here explicitly so this
review pass can either close the question or override it.
