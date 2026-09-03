# Design — 005 Dashboard write actions

[docs](../../../index.md) / [Roadmap](../../index.md) / [005 — Dashboard write actions](index.md) / **Design**

**Status: draft, pending review-panel pass.** This is the design
`docs/architecture/design.md` §6.5 requires before any write/mutating
dashboard endpoint ships: "no write/mutating endpoint ships without a
same-origin check (Origin/Sec-Fetch-Site header validation) in addition
to the Host-header fix. This is now a named gate on that future phase,
not an open question." This document is that gate being closed —
reviewed on its own, not improvised alongside the first button, per
item 004's own "Why" section.

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
