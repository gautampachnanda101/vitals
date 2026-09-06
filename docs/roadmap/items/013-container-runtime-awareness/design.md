# Design — 013 Local container & Kubernetes awareness

[docs](../../../index.md) / [Roadmap](../../index.md) / [013 — Local container & Kubernetes awareness](index.md) / **Design**

**Status: shipped (v1).** Reviewed inline against this doc and the
`internal/doctor` / `internal/dashboard` code before implementation
(§10 records the decisions on the six open questions). The Docker
Engine-API path and the local-only `kubectl` path are built, wired into
`doctor` findings + `--json` (schema 1.5.0), `vitals containers`, and a
nav-gated dashboard page. §11 records what was built and what was
deliberately deferred.

## 1. What this is, and what it is not

When a container runtime or a local Kubernetes is running on this
machine, surface it — gated exactly like the GPU page (only shown when
present). It is:

- a `doctor` **signal + findings** — the primary deliverable;
- a `containers` block in [011](../011-console-at-a-glance-view/)'s
  console view (011's design already reserves the panel);
- a dashboard page, nav-gated;
- `snapshot.containers` in `--json` (additive schema bump).

It is **not**:

- **`docker stats` / `kubectl top` reimplemented.** vitals is the
  diagnostic layer over established tools, not another one. The value is
  the verdict layer: correlation with host pressure, ranked failure
  signals, and attributing the opaque `com.docker.backend` host process
  to a real container.
- **container management.** Read-only. No create / start / stop / kill /
  `compose` / `kubectl apply` from vitals — that is `heal`'s territory
  if ever, on its own review.
- **remote clusters.** Only a *local* kubeconfig context pointing at a
  loopback / `.local` API server. A context pointing at a cloud
  cluster is ignored (no credentials handling, no egress).

## 2. Runtimes in scope

### Docker / Docker-API-compatible

Docker Desktop, Colima, Podman's Docker-compat socket, Rancher Desktop
— all speak Docker's **HTTP + JSON Engine API** over a local endpoint:

- Linux / macOS: a unix socket, `DOCKER_HOST` env override else
  `/var/run/docker.sock` (and `~/.docker/run/docker.sock` /
  `~/.colima/default/docker.sock` as fallbacks the review can confirm).
- Windows: `npipe:////./pipe/docker_engine`.

Reachable with **stdlib only** — `net.Dial("unix", path)` (or a
`winio` named-pipe dial on Windows — the one place a tiny build-tagged
helper may be unavoidable; the review decides whether Windows Docker
support waits) + `net/http` with a custom `DialContext`. **No
`github.com/docker/docker` client** (a large transitive tree).

### Local Kubernetes

k3s, kind, minikube, Docker Desktop's built-in k8s, Rancher Desktop.
`client-go` is huge, so k8s support **shells out to an installed
`kubectl`** — the `internal/tools` companion pattern. Add `kubectl`
(and `docker` for `--live` handoffs) to `internal/tools.Registry`.
Detection: `kubectl config current-context` succeeds *and* the
context's server URL is loopback / a `.local` / an RFC1918 address.
Data: `kubectl get pods -A -o json` (bounded, `--request-timeout`).

## 3. The endpoints vitals calls (Docker), read-only

| Endpoint | For |
|---|---|
| `GET /_ping` / `GET /info` | is the daemon alive; container/runtime counts; total memory the VM holds |
| `GET /containers/json?all=1` | every container: name, image, state, status, `RestartCount`, labels (compose project) |
| `GET /containers/{id}/stats?stream=false` | one non-streaming CPU/mem sample per running container |

Never `POST`, never `/exec`, never `/containers/{id}/logs` (that's a
"run `docker logs X`" hint in the finding, not something vitals
streams). The review pins the exact list and the per-call timeout.

## 4. The findings this raises

Ranked with the same `diag.Severity` model as every other signal:

- **critical** — a container `OOMKilled` (`State.OOMKilled`), or a pod
  in `CrashLoopBackOff` / `ImagePullBackOff` (from `kubectl`'s
  `status.containerStatuses[].state.waiting.reason`).
- **warning** — `RestartCount` climbing across `doctor` runs (needs the
  same history mechanism sparklines use, keyed by container id — or,
  simpler for v1, `RestartCount > N` in a short window); a container
  whose healthcheck is `unhealthy`; the container runtime's VM holding
  a large fraction of host RAM (correlate with the memory signal).
- **informational context** — "3 of the 5 heaviest processes are
  containers from compose project `foo`" alongside the CPU/memory
  verdict.

Each finding carries a concrete next step in its `Fixes`
(`docker logs <name>`, `docker inspect <name>`, `kubectl describe pod
<name>`) — never an action vitals takes.

### Why the host process scan isn't enough (the motivating case)

On macOS (and Windows) containers run inside the runtime's Linux VM.
A host process scan — what `vitals` / `monitor` / the Memory page use
today — sees only the VM helper process's resident set, which the
Virtualization.framework memory balloon keeps far below the VM's
configured RAM. Observed 2026-09-06: with a 2 GB `llama-server` and a
610 MB editor on top, the Docker VM process showed ~200 MB RSS and
didn't make the Memory page's top 5 — *correct* for how RSS is
measured, but it hides real container memory. `GET
/containers/{id}/stats` is the only accurate source, and it reports
both numbers this item should surface:

- **consumed** — `memory_stats.usage` (minus `stats.cache` on cgroup
  v1), the container's actual working set.
- **reserved** — `memory_stats.limit` (the container's `--memory` cap,
  or the VM total when uncapped) and, for k8s, `resources.requests` vs
  `resources.limits` from the pod spec.

The container page shows `used / limit` per container (the same
`consumed vs reserved` framing Activity Monitor lacks for VMs), and the
"runtime VM holding a large fraction of host RAM" warning above
correlates the VM total against the host memory signal so the Memory
verdict stops under-counting a container-heavy machine.

## 5. Trust boundary (the new part)

vitals has never opened a socket to another privileged daemon. The
analysis the review must sign off:

- **Read-only, fixed endpoint list (§3).** No method but `GET`. A
  compromised or malicious Docker daemon can feed vitals bogus JSON —
  the blast radius is "vitals shows wrong container info / a spurious
  finding", the same as any other bad probe result, not code
  execution. Container names, image names and labels are
  attacker-influenceable text → they go through `ui.Sanitize` before
  any terminal render (the choke point added in the 008/011 shared
  foundation) and through `html/template` on the web side.
- **The socket's own permissions are the auth.** If the invoking user
  can read `docker.sock`, they can already run `docker ps` — vitals
  grants no new capability, it just reads what the user could already
  read. If the socket isn't readable, detection fails closed and the
  feature simply doesn't appear.
- **Bounded and non-blocking.** A wedged daemon (socket present,
  daemon hung) is a real, common state. Every call gets a short
  `context` timeout (≈1s); a timeout means "no container data this
  pass", never a hung `doctor`. The `stats` calls across N running
  containers run concurrently under one shared budget, capped (e.g. 20
  containers sampled), so a host with hundreds of containers can't turn
  this into a fan-out.
- **No egress.** The unix socket / named pipe is local. The `kubectl`
  path only runs against a context already validated as loopback/local
  (§2); a cloud context is skipped entirely.

## 6. Gating & cost

- The panel / page / findings appear only when a runtime is genuinely
  reachable — detection is one cheap `/_ping` (or `kubectl
  current-context`) with a ≈250ms timeout, run as part of the snapshot
  refresh, not per render.
- On a machine with no runtime: one failed dial, instant, nothing
  shown. Same shape as the GPU probe on a machine with no GPU.
- Feeds the snapshot cache like every other signal; never collected
  inside a `Render`.

## 7. Where the data lives

- New `internal/containers` package: `Probe(ctx) ([]Container, error)`
  — pure-ish orchestrator with an injected transport (the
  `internal/smart` / `internal/tools` pattern) so the Engine-API
  parsing and the `kubectl`-JSON parsing are fixture-tested with no
  daemon.
- `doctor.Snapshot` gains `Containers []Container` (additive `--json`
  schema bump, MINOR); `analyzeContainers` in `analyze.go` raises the
  §4 findings, pure and fixture-tested.
- Wired into `doctor.Collect` behind the `SkipProbes` flag (it's a
  subprocess/socket signal, same class as GPU/power/LLM).

## 8. Open questions for the review

1. **Windows Docker** — the named-pipe dial needs `github.com/Microsoft/go-winio`
   (a small, single-purpose dep) or a build-tagged `syscall` helper.
   Ship Windows Docker support in v1, or Linux/macOS first and Windows
   as a follow-on (matching how `smartctl` deferred Windows)?
2. **`RestartCount` trend** — reuse `doctor`'s history file (new
   per-container series) for "restarts climbing", or ship v1 with just
   `RestartCount > 0 && lastStarted < 5m ago` and add the trend later?
   The history file was explicitly kept resource-sample-only in the 011
   review; adding container series is the same "does history carry more
   than resources" question.
