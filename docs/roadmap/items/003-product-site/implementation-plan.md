# Implementation plan — 003 Public product site

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

## Tasks

- [ ] Build the static page (location depends on whether this repo serves
      GitHub Pages from `/docs` or a `gh-pages` branch — decide before
      writing, since it affects the file's path; do not let it collide
      with the TechDocs `docs/` tree this file itself lives in): install
      instructions (mirror `README.md`'s Install section — keep the two
      from drifting independently, e.g. by testing one against the
      other), a short feature tour, the competitive comparison table from
      the persona review, links out to the user guide and GitHub repo.
- [ ] This page is public and network-reachable by design — unlike the
      dashboard/guide, it's fine to use a linked Google Font or a richer
      visual treatment. Reuse the established teal-accent/severity-color
      visual language for brand consistency.
- [ ] Enable GitHub Pages (or equivalent) for the repo once the page
      exists — a repo settings change, not code; easy to forget.

## Exit criteria

The page is live at a public URL, linked from `README.md`'s top.
