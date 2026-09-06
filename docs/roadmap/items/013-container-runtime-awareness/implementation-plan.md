# Implementation plan — 013 Local container & Kubernetes awareness

[docs](../../../index.md) / [Roadmap](../../index.md) / [013 — Local container & Kubernetes awareness](index.md) / **Implementation plan**

Designed and reviewed inline (`design.md` §10), then built. What shipped:

| Piece | Where |
|---|---|
| `internal/containers` — `Probe`/`Sample`/`Report`/`Container`, injected `transport` seam | `internal/containers/{containers,docker,kubernetes,exec}.go` |
| Docker Engine API, stdlib only, GET-only fixed endpoint list, one `dockerGET` chokepoint | `internal/containers/docker.go` |
| `kubectl` handoff, `isLocalAPIServer` gate (loopback / RFC1918 / CGNAT / `.local` only) | `internal/containers/kubernetes.go` |
| `Snapshot.Containers`, schema **1.5.0** (additive), golden regenerated | `internal/doctor/{analyze,schema}.go`, `schema.json`, `testdata/schema_fields.golden` |
| `collectContainers()` behind `SkipProbes` (list only, no stats) | `internal/doctor/collect.go` |
| `analyzeContainers` — OOMKilled/CrashLoop critical; restart-loop / unhealthy / VM-RAM-hog warning | `internal/doctor/analyze_containers.go` |
| `vitals containers` (`RunContainers`, aliases docker/k8s/kubernetes) | `internal/doctor/containers_cmd.go`, `main.go`, `internal/help/help.go` |
| Dashboard **Containers** page, nav-gated on `HasContainers`, own 12s stats cache | `internal/dashboard/modules_containers.go`, `module.go`, `render.go` |
| Coverage floor for `internal/containers` | `check_coverage.py` |

## Follow-on — done 2026-09-06

| Piece | Where |
|---|---|
| **Windows named-pipe Docker.** `defaultDockerEndpoint`/`dialDocker` split into `//go:build` unix/windows files; Windows dials `\\.\pipe\docker_engine` via `github.com/Microsoft/go-winio` (build-tagged, never in a mac/Linux build; see `AGENTS.md` "One dependency"). | `internal/containers/endpoint_{unix,windows}.go` |
| **`--runtime docker\|kubernetes` override** on `vitals containers` (`ProbeRuntime` / `containerProber` seam). Default stays auto (docker-first); the override is for a kind/k3s cluster whose nodes are themselves Docker containers, where auto would stop at Docker. | `internal/containers/containers.go`, `internal/doctor/containers_cmd.go`, `main.go`, `internal/help/help.go` |
| **Live integration CI** — path-scoped (`internal/containers/**` etc.). A real Docker daemon with a restart-loop container + a best-effort OOM container; a real `kind` cluster with a `CrashLoopBackOff` pod; `vitals` run as the compiled binary asserting detection + the doctor finding + the cloud-context rejection. Runs only when the container code changes. | `.github/workflows/containers-integration.yml` |
| `realExec` success-path assertion (the `sudo purge` / delegate exec shape; the literal `sudo purge` still can't run in CI — needs sudo + macOS) | `internal/heal/heal_test.go` |

## Follow-on 2 — done 2026-09-06

| Piece | Where |
|---|---|
| **Every reachable runtime, not just the first.** `containers.ProbeAll` returns Docker *and* a local Kubernetes when both are present (a kind/k3s host has both). `doctor.Snapshot.Containers` is now `[]Report`; schema **1.6.0** (retype of a one-release-old field, plus `ports` / `created_unix`). `analyzeContainers`, `vitals containers`, and the dashboard all iterate every runtime; the dashboard shows a titled section per runtime, an Overview card, and the CLI's `--runtime` now *filters* rather than forcing a re-probe. | `internal/containers/containers.go`, `internal/doctor/{analyze,collect,containers_cmd}*.go`, `internal/dashboard/modules_containers.go`, `modules_overview.go` |
| **Richer container detail** — per-container cards: published ports, short ID, uptime, compose-project / namespace grouping, a memory-vs-limit bar. | `internal/dashboard/modules_containers.go` |
| Sidebar switched to the standard **Lucide** icon set (inlined paths, no dependency); rendered in the accent colour so they're visible in dark mode. | `internal/dashboard/render.go` |

Still deferred: the 011 console-view `containers` panel; `RestartCount` trend history; k8s nodes/events; a container-count trend sparkline on the Overview (needs its own history series).

### Future — remote / generic Kubernetes clusters (opt-in)

Today `isLocalAPIServer` gates the k8s path to a context whose API
server is loopback / RFC1918 / CGNAT / `.local` / `.internal` — kind,
k3s, k3d, minikube, Docker Desktop, Rancher Desktop, microk8s all pass;
a public IP or a real domain (a cloud cluster, or a self-hosted one on a
public address) is rejected on purpose (no egress, no cloud-credential
handling — the 013 trust boundary). The detection isn't tied to kind in
any way; kind is just how CI gets a real cluster.

A follow-up could make the non-local case an **explicit opt-in**:
`vitals containers --kube-context <name>` (or `--allow-remote-kube`),
still read-only, still `kubectl get pods` only, but acknowledging the
user is pointing at a cluster their own kubeconfig already trusts.
Needs its own review of the trust boundary (an unreachable/slow remote
API server must still be bounded; no context switch is written back)
before it ships.

## Deferred (see `design.md` §10–§11)

- The 011 console-view `containers` panel — needs `QuickAssess` to
  carry containers, a trade against 011's no-probe speed model; 011's
  call.
- `RestartCount` trend via the history file (v1 uses a static
  `>= 5` threshold).
- `internal/tools` registry rows for `docker`/`kubectl`.
- k8s nodes / events / deployments.