3. **`stats` sampling** — `GET /containers/{id}/stats?stream=false`
   still blocks ~1s server-side for its own delta. Across 20
   containers concurrently that's ~1s wall-clock added to a refresh.
   Acceptable behind `SkipProbes`, or should per-container CPU/mem be
   an opt-in (`vitals containers`) rather than part of every `doctor`?
4. **k8s scope** — is `kubectl get pods` enough for v1, or does it need
   nodes / events / deployments to say anything useful? Start minimal.
5. **`internal/tools` additions** — adding `docker` and `kubectl` to
   the registry means `vitals tools` lists them and `--install` offers
   them. Is offering to install Docker/kubectl appropriate, or should
   these be detect-only entries?

## 9. Verification gates

Standard repo gates + `internal/containers` at the 95% raw floor
(`check_coverage.py` entry) — the injected-transport seam makes the
Engine-API and `kubectl`-JSON parsing fully fixture-testable (real
captured JSON as fixtures: running/exited/OOMKilled containers, an
empty list, a wedged-daemon timeout, a CrashLoopBackOff pod, a
malformed response). `analyzeContainers` fixture-tested like every
other `Analyze` rule. No real daemon in CI. `vitals` end-to-end against
a real local Docker + a real `kind`/`k3s` cluster on macOS and Linux
before the item is called done, recorded here.

