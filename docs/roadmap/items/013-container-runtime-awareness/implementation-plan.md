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

## Still owed

- **`kind` / `k3s` end-to-end pass on Linux.** Development e2e was
  against a real local Docker (a running compose project) on macOS; the
  Kubernetes path is fixture-tested but not yet exercised against a
  real local cluster.

## Deferred (not v1 — see `design.md` §10–§11)

- Windows named-pipe Docker (`go-winio` dep) — Linux/macOS first, as
  `smartctl` did.
- The 011 console-view `containers` panel — needs `QuickAssess` to
  carry containers, a trade against 011's no-probe speed model; 011's
  call.
- `RestartCount` trend via the history file (v1 uses a static
  `>= 5` threshold).
- `internal/tools` registry rows for `docker`/`kubectl`.
- k8s nodes / events / deployments.
