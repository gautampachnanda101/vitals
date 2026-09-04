#!/usr/bin/env python3
"""Docs consistency gate: catches the doc-rot this repo has actually shipped.

Four checks, each one added because it caught a real bug while writing this
script (see docs: parent/child breadcrumb nav commit) or shortly after:

1. Every relative link in docs/*.md and README.md resolves to a real file.
   (Caught 5 of 6 roadmap items linking to a nonexistent
   docs/roadmap/architecture/design.md via a wrong `../` count.)
2. Every docs/roadmap/items/NNN-slug/ directory has a matching entry in
   mkdocs.yml's nav. (Caught item 006 missing from the nav entirely.)
3. Every page under docs/ other than docs/index.md itself has a breadcrumb
   line back to the docs home (a line starting with "[docs](" or "[←
   docs]("), so a new page can't be added without wiring it into the
   parent/child navigation this check exists to enforce.
4. site/index.html never references a version number that hasn't actually
   been tagged yet. .github/workflows/pages.yml deploys the live public
   site on every push to main touching site/**, completely independent of
   `git tag` — so a commit that bumps the site's version string ahead of
   the real tag puts a false "this is released" claim on the live public
   site for however long the gap lasts. This happened for real
   (2026-09-04): the site went live claiming v0.5.0 while only v0.4.0 was
   tagged, caught by the user reading the live site, not by any check —
   see AGENTS.md's Release process. CI's checkout step needs
   `fetch-depth: 0` for `git tag --list` to see anything at all; a shallow
   clone has no tags.

Usage: python3 check_docs.py
"""
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).parent
DOCS = ROOT / "docs"


def check_links():
    failed = False
    files = list(DOCS.rglob("*.md")) + [ROOT / "README.md"]
    for f in files:
        text = f.read_text()
        for m in re.finditer(r"\]\(([^)]+)\)", text):
            link = m.group(1)
            if link.startswith(("http://", "https://", "#", "mailto:")):
                continue
            target = link.split("#", 1)[0]
            if not target:
                continue
            resolved = (f.parent / target).resolve()
            if not resolved.exists():
                print(f"::error::{f.relative_to(ROOT)}: broken link {link!r} (resolved to {resolved}, which doesn't exist)")
                failed = True
    return failed


def check_roadmap_nav():
    failed = False
    mkdocs_text = (ROOT / "mkdocs.yml").read_text()
    items_dir = DOCS / "roadmap" / "items"
    for item_dir in sorted(items_dir.iterdir()):
        if not item_dir.is_dir():
            continue
        nav_path = f"roadmap/items/{item_dir.name}/index.md"
        if nav_path not in mkdocs_text:
            print(f"::error::mkdocs.yml nav is missing {item_dir.name} ({nav_path}) — every docs/roadmap/items/ entry must be registered in the nav")
            failed = True
    return failed


def check_breadcrumbs():
    failed = False
    for f in DOCS.rglob("*.md"):
        if f == DOCS / "index.md":
            continue
        text = f.read_text()
        lines = text.splitlines()
        # Breadcrumb is expected on the line right after the H1 (and the
        # blank line following it) — check the first few lines, not just
        # the very next one, so a page can carry a short subtitle first.
        head = "\n".join(lines[:6])
        if not re.search(r"\[(?:←\s*)?docs\]\(", head):
            print(f"::error::{f.relative_to(ROOT)}: no breadcrumb back to docs/index.md found near the top of the file")
            failed = True
    return failed


def check_site_version():
    site = ROOT / "site" / "index.html"
    if not site.exists():
        return False  # nothing to check; not this script's job to require site/ exist

    text = site.read_text()
    versions = set(re.findall(r"\bv(\d+\.\d+\.\d+)\b", text))
    if not versions:
        print("::error::site/index.html has no vX.Y.Z version reference at all — "
              "expected at least the hero eyebrow and the comparison-table footnote")
        return True

    try:
        tags = subprocess.run(
            ["git", "tag", "--list"], cwd=ROOT, capture_output=True, text=True, check=True
        ).stdout.split()
    except (subprocess.CalledProcessError, FileNotFoundError) as e:
        print(f"::error::could not list git tags to verify site/index.html's version claims: {e}")
        return True
    tagged = {t.lstrip("v") for t in tags}

    failed = False
    for v in sorted(versions):
        if v not in tagged:
            print(f"::error::site/index.html references v{v}, but no matching git tag exists yet — "
                  "the live public site must never claim a release before `git tag vX.Y.Z` is pushed "
                  "(see AGENTS.md's Release process: tag first, then update the site)")
            failed = True
    return failed


def main():
    failed = False
    for name, check in [
        ("relative links", check_links),
        ("roadmap items registered in mkdocs.yml nav", check_roadmap_nav),
        ("breadcrumb navigation", check_breadcrumbs),
        ("site/index.html only references tagged versions", check_site_version),
    ]:
        print(f"check_docs: {name}")
        if check():
            failed = True
    if failed:
        print("check_docs: FAILED")
        return 1
    print("check_docs: all checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
