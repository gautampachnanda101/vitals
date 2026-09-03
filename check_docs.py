#!/usr/bin/env python3
"""Docs consistency gate: catches the doc-rot this repo has actually shipped.

Three checks, each one added because it caught a real bug while writing this
script (see docs: parent/child breadcrumb nav commit):

1. Every relative link in docs/*.md and README.md resolves to a real file.
   (Caught 5 of 6 roadmap items linking to a nonexistent
   docs/roadmap/architecture/design.md via a wrong `../` count.)
2. Every docs/roadmap/items/NNN-slug/ directory has a matching entry in
   mkdocs.yml's nav. (Caught item 006 missing from the nav entirely.)
3. Every page under docs/ other than docs/index.md itself has a breadcrumb
   line back to the docs home (a line starting with "[docs](" or "[←
   docs]("), so a new page can't be added without wiring it into the
   parent/child navigation this check exists to enforce.

Usage: python3 check_docs.py
"""
import pathlib
import re
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


def main():
    failed = False
    for name, check in [
        ("relative links", check_links),
        ("roadmap items registered in mkdocs.yml nav", check_roadmap_nav),
        ("breadcrumb navigation", check_breadcrumbs),
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