## 10. Decisions (inline review, 2026-09-06)

The six open questions (§8), resolved before implementation against this
doc plus the existing `internal/doctor` / `internal/dashboard` code:

1. **Windows Docker — deferred.** Linux/macOS unix-socket support in
   v1; the named-pipe dial (and its `go-winio` dep) is a follow-on,
   matching how `smartctl` deferred Windows. `defaultDockerEndpoint`
   returns `""` on Windows, so the feature is simply absent there — no
   new dependency, the "one dependency (gopsutil)" claim holds.
2. **`RestartCount` trend — deferred.** v1 fires on
   `RestartCount >= 5` (a static threshold), no per-container history
   series. The 011 review kept the history file resource-sample-only;
   revisiting that is its own decision.
3. **`stats` sampling — opt-in, not on every `doctor`.** The snapshot
   path (`collectContainers`, behind `SkipProbes`) fetches only the
   cheap list + `/info`. Per-container CPU/mem (`/stats?stream=false`,
   ~1s server-side each) is sampled only by `vitals containers` and the
   dashboard Containers page, each behind its own short-TTL cache. So
   `doctor` never pays the fan-out.
4. **k8s scope — `kubectl get pods -A -o json` only.** No nodes /
   events / deployments in v1.
5. **`internal/tools` entries — deferred.** `vitals containers` does
   its own detection (socket dial / `kubectl` lookup); adding
   detect-only `docker`/`kubectl` rows to the registry (which currently
   requires a `Packages` map on every entry) is cosmetic and out of v1
   scope.
6. **Windows named pipe — see 1.**

The full 7-agent `review-panel` was not run for v1: the trust-boundary
analysis in §5 was validated directly (read-only, fixed GET-only
endpoint list enforced through one `dockerGET` chokepoint; socket
permissions are the auth; loopback-only kube contexts; every call
context-bounded; all daemon strings through `ui.Sanitize`). A panel
pass remains available if the boundary is widened later (e.g. Windows
pipe support, or any non-GET call).

## 11. As built

**`internal/containers`** (new, `!windows` unix-socket + `kubectl`):

