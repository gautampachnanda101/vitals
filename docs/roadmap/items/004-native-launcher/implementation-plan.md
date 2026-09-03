# Implementation plan — 004 Native double-click launcher

[docs](../../../index.md) / [Roadmap](../../index.md) / [004 — Native double-click launcher](index.md) / **Implementation plan**

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

## Tasks

- [x] **macOS**: `Vitals.app` (`packaging/macos/Info.plist` +
      `packaging/macos/vitals-launcher`, a POSIX shell script), assembled
      into the darwin release archives by `.github/workflows/release.yml`
      alongside the raw `vitals` binary — both in the same tar.gz, so
      `install.sh`'s existing `tar -xzf` + `install $tmp/vitals` needed no
      change. **Genuinely tested on real macOS** (not just written and
      assumed): built the exact archive locally, extracted it, and
      double-click-equivalent launched it via `open Vitals.app` — verified
      the real bundled binary starts, listens on a real loopback port,
      and terminates cleanly on SIGTERM. `LSUIElement` is set (no Dock
      icon/menu bar) since the browser tab is the only UI; the disclosed
      cost is **no visible Quit** — stopping it needs Activity Monitor
      → Force Quit "vitals", or `pkill -f 'vitals dashboard'` from a
      terminal. Unsigned and not notarized (no Apple Developer ID
      available to do either) — first open needs right-click → Open past
      Gatekeeper, documented here rather than silently assumed away.
      Homebrew's formula also carries `Vitals.app` (in the keg's prefix,
      not `/Applications` — that's a Cask's job, not a Formula's; the
      formula's `caveats` says so).
- [x] **Linux**: `packaging/linux/vitals.desktop`, validated structurally
      (a real INI parse, all required `Desktop Entry` keys present) but
      **not tested against a real GNOME/KDE launcher** — no Linux GUI
      environment was available to verify against. `install.sh` now
      writes it to `~/.local/share/applications/vitals.desktop` after
      installing the binary, substituting the real install path, best
      effort (never fails the install if `~/.local/share` isn't
      writable).
- [x] **Windows**: no separate launcher binary — `packaging/scoop/
      vitals.json` gained a `shortcuts` entry (`["vitals.exe", "Vitals
      Dashboard", "dashboard"]`), Scoop's own built-in mechanism for a
      Start Menu shortcut targeting `vitals.exe dashboard`. **Not tested
      on real Windows** — no Windows environment was available. Known,
      disclosed limitation: this only covers Scoop installs; a user who
      grabs the raw `vitals_Windows_*.zip` from the release page gets no
      shortcut at all — an actual installer (MSI/NSIS) would be needed to
      close that gap, out of scope for this pass.
- [x] Updated `install.sh` (Linux `.desktop`) and `packaging/homebrew/
      vitals.rb` (`Vitals.app` in the keg). Scoop's manifest already
      covered above.
- [x] **"Must not fail silently" — closed for macOS, open for Linux/
      Windows.** A double-clicked `.app` has no terminal for `vitals
      dashboard`'s own `ui.Warnf` browser-open failure message to reach,
      so `vitals-launcher` (not `vitals` itself) now owns opening the
      browser (`vitals dashboard --no-open`, then `open <url>`) and falls
      back to a native `osascript` notification if either the browser
      fails to open or vitals never starts at all. **Both failure paths
      verified for real**: faked a failing `open` and a hanging `vitals`
      binary on PATH and confirmed, via a fake `osascript` that logs its
      own invocation, that each path fires the correct notification text
      with the real URL embedded. The equivalent gap on Linux (`.desktop`
      Exec=) and Windows (Scoop shortcut) is real and currently
      undemonstrated — no environment available to build or verify a
      `notify-send`/Windows-toast equivalent; left open rather than
      shipping unverified code for platforms this session couldn't test.

## Exit criteria

On each of the three OSes, a user who downloads the release archive can
reach the dashboard without opening a terminal, or this plan explicitly
documents why a given OS still needs one step of terminal use and what
that step is. **Met for macOS** (fully verified, including the
must-not-fail-silently requirement) **and Windows-via-Scoop**
(mechanism in place, unverified on real hardware). **Partially met for
Linux and for Windows-via-direct-download**: the launcher file exists and
is structurally valid, but hasn't been verified against a real desktop
environment, and the silent-browser-failure gap is explicitly still open
on both.
