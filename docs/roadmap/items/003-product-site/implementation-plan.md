# Implementation plan — 003 Public product site

[docs](../../../index.md) / [Roadmap](../../index.md) / [003 — Public product site](index.md) / **Implementation plan**

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

## Tasks

- [x] **Every claim on this page must be 100% accurate — no overclaiming,
      no marketing fibs.** Every specific claim (command list, dependency
      count, supported platforms, cloud LLM provider count, install
      commands) was checked directly against `internal/help/help.go`,
      `go.mod`, `build.sh`'s platform matrix, and `internal/llm/llm.go`'s
      provider list — not written from memory. The original "competitive
      comparison table from the persona review" referenced in
      `docs/architecture/design.md` §5.3 turned out not to be archived
      anywhere in this repo; rather than fabricate its old content, wrote
      a fresh one, worded as "what each tool is for" / "what vitals
      adds" (never a bare superiority claim), with an explicit note that
      it describes each tool's stated purpose as of this vitals version,
      not a guarantee about their current full feature set.
- [x] Built the static page at `site/index.html` — a single file, no
      build step, matching this repo's own no-bundler philosophy.
      Location decision: **not** `docs/index.html` (would collide with
      the TechDocs `docs/` tree already living there) and **not** a
      `gh-pages` branch — a new `site/` folder on `main`, deployed via
      GitHub's own Actions-based Pages pipeline
      (`.github/workflows/pages.yml`: `actions/configure-pages` +
      `actions/upload-pages-artifact` + `actions/deploy-pages`), which
      has no branch/folder collision constraint at all since Pages just
      serves whatever the workflow uploads. Install instructions are
      copied verbatim from `README.md`'s Install section. Links to the
      user guide point at its GitHub blob URL
      (`docs/user-guide.md`) — the TechDocs `mkdocs` site itself isn't
      published anywhere yet, so that's the only real, always-available
      link today.
- [x] Reused the dashboard's established palette (teal accent, OK/warn/
      critical severity colors) for brand consistency, per the design
      doc's instruction — plus a linked Google Font (Inter + JetBrains
      Mono), which is fine here since this page is public and
      network-reachable by design, unlike the offline-only
      dashboard/guide.
- [ ] **Enable GitHub Pages for the repo — a one-time repo settings
      change only you can make**: Settings → Pages → "Build and
      deployment" → Source → **GitHub Actions** (not "Deploy from a
      branch"). Once set, `.github/workflows/pages.yml` runs on every
      push touching `site/` and publishes automatically; the site's URL
      will be `https://gautampachnanda101.github.io/vitals/`.
- [ ] Add the live site URL to `README.md`'s top, once the above setting
      is flipped and the first deploy has actually succeeded — not
      before, since an unverified URL would violate this item's own
      accuracy requirement.

## Exit criteria

The page is live at a public URL, linked from `README.md`'s top.