- `Probe(ctx) Report` — tries Docker (its unix socket: `DOCKER_HOST`,
  then `/var/run/docker.sock` and the rootless/Colima/Rancher
  fallbacks) then a local Kubernetes context, else an empty `Report`.
  Never returns an error — a wedged daemon is `Report.Note`, not a
  failure.
- Docker: stdlib only — `net.Dial("unix", …)` + `net/http` with a
  custom `DialContext`, **no `github.com/docker/docker`**. Every call
  goes through one `dockerGET` that is structurally GET-only, against a
  fixed path set: `/_ping`, `/info`, `/containers/json?all=1`,
  `/containers/{id}/json` (inspect — only for not-cleanly-running
  containers, capped), `/containers/{id}/stats?stream=false` (opt-in).
- Kubernetes: shells `kubectl`, and only when `kubectl config
  current-context` resolves *and* the context's server URL is a
  loopback / RFC1918 / CGNAT / `.local` host (`isLocalAPIServer`). A
  cloud context is ignored — no credentials, no egress.
- Bounds: ping 300ms, list 2s, whole snapshot probe 3s; container list
  capped at 50, inspect pass at 25, stats fan-out concurrent under a
  4–6s budget.
- `Sample(ctx, Report)` adds per-container CPU% (computed the way
  `docker stats` does), mem usage (cgroup-v1 page cache subtracted) and
  the mem limit. Opt-in only.
- Injected `transport` seam (socket dialer / `kubectl` exec / PATH) —
  every parser is fixture-tested against an `httptest` server and
  canned command output, no daemon in CI. 96%+ coverage,
  `check_coverage.py` floor added.

**`doctor` wiring:**

- `Snapshot.Containers containers.Report` (`json:"containers"`);
  `SchemaVersion` 1.4.0 → **1.5.0** (additive), `schema.json` +
  `testdata/schema_fields.golden` regenerated.
- `collectContainers()` in `collect.go`, behind `SkipProbes` (same
  class as GPU/power/LLM) — list only, no stats.
- `analyzeContainers` in `analyze_containers.go`, wired into `Analyze`
  and `AnalyzeResource("containers")`: **critical** for `OOMKilled` and
  for a pod stuck in `CrashLoopBackOff`/`ImagePullBackOff`/…;
  **warning** for `RestartCount >= 5`, a failing healthcheck, and the
  runtime VM holding ≥4 GB while system memory is ≥85% used (the
  host-scan blind spot from the Docker "not in memory top 5" question).
  Every `Fix` is a `docker`/`kubectl` command — vitals manages nothing.

**Surfaces:**

- `vitals containers` (`RunContainers`, aliases `docker`/`k8s`/
  `kubernetes`) — the list with per-container CPU/mem (opt-in stats
  sample) + only its findings; `--json`/`--ci`/`--quiet`/`--verbose`
  like the other deep-dives. Nothing shown when no runtime is present.
- Dashboard **Containers** page — nav-gated on `HasContainers`, own
  12s stats cache keyed by runtime endpoint, container/image/status
  through `html/template`.
- `vitals help` gains a `containers` entry.

**Follow-on (2026-09-06), see `implementation-plan.md`:** Windows
named-pipe Docker shipped (`go-winio`, build-tagged); a
`--runtime docker|kubernetes` override for the kind/k3s-on-Docker case;
a path-scoped live integration workflow (real Docker daemon + real
`kind` cluster, `vitals` run as the compiled binary).

**Still deferred:** the console-view (011) `containers` panel — it needs
`QuickAssess` to carry containers, which trades against 011's no-probe
speed model and is 011's call; `RestartCount` trend history;
`internal/tools` registry rows; k8s nodes/events/deployments.

**Verification:** repo gates green. Fixture tests cover the Engine-API
list/inspect/`stats` CPU maths, pod status parsing (CrashLoop, OOM,
not-ready), the loopback-only API-server gate, wedged-daemon and
malformed-response paths, and every `analyzeContainers` finding.
`.github/workflows/containers-integration.yml` exercises it end to end
against a real Docker daemon and a real `kind` cluster (restart-loop
container, CrashLoopBackOff pod, cloud-context rejection), path-scoped
to the container code.

## Plan

[`implementation-plan.md`](implementation-plan.md) records what shipped.
