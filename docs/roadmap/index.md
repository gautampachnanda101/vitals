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
| [003](items/003-product-site/) | Public product site | Not started | — (parallel) | v0.5.0 |
| [004](items/004-native-launcher/) | Native double-click launcher | Not started | 002 | v0.6.0 |
| [005](items/005-dashboard-write-actions/) | Dashboard write actions | Not started | 001, 002, 004 | v0.7.0+ |
| [006](items/006-coverage-hardening/) | Coverage hardening to 95%+ | Done | — (cross-cutting, ongoing) | ongoing |
| [007](items/007-dashboard-visuals/) | Dashboard visuals & machine identity | Not started — unscheduled, see its Trigger | 002 | not yet |

## Releases

- [v0.5.0](releases/v0.5.0.md) — dashboard foundation + MVP, product site.

## Why 003 has no dependency on 001/002

The product site is static and public; the dashboard is dynamic and
local-only. They share nothing but visual language. Per the product-manager
review that shaped this plan (design doc §7), the site is scheduled
alongside the dashboard specifically *because* it has no code dependency on
it — waiting for the dashboard to ship first was the sequencing mistake the
review caught.
