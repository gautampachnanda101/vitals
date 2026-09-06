# vitals roadmap

[docs](../index.md) / **Roadmap**

Architecture and rationale for everything here: [design doc](../architecture/design.md).
This is the roadmap: one item per initiative, phased, tied to the release
that ships it. It's meant to stay current — see the maintenance rule in
`AGENTS.md`'s "Roadmap discipline" section: an item's
`implementation-plan.md` shows what's *left*, not a historical log.

## How this is organized

```
docs/                        TechDocs root (see mkdocs.yml, catalog-info.yaml)
  index.md                   docs home
  user-guide.md              the embedded CLI user guide
  architecture/
    design.md                architecture + the six-agent review outcome
  roadmap/
    index.md                 this file
    items/
      NNN-slug/
        index.md             what, why, status, depends-on, target release
        implementation-plan.md   the live task list for this item
    releases/
      vX.Y.Z.md              which items ship in this release
```

Items are numbered in dependency order, not necessarily build order — a
higher-numbered item may be designed already even if a lower-numbered one
is still in progress, but it generally can't *ship* before the items it
depends on.

## Items

| # | Item | Status | Depends on | Target |
|---|---|---|---|---|
| [001](items/001-dashboard-foundation/) | Dashboard foundation fixes | Done | — | v0.5.0 |
| [002](items/002-dashboard-mvp/) | `vitals dashboard` MVP | Done | 001 | v0.5.0 |
| [003](items/003-product-site/) | Public product site | Done | — (parallel) | v0.5.0 |
| [004](items/004-native-launcher/) | Native double-click launcher | Mostly done (macOS verified; Linux/Windows unverified) | 002 | v0.6.0 |
| [005](items/005-dashboard-write-actions/) | Dashboard write actions | Done | 001, 002, 004 | v0.7.0+ |
| [006](items/006-coverage-hardening/) | Coverage hardening to 95%+ | Done | — (cross-cutting, ongoing) | ongoing |
| [007](items/007-dashboard-visuals/) | Dashboard visuals & machine identity | Delivered incrementally — System page + overview cards + sparklines in v0.8.0+; richer charts still gated | 002 | v0.8.0+ |
| [008](items/008-heal-command/) | `vitals heal` — apply a finding's fix | Shipped 2026-09-06 — `internal/heal`, two v1 remedies, schema 1.4.0 | diag findings/fixes model | v0.9.0 |
| [009](items/009-raw-coverage-95/) | Raw 95%+ coverage, no live-glue exemption | Done | — (cross-cutting) | ongoing |
| [010](items/010-companion-tools-integration/) | Companion tools: real integration, not just a catalog | Done — `nvtop` (`gpu --live`), `jdupes` (`dupes --fast`), `smartctl` (S.M.A.R.T. on disk) all shipped | `internal/tools` (existing) | v0.8.0+ |
| [011](items/011-console-at-a-glance-view/) | Console at-a-glance view | Shipped 2026-09-06 — `internal/consoleview`, `vitals view` + bare-`vitals`-on-TTY | `doctor` snapshot/cache (existing) | v0.9.0 |
| [012](items/012-disk-consumers-detail/) | Per-resource consumers: the deeper numbers | Shipped — per-process disk I/O rate (Linux/Windows) + real macOS per-process energy; per-process network bandwidth documented as not achievable | `internal/monitor` sampling, `internal/power` | shipped |
| [013](items/013-container-runtime-awareness/) | Local container & Kubernetes awareness | Shipped — `internal/containers` (stdlib Docker Engine-API incl. Windows named pipe + local-only kubectl); v0.10.0 probes every local runtime in one pass (`--json` `snapshot.containers` array, schema 1.6.0), richer per-container detail, dashboard sections per runtime + Overview card; live `kind`+Docker integration CI; 011 console panel + remote k8s deferred | `doctor` (existing) | shipped |

## Releases

The [GitHub Releases](https://github.com/gautampachnanda101/vitals/releases)
page is the authoritative, per-tag history with binaries. These pages
map a release to the roadmap items it shipped.

- [v0.10.0](releases/v0.10.0.md) — `vitals containers` shows every local
  runtime (Docker + local Kubernetes) at once, richer per-container
  detail, standard dashboard sidebar icons, clearer `vitals help`
  examples. `--json` schema **1.6.0**.
- [v0.9.0](releases/v0.9.0.md) — `vitals heal`, `vitals view`, deeper
  per-resource numbers, local container & Kubernetes awareness,
  self-refreshing dashboard.
- [v0.5.0](releases/v0.5.0.md) — dashboard foundation + MVP, product site.

v0.6.0–v0.8.0 shipped incrementally (companion-tool integration,
dashboard redesign, overview sparklines) without their own roadmap
pages — see the GitHub Releases notes.

## Why 003 has no dependency on 001/002

The product site is static and public; the dashboard is dynamic and
local-only. They share nothing but visual language. Per the product-manager
review that shaped this plan (design doc §7), the site is scheduled
alongside the dashboard specifically *because* it has no code dependency on
it — waiting for the dashboard to ship first was the sequencing mistake the
review caught.
