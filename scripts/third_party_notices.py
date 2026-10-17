#!/usr/bin/env python3
"""Generate (or verify) THIRD_PARTY_NOTICES.txt.

vitals ships as a single static binary, so every third-party licence whose
terms require the notice to accompany binary copies (BSD-3-Clause, MIT,
Apache-2.0, SIL OFL, ISC) has to travel inside it. This file collects all of
them; main.go embeds it and `vitals licenses` prints it, and release
archives carry it as a plain file too.

Sources, all read locally — nothing is downloaded:
  - every Go module compiled into any release target (go list -deps across
    the 6 GOOS/GOARCH pairs build.sh ships), with its LICENSE/NOTICE files
    from the module cache;
  - the Go standard library itself (BSD-3-Clause, The Go Authors);
  - the dashboard's embedded fonts and icons
    (internal/dashboard/static/LICENSE-*.txt).

Usage:
  python3 scripts/third_party_notices.py          # rewrite the file
  python3 scripts/third_party_notices.py --check  # exit 1 if it's stale (CI)
"""

import glob
import json
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
OUT = os.path.join(ROOT, "THIRD_PARTY_NOTICES.txt")
TARGETS = [(o, a) for o in ("darwin", "linux", "windows") for a in ("amd64", "arm64")]
LICENSE_NAMES = ("LICENSE", "LICENSE.txt", "LICENSE.md", "LICENCE", "COPYING", "NOTICE", "NOTICE.txt", "NOTICE.md")

# The Go standard library's licence. Pinned here rather than read from
# GOROOT because some toolchain packages (e.g. Homebrew's) don't install the
# LICENSE file; the text is fixed and identical to golang.org/x/*'s.
GO_LICENSE = """Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
"""

STATIC_ASSETS = [
    ("LICENSE-space-grotesk.txt", "Space Grotesk typeface (dashboard)", "SIL Open Font License 1.1"),
    ("LICENSE-ibm-plex-mono.txt", "IBM Plex Mono typeface (dashboard) — IBM's unmodified Latin-1 web subsets", "SIL Open Font License 1.1, Reserved Font Name \"Plex\""),
    ("LICENSE-lucide.txt", "Lucide icons (dashboard)", "ISC License; MIT for Feather-derived icons"),
]


def modules():
    """Every non-main module any release target links, as {path: (version, dir)}."""
    found = {}
    for goos, goarch in TARGETS:
        env = dict(os.environ, GOOS=goos, GOARCH=goarch, CGO_ENABLED="0")
        out = subprocess.run(
            ["go", "list", "-deps", "-json=Module", "."],
            cwd=ROOT, env=env, check=True, capture_output=True, text=True,
        ).stdout
        dec = json.JSONDecoder()
        i = 0
        while i < len(out):
            while i < len(out) and out[i].isspace():
                i += 1
            if i >= len(out):
                break
            obj, i = dec.raw_decode(out, i)
            m = obj.get("Module")
            if not m or m.get("Main"):
                continue
            found[m["Path"]] = (m["Version"], m["Dir"])
    return found


def licence_files(d):
    names = sorted(n for n in os.listdir(d) if n in LICENSE_NAMES)
    if not any(n.startswith(("LICEN", "COPYING")) for n in names):
        raise SystemExit(f"third_party_notices: no LICENSE file in {d} — check this dependency's licence by hand before shipping it")
    return names


def section(title, subtitle, body):
    rule = "=" * 78
    return f"{rule}\n{title}\n{subtitle}\n{rule}\n\n{body.strip()}\n\n"


def render():
    parts = [
        "Third-party notices for vitals\n"
        "==============================\n\n"
        "vitals itself is MIT-licensed (see LICENSE, or `vitals licenses`). It\n"
        "includes the software and assets below, each under its own licence,\n"
        "reproduced in full as those licences require. Generated by\n"
        "scripts/third_party_notices.py — do not edit by hand.\n\n",
        section("Go standard library", "The Go Authors — BSD-3-Clause", GO_LICENSE),
    ]
    for path, (version, d) in sorted(modules().items()):
        body = "\n\n".join(open(os.path.join(d, n), encoding="utf-8").read().strip() for n in licence_files(d))
        parts.append(section(f"{path} {version}", "Go module compiled into vitals", body))
    for name, title, lic in STATIC_ASSETS:
        body = open(os.path.join(ROOT, "internal/dashboard/static", name), encoding="utf-8").read()
        parts.append(section(title, lic, body))
    # Fail loudly if a new static licence appears without being listed above.
    on_disk = {os.path.basename(p) for p in glob.glob(os.path.join(ROOT, "internal/dashboard/static/LICENSE-*.txt"))}
    listed = {n for n, _, _ in STATIC_ASSETS}
    if on_disk != listed:
        raise SystemExit(f"third_party_notices: static licences on disk {sorted(on_disk)} != listed {sorted(listed)}")
    return "".join(parts).rstrip() + "\n"


def main():
    text = render()
    if "--check" in sys.argv[1:]:
        current = open(OUT, encoding="utf-8").read() if os.path.exists(OUT) else ""
        if current != text:
            print("third_party_notices: THIRD_PARTY_NOTICES.txt is stale — run `make notices` and commit the result", file=sys.stderr)
            return 1
        print("third_party_notices: THIRD_PARTY_NOTICES.txt is up to date")
        return 0
    with open(OUT, "w", encoding="utf-8") as f:
        f.write(text)
    print(f"third_party_notices: wrote {os.path.relpath(OUT, ROOT)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
