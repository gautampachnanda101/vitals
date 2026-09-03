# Implementation plan — 004 Native double-click launcher

[docs](../../../index.md) / [Roadmap](../../index.md) / [004 — Native double-click launcher](index.md) / **Implementation plan**

This file shows what's **left**. Check off or delete a task as it lands.
See `AGENTS.md`'s "Roadmap discipline" section for the rule.

## Tasks

- [ ] macOS: a minimal `.app` bundle (`Info.plist` + a shell-script or tiny
      stub calling the binary). Must address code signing/Gatekeeper: an
      unsigned double-clicked app is blocked by default — either Apple
      Developer ID signing + notarization, or an explicit, honest
      "right-click → Open the first time" instruction in the download
      flow. Decide which before building, not after.
- [ ] Linux: a `.desktop` file alongside the existing tarball/package.
- [ ] Windows: a shortcut (`.lnk`) or a trivial launcher `.exe`; verify it
      doesn't trip SmartScreen unsigned-binary warnings any worse than the
      existing Scoop-installed binary already does.
- [ ] Update `install.sh`/Homebrew/Scoop packaging to lay down the
      launcher alongside the binary.
- [ ] Decide and document what happens when the launcher runs and a
      browser can't be opened / no default browser is configured — must
      not fail silently.

## Exit criteria

On each of the three OSes, a user who downloads the release archive can
reach the dashboard without opening a terminal, or this plan explicitly
documents why a given OS still needs one step of terminal use and what
that step is.
