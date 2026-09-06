# 013 — Local container & Kubernetes awareness

[docs](../../../index.md) / [Roadmap](../../index.md) / **013 — Local container & Kubernetes awareness**

**Implementation plan**: [what's left →](implementation-plan.md)

**Status**: [`design.md`](design.md) drafted 2026-09-05 — pre-review.
Needs a `review-panel` pass before any code (a new data source across a
new trust boundary, a local daemon socket).
**Depends on**: `doctor`'s `Collect`/`Analyze`, the `internal/tools`
registry pattern; feeds [011](../011-console-at-a-glance-view/)'s
container panel and would add a dashboard page
**Target release**: not yet
**Architecture**: [`design.md`](design.md) — stdlib HTTP over the Docker socket (no vendored client), `kubectl` handoff for k8s (no client-go), read-only fixed endpoint list, the local-daemon-socket trust boundary, gating like the GPU page. Pre-review.

## What

When a container runtime or a local Kubernetes is actually running on
this machine, surface it — the same way `doctor` already surfaces a GPU
only when one is present. Two runtimes in scope:

- **Docker / a Docker-API-compatible runtime** (Docker Desktop,
  Colima, Podman's Docker-compat socket, Rancher Desktop). Reachable
  at a local socket — `/var/run/docker.sock` on Linux/macOS, a named
  pipe on Windows — speaking Docker's HTTP+JSON Engine API.
- **A local Kubernetes** (k3s, kind, minikube, Docker Desktop's
  built-in k8s, Rancher Desktop). Detected via a reachable current
  kubeconfig context that points at a loopback/`.local` API server.

## The vitals angle — diagnosis, not `docker stats` again

Per the standing bet, vitals is the diagnostic layer over established
tools, not another one of them. So this is **not** a `docker stats` /
`kubectl top` reimplementation. It is:

- **Correlation with host pressure.** "Your machine is swapping and the
  three heaviest processes are all containers from the same compose
  project" is a finding no per-tool view gives you. So is "the
  container runtime's own VM is holding 8 GB of your RAM" (Docker
  Desktop / Colima run a Linux VM that shows up as one opaque host
  process today — see the existing memhogs family-resolution work).
- **Failure signals, ranked.** Containers in a restart loop
  (`RestartCount` climbing), an OOM-killed container
  (`State.OOMKilled`), pods in `CrashLoopBackOff` /
  `ImagePullBackOff`, a container whose healthcheck is failing — as
  `diag.Finding`s with the same severity ranking every other signal
  gets, with a concrete next step (`docker logs <name>`,
  `kubectl describe pod <name>`).
- **A resource attribution the process table can't give.** `vitals
  top` sees `com.docker.backend`; it can't see that 90% of that is one
  container. This closes that gap.

## Why it needs review before code

- **New trust boundary: a local daemon socket.** vitals has never
  opened a unix socket / named pipe to another privileged daemon.
  Read-only Engine API calls (`/containers/json`, `/containers/<id>/stats?stream=false`,
  `/info`) only — never `POST` (no create/start/stop/kill from
  vitals; that's `heal`'s territory if ever, separately reviewed).
  The review settles: which endpoints, what timeout, what happens when
  the socket exists but the daemon is wedged (a real, common state).
- **Dependency question.** Docker's Engine API is plain HTTP+JSON over
  a socket — reachable with stdlib (`net.Dial("unix", …)` +
  `net/http`), no `github.com/docker/docker` client (a large
  transitive tree). Kubernetes is the opposite: `client-go` is huge,
  so k8s support should shell out to an installed `kubectl` (the
  `internal/tools` companion pattern — add `kubectl` and maybe
  `docker` to the registry), parsing `kubectl get pods -o json`. The
  review confirms "stdlib for Docker, `kubectl` handoff for k8s, no
  vendored client for either".
- **Gating and cost.** The panel/page/finding only appears when a
  runtime is genuinely reachable, and the detection probe itself must
  be cheap and bounded (a wedged daemon must not hang `doctor`). Same
  discipline as the DNS-latency probe's own timeout.
- **Scope creep risk.** This is "is it healthy", not "manage my
  containers". No `compose` orchestration, no `kubectl apply`, no
  registry auth. The review draws that line explicitly.

## Where it surfaces

- a `doctor` signal + findings (restart loops, OOM kills, crashlooping
  pods) — the primary deliverable;
- a `containers` block in [011](../011-console-at-a-glance-view/)'s
  console view (011's design already reserves a panel for it);
- a dashboard page, gated like the GPU page;
- `--json` (`snapshot.containers` — additive schema bump).

## Plan

[`implementation-plan.md`](implementation-plan.md) — empty until a
design doc exists and its `review-panel` pass converges.
